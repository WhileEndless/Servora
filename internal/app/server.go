package app

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
	"github.com/WhileEndless/Servora/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	cfg               Config
	version           string
	store             *store.Store
	agent             *http.Client
	packageAgent      *http.Client
	http              *http.Server
	mu                sync.RWMutex
	snapshot          model.Snapshot
	subs              map[chan []byte]struct{}
	stop              chan struct{}
	lastPersist       time.Time
	lastFlowPersist   time.Time
	lastCleanup       time.Time
	packageRefreshing bool
	packageRefresh    chan bool
}

type contextKey string

const sessionKey contextKey = "session"

func NewServer(cfg Config, version string) (*Server, error) {
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", cfg.AgentSocket)
		},
		DisableCompression: true,
		MaxIdleConns:       4,
	}
	s := &Server{
		cfg: cfg, version: version, store: db,
		agent:        &http.Client{Transport: transport, Timeout: 20 * time.Second},
		packageAgent: &http.Client{Transport: transport, Timeout: 11 * time.Minute},
		subs:         map[chan []byte]struct{}{}, stop: make(chan struct{}),
		packageRefresh: make(chan bool, 1),
	}
	mux := http.NewServeMux()
	s.routes(mux)
	s.http = &http.Server{
		Addr: cfg.Listen, Handler: s.security(mux), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 20 * time.Second, WriteTimeout: 45 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return s, nil
}

func (s *Server) Close() error {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	return s.store.Close()
}

func (s *Server) Run() error {
	go s.collectLoop()
	go s.packageLoop()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}()
	log.Printf("monitor v%s listening on https://%s", s.version, s.cfg.Listen)
	err := s.http.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/health", s.health)
	mux.HandleFunc("/api/v1/auth/login", s.login)
	mux.Handle("/api/v1/auth/logout", s.requireAuth(http.HandlerFunc(s.logout)))
	mux.Handle("/api/v1/auth/session", s.requireAuth(http.HandlerFunc(s.session)))
	mux.Handle("/api/v1/overview", s.requireAuth(http.HandlerFunc(s.overview)))
	mux.Handle("/api/v1/processes", s.requireAuth(http.HandlerFunc(s.segment("processes"))))
	mux.Handle("/api/v1/processes/", s.requireAuth(http.HandlerFunc(s.processDetail)))
	mux.Handle("/api/v1/network", s.requireAuth(http.HandlerFunc(s.segment("network"))))
	mux.Handle("/api/v1/network-usage", s.requireAuth(http.HandlerFunc(s.networkUsage)))
	mux.Handle("/api/v1/network-usage/detail", s.requireAuth(http.HandlerFunc(s.networkUsageDetail)))
	mux.Handle("/api/v1/resource-usage", s.requireAuth(http.HandlerFunc(s.resourceUsage)))
	mux.Handle("/api/v1/resource-usage/detail", s.requireAuth(http.HandlerFunc(s.resourceUsageDetail)))
	mux.Handle("/api/v1/settings/network", s.requireAuth(http.HandlerFunc(s.networkSettings)))
	mux.Handle("/api/v1/settings/resources", s.requireAuth(http.HandlerFunc(s.resourceSettings)))
	mux.Handle("/api/v1/ssh", s.requireAuth(http.HandlerFunc(s.segment("ssh"))))
	mux.Handle("/api/v1/docker", s.requireAuth(http.HandlerFunc(s.segment("docker"))))
	mux.Handle("/api/v1/docker/images", s.requireAuth(http.HandlerFunc(s.dockerImages)))
	mux.Handle("/api/v1/services", s.requireAuth(http.HandlerFunc(s.segment("services"))))
	mux.Handle("/api/v1/packages", s.requireAuth(http.HandlerFunc(s.packages)))
	mux.Handle("/api/v1/packages/refresh", s.requireAuth(http.HandlerFunc(s.packageRefreshRequest)))
	mux.Handle("/api/v1/packages/", s.requireAuth(http.HandlerFunc(s.packageItem)))
	mux.Handle("/api/v1/package-events", s.requireAuth(http.HandlerFunc(s.packageEvents)))
	mux.Handle("/api/v1/schedules", s.requireAuth(http.HandlerFunc(s.schedules)))
	mux.Handle("/api/v1/history", s.requireAuth(http.HandlerFunc(s.history)))
	mux.Handle("/api/v1/watches", s.requireAuth(http.HandlerFunc(s.watches)))
	mux.Handle("/api/v1/watches/", s.requireAuth(http.HandlerFunc(s.watchItem)))
	mux.Handle("/api/v1/alert-rules", s.requireAuth(http.HandlerFunc(s.alertRules)))
	mux.Handle("/api/v1/alert-rules/", s.requireAuth(http.HandlerFunc(s.alertRuleItem)))
	mux.Handle("/api/v1/alerts", s.requireAuth(http.HandlerFunc(s.alerts)))
	mux.Handle("/api/v1/alerts/", s.requireAuth(http.HandlerFunc(s.alertItem)))
	mux.Handle("/api/v1/notification-targets", s.requireAuth(http.HandlerFunc(s.notificationTargets)))
	mux.Handle("/api/v1/notification-targets/", s.requireAuth(http.HandlerFunc(s.notificationTargetItem)))
	mux.Handle("/api/v1/activities", s.requireAuth(http.HandlerFunc(s.activities)))
	mux.Handle("/api/v1/actions", s.requireAuth(http.HandlerFunc(s.actions)))
	mux.Handle("/api/v1/stream", s.requireAuth(http.HandlerFunc(s.stream)))
	mux.Handle("/api/v1/modules", s.requireAuth(http.HandlerFunc(s.modules)))
	// Keep every API path behind authentication by default. Public endpoints
	// above are more specific and therefore still win ServeMux routing.
	mux.Handle("/api/", s.requireAuth(http.NotFoundHandler()))
	root, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", spaHandler(http.FS(root)))
}

func (s *Server) processDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rawPID := strings.TrimPrefix(r.URL.Path, "/api/v1/processes/")
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid < 1 {
		writeError(w, http.StatusBadRequest, "invalid_pid", "Invalid process ID")
		return
	}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://agent/v1/process/"+strconv.Itoa(pid), nil)
	response, err := s.agent.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "agent_error", "Process details are unavailable")
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 2<<20))
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		ip := peerIP(r)
		if !containsIP(s.cfg.AllowedCIDRs, net.ParseIP(ip)) {
			http.Error(w, "network not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("sms_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Authentication required")
			return
		}
		session, err := s.store.Session(cookie.Value, s.cfg.SessionIdle, s.cfg.SessionAbsolute, time.Now())
		if err != nil || session.IP != peerIP(r) {
			s.store.DeleteSession(cookie.Value)
			writeError(w, http.StatusUnauthorized, "invalid_session", "Session expired")
			return
		}
		if mutation(r.Method) {
			if r.Header.Get("X-CSRF-Token") != session.CSRF || !sameOrigin(r) {
				writeError(w, http.StatusForbidden, "csrf_failed", "Request verification failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, session)))
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ip := peerIP(r)
	if until, banned := s.store.IsBanned(ip, time.Now()); banned {
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
		writeError(w, http.StatusTooManyRequests, "temporarily_banned", "Too many failed attempts")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request")
		return
	}
	time.Sleep(250 * time.Millisecond)
	err := s.agentAuthenticate(r.Context(), input.Username, input.Password, ip)
	if err != nil {
		until, banned := s.store.LoginFailed(ip, input.Username, time.Now())
		s.store.Audit(input.Username, ip, "auth.login", "", map[string]string{}, false, "authentication failed")
		if banned {
			w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
		}
		writeError(w, http.StatusUnauthorized, "login_failed", "Invalid credentials or account is not authorized")
		return
	}
	token, csrf := randomID(32), randomID(24)
	if err := s.store.CreateSession(token, input.Username, csrf, ip, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "Could not create session")
		return
	}
	s.store.ClearFailures(ip)
	s.store.Audit(input.Username, ip, "auth.login", "", map[string]string{}, true, "login successful")
	http.SetCookie(w, &http.Cookie{
		Name: "sms_session", Value: token, Path: "/", MaxAge: int(s.cfg.SessionAbsolute.Seconds()),
		Expires:  time.Now().Add(s.cfg.SessionAbsolute),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": input.Username, "csrf": csrf, "version": s.version})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if cookie, err := r.Cookie("sms_session"); err == nil {
		s.store.DeleteSession(cookie.Value)
	}
	session := currentSession(r)
	s.store.Audit(session.User, peerIP(r), "auth.logout", "", nil, true, "logout successful")
	http.SetCookie(w, &http.Cookie{Name: "sms_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ss := currentSession(r)
	writeJSON(w, http.StatusOK, map[string]any{"username": ss.User, "csrf": ss.CSRF, "version": s.version, "role": "admin"})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	err := s.store.Ping()
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ok": err == nil})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	snapshot := s.snapshot
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) segment(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.mu.RLock()
		snapshot := s.snapshot
		s.mu.RUnlock()
		switch name {
		case "processes":
			writeJSON(w, http.StatusOK, map[string]any{"items": snapshot.Processes, "timestamp": snapshot.Timestamp})
		case "network":
			writeJSON(w, http.StatusOK, map[string]any{"interfaces": snapshot.Network, "connections": snapshot.Connections, "timestamp": snapshot.Timestamp})
		case "ssh":
			writeJSON(w, http.StatusOK, map[string]any{"sessions": snapshot.SSHSessions, "timestamp": snapshot.Timestamp})
		case "docker":
			writeJSON(w, http.StatusOK, map[string]any{
				"items": snapshot.Containers, "available": snapshot.Capabilities["docker"],
				"summary": snapshot.Docker, "freshness": snapshot.Freshness["docker"],
				"errors": dockerErrors(snapshot.Errors),
			})
		case "services":
			writeJSON(w, http.StatusOK, map[string]any{"items": snapshot.Services})
		}
	}
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			from = parsed
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			to = parsed
		}
	}
	points, err := s.store.History(from, to, 10000)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": points})
}

func (s *Server) networkUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	from, to := networkQueryRange(r, s.store.NetworkRetentionDays())
	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "group" {
		groupBy = "process"
	}
	items, err := s.store.NetworkUsage(from, to, groupBy, r.URL.Query().Get("q"), 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Network history is unavailable")
		return
	}
	if items == nil {
		items = []store.NetworkUsage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "group_by": groupBy, "from": from, "to": to,
		"storage": s.store.NetworkStorage(),
	})
}

func (s *Server) networkUsageDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	selector := r.URL.Query().Get("selector")
	if selector != "group" && selector != "pid" {
		selector = "process"
	}
	value := strings.TrimSpace(r.URL.Query().Get("value"))
	if value == "" || len(value) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_selector", "A process, group or PID is required")
		return
	}
	if selector == "pid" {
		pid, err := strconv.Atoi(value)
		if err != nil || pid < 1 {
			writeError(w, http.StatusBadRequest, "invalid_selector", "A valid PID is required")
			return
		}
		value = strconv.Itoa(pid)
	}
	from, to := networkQueryRange(r, s.store.NetworkRetentionDays())
	destinations, err := s.store.NetworkDestinations(from, to, selector, value, 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Network destinations are unavailable")
		return
	}
	timeline, err := s.store.NetworkTimeline(from, to, selector, value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Network timeline is unavailable")
		return
	}
	if destinations == nil {
		destinations = []store.NetworkDestination{}
	}
	if timeline == nil {
		timeline = []store.NetworkTimelinePoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"selector": selector, "value": value, "from": from, "to": to,
		"destinations": destinations, "timeline": timeline,
	})
}

func (s *Server) resourceUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	from, to := networkQueryRange(r, s.store.NetworkRetentionDays())
	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "group" {
		groupBy = "process"
	}
	items, err := s.store.ResourceUsage(from, to, groupBy, r.URL.Query().Get("q"), 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Resource history is unavailable")
		return
	}
	if items == nil {
		items = []store.ResourceUsage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "group_by": groupBy, "from": from, "to": to,
		"storage": s.store.NetworkStorage(),
	})
}

func (s *Server) resourceUsageDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	selector := r.URL.Query().Get("selector")
	if selector != "group" {
		selector = "process"
	}
	value := strings.TrimSpace(r.URL.Query().Get("value"))
	if value == "" || len(value) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_selector", "A process or group is required")
		return
	}
	from, to := networkQueryRange(r, s.store.NetworkRetentionDays())
	timeline, err := s.store.ResourceTimeline(from, to, selector, value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Resource timeline is unavailable")
		return
	}
	if timeline == nil {
		timeline = []store.ResourceTimelinePoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"selector": selector, "value": value, "from": from, "to": to, "timeline": timeline,
	})
}

func (s *Server) networkSettings(w http.ResponseWriter, r *http.Request) {
	session := currentSession(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.NetworkStorage())
	case http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var input struct {
			RetentionDays  int   `json:"retention_days"`
			NetworkEnabled *bool `json:"network_enabled"`
			CPUEnabled     *bool `json:"cpu_enabled"`
			MemoryEnabled  *bool `json:"memory_enabled"`
			DiskIOEnabled  *bool `json:"disk_io_enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.RetentionDays < 1 || input.RetentionDays > 365 {
			writeError(w, http.StatusBadRequest, "invalid_retention", "Retention must be between 1 and 365 days")
			return
		}
		if err := s.store.SetNetworkRetentionDays(input.RetentionDays); err != nil {
			writeError(w, http.StatusInternalServerError, "settings_failed", "Could not save network retention")
			return
		}
		current := s.store.NetworkStorage()
		network, cpu, memory, diskIO := current.NetworkEnabled, current.CPUEnabled, current.MemoryEnabled, current.DiskIOEnabled
		if input.NetworkEnabled != nil {
			network = *input.NetworkEnabled
		}
		if input.CPUEnabled != nil {
			cpu = *input.CPUEnabled
		}
		if input.MemoryEnabled != nil {
			memory = *input.MemoryEnabled
		}
		if input.DiskIOEnabled != nil {
			diskIO = *input.DiskIOEnabled
		}
		if err := s.store.SetHistoryCollectors(network, cpu, memory, diskIO); err != nil {
			writeError(w, http.StatusInternalServerError, "settings_failed", "Could not save collector settings")
			return
		}
		_ = s.store.PruneNetworkFlows(time.Now())
		s.store.Audit(session.User, peerIP(r), "settings.network.update", "", input, true, "network retention updated")
		writeJSON(w, http.StatusOK, s.store.NetworkStorage())
	case http.MethodDelete:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var input struct {
			Confirm string `json:"confirm"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Confirm != "DELETE NETWORK HISTORY" {
			writeError(w, http.StatusBadRequest, "confirmation_required", "Exact confirmation phrase is required")
			return
		}
		if err := s.resetAgentNetworkAccounting(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, "accounting_reset_failed", "Could not reset live network accounting")
			return
		}
		if err := s.store.ClearNetworkFlows(); err != nil {
			writeError(w, http.StatusInternalServerError, "cleanup_failed", "Could not clear network history")
			return
		}
		s.store.Audit(session.User, peerIP(r), "network.history.clear", "", nil, true, "network history cleared")
		writeJSON(w, http.StatusOK, s.store.NetworkStorage())
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) resetAgentNetworkAccounting(ctx context.Context) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/network/reset", nil)
	response, err := s.agent.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return fmt.Errorf("agent returned %s", response.Status)
	}
	return nil
}

func (s *Server) resourceSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.store.NetworkStorage())
		return
	}
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var input struct {
		Confirm string `json:"confirm"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.Confirm != "DELETE RESOURCE HISTORY" {
		writeError(w, http.StatusBadRequest, "confirmation_required", "Exact confirmation phrase is required")
		return
	}
	if err := s.store.ClearResourceHistory(); err != nil {
		writeError(w, http.StatusInternalServerError, "cleanup_failed", "Could not clear resource history")
		return
	}
	session := currentSession(r)
	s.store.Audit(session.User, peerIP(r), "resource.history.clear", "", nil, true, "resource history cleared")
	writeJSON(w, http.StatusOK, s.store.NetworkStorage())
}

func networkQueryRange(r *http.Request, retentionDays int) (time.Time, time.Time) {
	to := time.Now()
	from := to.AddDate(0, 0, -retentionDays)
	if parsed, err := time.Parse(time.RFC3339, r.URL.Query().Get("to")); err == nil {
		to = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, r.URL.Query().Get("from")); err == nil {
		from = parsed
	}
	maxFrom := to.AddDate(0, 0, -retentionDays)
	if from.Before(maxFrom) {
		from = maxFrom
	}
	return from, to
}

func (s *Server) watches(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.Watches()
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var item store.Watch
		if !decodeJSON(w, r, &item, 32<<10) {
			return
		}
		if item.ID == "" {
			item.ID = randomID(12)
		}
		if item.Name == "" || (item.Field != "name" && item.Field != "executable" && item.Field != "command") {
			writeError(w, 400, "invalid_watch", "Invalid watch rule")
			return
		}
		if _, err := regexp.Compile(item.Pattern); err != nil {
			writeError(w, 400, "invalid_regex", err.Error())
			return
		}
		item.CreatedBy = currentSession(r).User
		if err := s.store.PutWatch(item); err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		s.store.Audit(item.CreatedBy, peerIP(r), "watch.create", item.ID, item, true, "watch saved")
		writeJSON(w, 201, item)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) watchItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/watches/")
	if !safeID(id) {
		methodNotAllowed(w)
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.WatchHistory(id, time.Now().Add(-24*time.Hour), 10000)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	err := s.store.DeleteWatch(id)
	s.auditResult(r, "watch.delete", id, nil, err)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) alertRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.AlertRules()
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var item store.AlertRule
		if !decodeJSON(w, r, &item, 32<<10) {
			return
		}
		if item.ID == "" {
			item.ID = randomID(12)
		}
		if !validAlertRule(item) {
			writeError(w, 400, "invalid_rule", "Invalid alert rule")
			return
		}
		err := s.store.PutAlertRule(item)
		s.auditResult(r, "alert_rule.save", item.ID, item, err)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 201, item)
	default:
		methodNotAllowed(w)
	}
}
func (s *Server) alertRuleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-rules/")
	if r.Method != http.MethodDelete || !safeID(id) {
		methodNotAllowed(w)
		return
	}
	err := s.store.DeleteAlertRule(id)
	s.auditResult(r, "alert_rule.delete", id, nil, err)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	items, err := s.store.Alerts(250)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) alertItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/"), "/acknowledge")
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/acknowledge") || !safeID(id) {
		methodNotAllowed(w)
		return
	}
	err := s.store.AcknowledgeAlert(id)
	s.auditResult(r, "alert.acknowledge", id, nil, err)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) notificationTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.NotificationTargets()
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			store.NotificationTarget
			Token string `json:"token"`
		}
		if !decodeJSON(w, r, &in, 16<<10) {
			return
		}
		// IDs and secret references are server-owned so a caller cannot replace
		// another destination or overwrite an existing secret.
		in.ID = randomID(12)
		if in.Provider == "" {
			in.Provider = "telegram"
		}
		in.SecretRef = randomID(12)
		if !validNotificationTarget(in.Name, in.Provider, in.ChatID, in.Token) {
			writeError(w, 400, "invalid_target", "Name, chat ID and bot token are required")
			return
		}
		req := model.ActionRequest{Action: "secret.write", Target: in.SecretRef, Params: map[string]string{"value": in.Token}}
		if _, err := s.agentAction(r.Context(), req); err != nil {
			writeError(w, 502, "agent_error", err.Error())
			return
		}
		err := s.store.PutNotificationTarget(in.NotificationTarget)
		if err != nil {
			_, _ = s.agentAction(r.Context(), model.ActionRequest{Action: "secret.delete", Target: in.SecretRef})
		}
		s.auditResult(r, "notification_target.save", in.ID, map[string]string{"provider": "telegram", "chat_id": in.ChatID}, err)
		if err != nil {
			writeError(w, 500, "database_error", err.Error())
			return
		}
		in.Token = ""
		writeJSON(w, 201, in.NotificationTarget)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) notificationTargetItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/notification-targets/")
	if strings.HasSuffix(path, "/test") {
		id := strings.TrimSuffix(path, "/test")
		if r.Method != http.MethodPost || !safeID(id) {
			methodNotAllowed(w)
			return
		}
		err := s.sendToTarget(r.Context(), id, "✅ Servora test message")
		s.auditResult(r, "notification_target.test", id, nil, err)
		if err != nil {
			writeError(w, 502, "telegram_error", err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != http.MethodDelete || !safeID(path) {
		methodNotAllowed(w)
		return
	}
	targets, _ := s.store.NotificationTargets()
	var secret string
	for _, t := range targets {
		if t.ID == path {
			secret = t.SecretRef
		}
	}
	err := s.store.DeleteNotificationTarget(path)
	if err == nil && secret != "" {
		_, _ = s.agentAction(r.Context(), model.ActionRequest{Action: "secret.delete", Target: secret})
	}
	s.auditResult(r, "notification_target.delete", path, nil, err)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) activities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	items, err := s.store.Audits(500)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.RLock()
		items := s.snapshot.Timers
		s.mu.RUnlock()
		writeJSON(w, 200, map[string]any{"items": items, "executables": mapKeys(s.cfg.JobExecutables)})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var req model.ActionRequest
	if !decodeJSON(w, r, &req, 32<<10) {
		return
	}
	if !strings.HasPrefix(req.Action, "schedule.") {
		writeError(w, 400, "invalid_action", "Invalid schedule action")
		return
	}
	s.forwardAction(w, r, req)
}

func (s *Server) actions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req model.ActionRequest
	if !decodeJSON(w, r, &req, 32<<10) {
		return
	}
	if strings.HasPrefix(req.Action, "secret.") || strings.HasPrefix(req.Action, "schedule.") {
		writeError(w, 403, "action_denied", "Use the dedicated endpoint")
		return
	}
	s.forwardAction(w, r, req)
}
func (s *Server) forwardAction(w http.ResponseWriter, r *http.Request, req model.ActionRequest) {
	response, err := s.agentAction(r.Context(), req)
	s.auditResult(r, req.Action, req.Target, redactParams(req.Params), err)
	if err != nil {
		writeError(w, 400, "action_failed", err.Error())
		return
	}
	writeJSON(w, 200, response)
}

func (s *Server) modules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	caps := s.snapshot.Capabilities
	s.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"version": s.version, "capabilities": caps, "modules": []map[string]any{{"id": "linux", "version": s.version, "healthy": true}, {"id": "systemd", "version": s.version, "healthy": caps["systemd"]}, {"id": "docker", "version": s.version, "healthy": caps["docker"]}, {"id": "telegram", "version": s.version, "healthy": true}}})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "Streaming unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 1000\n\n")
	ch := make(chan []byte, 2)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	latest, _ := json.Marshal(s.snapshot)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subs, ch); s.mu.Unlock() }()
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", latest)
	flusher.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case data := <-ch:
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
			flusher.Flush()
		case <-tick.C:
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) collectLoop() {
	timer := time.NewTicker(s.cfg.SampleInterval)
	defer timer.Stop()
	s.collect()
	for {
		select {
		case <-timer.C:
			s.collect()
		case <-s.stop:
			return
		}
	}
}
func (s *Server) collect() {
	req, _ := http.NewRequest(http.MethodGet, "http://agent/v1/snapshot", nil)
	resp, err := s.agent.Do(req)
	if err != nil {
		log.Printf("collector: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("collector status: %s", resp.Status)
		return
	}
	var snapshot model.Snapshot
	if json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&snapshot) != nil {
		return
	}
	now := time.Now()
	var newestFlow time.Time
	for _, flow := range snapshot.NetworkFlows {
		if flow.Timestamp.After(newestFlow) {
			newestFlow = flow.Timestamp
		}
	}
	if s.store.NetworkHistoryEnabled() && newestFlow.After(s.lastFlowPersist) {
		if err := s.store.SaveNetworkFlows(snapshot.NetworkFlows); err != nil {
			log.Printf("save network flows: %v", err)
		} else {
			s.lastFlowPersist = newestFlow
		}
	}
	// Flow deltas are an ingestion detail. Persist them before publishing the
	// snapshot, then omit them from SSE/overview payloads: history has dedicated
	// query endpoints and sending it every second wastes network bandwidth.
	snapshot.NetworkFlows = nil
	s.mu.Lock()
	s.snapshot = snapshot
	data, _ := json.Marshal(snapshot)
	for ch := range s.subs {
		select {
		case ch <- data:
		default:
		}
	}
	s.mu.Unlock()
	if s.lastPersist.IsZero() || now.Sub(s.lastPersist) >= 10*time.Second {
		if err := s.store.SaveMetric(snapshot); err != nil {
			log.Printf("save metrics: %v", err)
		}
		if err := s.store.SaveWatchMetrics(snapshot); err != nil {
			log.Printf("save watch metrics: %v", err)
		}
		if err := s.store.SaveProcessResources(snapshot); err != nil {
			log.Printf("save process resources: %v", err)
		}
		s.lastPersist = now
	}
	s.evaluateAlerts(snapshot)
	if s.lastCleanup.IsZero() || now.Sub(s.lastCleanup) >= 15*time.Minute {
		_ = s.store.RollupAndPrune(now, s.cfg.RawRetention, s.cfg.RollupRetention, s.cfg.MaxDatabaseBytes, s.cfg.DatabasePath())
		_ = s.store.PruneNetworkFlows(now)
		s.lastCleanup = now
	}
}

func (s *Server) evaluateAlerts(snapshot model.Snapshot) {
	rules, err := s.store.AlertRules()
	if err != nil {
		return
	}
	now := time.Now()
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		value, ok := s.alertValue(rule.Source, snapshot)
		if !ok {
			continue
		}
		trigger := compare(value, rule.Operator, rule.Threshold)
		active, exists := s.store.ActiveAlert(rule.ID)
		if trigger {
			if !exists {
				active = store.Alert{ID: randomID(12), RuleID: rule.ID, Name: rule.Name, Severity: rule.Severity, State: "pending", StartedAt: now}
			}
			active.Message = fmt.Sprintf("%s: %.2f %s %.2f%s", rule.Source, value, rule.Operator, rule.Threshold, s.alertContext(rule.Source, snapshot))
			active.UpdatedAt = now
			if active.State == "pending" && now.Sub(active.StartedAt) >= time.Duration(rule.ForSeconds*float64(time.Second)) {
				active.State = "firing"
				go s.notifyRule(rule, "🔥 "+active.Name+"\n"+active.Message)
			}
			_ = s.store.PutAlert(active)
		} else if exists {
			wasFiring := active.State == "firing" || active.State == "acknowledged"
			active.State = "resolved"
			active.UpdatedAt = now
			active.Message = fmt.Sprintf("%s recovered at %.2f%s", rule.Source, value, s.alertContext(rule.Source, snapshot))
			_ = s.store.PutAlert(active)
			if wasFiring && rule.NotifyRecovery {
				go s.notifyRule(rule, "✅ "+active.Name+" resolved\n"+active.Message)
			}
		}
	}
}

func (s *Server) alertContext(source string, snapshot model.Snapshot) string {
	if strings.HasPrefix(source, "network_") {
		hours := 24
		if source == "network_total_1h" {
			hours = 1
		}
		items, err := s.store.NetworkUsage(time.Now().Add(-time.Duration(hours)*time.Hour), time.Now(), "process", "", 3)
		if err == nil && len(items) > 0 {
			parts := make([]string, 0, len(items))
			for _, item := range items {
				parts = append(parts, fmt.Sprintf("%s %s", item.Key, formatIEC(item.RXBytes+item.TXBytes)))
			}
			return " · Top consumers: " + strings.Join(parts, ", ")
		}
		return ""
	}
	if source == "disk" {
		if len(snapshot.Disks) == 0 {
			return ""
		}
		disks := append([]model.Disk(nil), snapshot.Disks...)
		sort.Slice(disks, func(i, j int) bool { return disks[i].UsedPercent > disks[j].UsedPercent })
		return fmt.Sprintf(" · Highest mount: %s %.1f%% (%s used)", disks[0].Mount, disks[0].UsedPercent, formatIEC(disks[0].Used))
	}
	processes := append([]model.Process(nil), snapshot.Processes...)
	memorySource := source == "memory" || source == "swap"
	if memorySource {
		sort.Slice(processes, func(i, j int) bool { return processes[i].Memory > processes[j].Memory })
	} else {
		sort.Slice(processes, func(i, j int) bool { return processes[i].CPU > processes[j].CPU })
	}
	if len(processes) > 3 {
		processes = processes[:3]
	}
	parts := make([]string, 0, len(processes))
	for _, process := range processes {
		if memorySource {
			parts = append(parts, fmt.Sprintf("%s(%d) %s", process.Name, process.PID, formatIEC(process.Memory)))
		} else {
			parts = append(parts, fmt.Sprintf("%s(%d) %.1f%%", process.Name, process.PID, process.CPU))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · Top processes: " + strings.Join(parts, ", ")
}

func formatIEC(value uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	amount, index := float64(value), 0
	for amount >= 1024 && index < len(units)-1 {
		amount /= 1024
		index++
	}
	return fmt.Sprintf("%.1f %s", amount, units[index])
}

func (s *Server) notifyRule(rule store.AlertRule, message string) {
	for _, id := range strings.Split(rule.TargetIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := s.sendToTarget(ctx, id, message)
			cancel()
			if err != nil {
				log.Printf("notification %s: %v", id, err)
			}
		}
	}
}
func (s *Server) sendToTarget(ctx context.Context, id, message string) error {
	targets, err := s.store.NotificationTargets()
	if err != nil {
		return err
	}
	var target *store.NotificationTarget
	for i := range targets {
		if targets[i].ID == id && targets[i].Enabled {
			target = &targets[i]
			break
		}
	}
	if target == nil {
		return errors.New("notification target not found")
	}
	tokenRaw, err := os.ReadFile(filepath.Join(s.cfg.SecretDir(), target.SecretRef))
	if err != nil {
		return errors.New("notification secret unavailable")
	}
	token := strings.TrimSpace(string(tokenRaw))
	form := url.Values{"chat_id": {target.ChatID}, "text": {message}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+url.PathEscape(token)+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// net/http errors may embed req.URL, whose path contains the bot token.
		return errors.New("telegram request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("telegram returned %s", resp.Status)
	}
	return nil
}

func (s *Server) agentAction(ctx context.Context, req model.ActionRequest) (model.ActionResponse, error) {
	var out model.ActionResponse
	raw, _ := json.Marshal(req)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/action", strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	resp, err := s.agent.Do(request)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return out, err
	}
	if resp.StatusCode/100 != 2 || !out.OK {
		return out, errors.New(out.Message)
	}
	return out, nil
}

func (s *Server) agentAuthenticate(ctx context.Context, username, password, remote string) error {
	raw, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
		"remote":   remote,
	})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/auth", strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	response, err := s.agent.Do(request)
	if err != nil {
		return errors.New("authentication service unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
	if response.StatusCode != http.StatusOK {
		return errors.New("authentication failed")
	}
	return nil
}

func (s *Server) auditResult(r *http.Request, action, target string, params any, err error) {
	result := "operation completed"
	success := err == nil
	if err != nil {
		result = err.Error()
	}
	ss := currentSession(r)
	s.store.Audit(ss.User, peerIP(r), action, target, params, success, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid_json", err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "invalid_json", "Request body must contain exactly one JSON value")
		return false
	}
	return true
}
func randomID(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func safeID(v string) bool { ok, _ := regexp.MatchString(`^[a-f0-9-]{8,64}$`, v); return ok }
func mutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
func currentSession(r *http.Request) store.Session {
	v, _ := r.Context().Value(sessionKey).(store.Session)
	return v
}
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func containsIP(networks []*net.IPNet, ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) && u.Scheme == "https"
}
func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}
func dockerErrors(errors []string) []string {
	var result []string
	for _, message := range errors {
		if strings.HasPrefix(message, "Docker: ") {
			result = append(result, strings.TrimPrefix(message, "Docker: "))
		}
	}
	return result
}
func (server *Server) alertValue(source string, s model.Snapshot) (float64, bool) {
	switch source {
	case "cpu":
		return s.CPU.Usage, true
	case "memory":
		return percent(s.Memory.Used, s.Memory.Total), true
	case "swap":
		return percent(s.Memory.SwapUsed, s.Memory.SwapTotal), true
	case "load":
		return s.CPU.Load[0], true
	case "disk":
		var max float64
		for _, d := range s.Disks {
			if d.UsedPercent > max {
				max = d.UsedPercent
			}
		}
		return max, true
	case "processes":
		return float64(len(s.Processes)), true
	case "containers":
		return float64(len(s.Containers)), true
	case "network_total_1h":
		return server.store.NetworkTotal(time.Now().Add(-time.Hour), "total")
	case "network_total_24h":
		return server.store.NetworkTotal(time.Now().Add(-24*time.Hour), "total")
	case "network_rx_24h":
		return server.store.NetworkTotal(time.Now().Add(-24*time.Hour), "rx")
	case "network_tx_24h":
		return server.store.NetworkTotal(time.Now().Add(-24*time.Hour), "tx")
	}
	return 0, false
}
func compare(value float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	}
	return false
}
func validAlertRule(r store.AlertRule) bool {
	return r.Name != "" && (r.Source == "cpu" || r.Source == "memory" || r.Source == "swap" || r.Source == "load" || r.Source == "disk" || r.Source == "processes" || r.Source == "containers" || r.Source == "network_total_1h" || r.Source == "network_total_24h" || r.Source == "network_rx_24h" || r.Source == "network_tx_24h") && (r.Operator == ">" || r.Operator == ">=" || r.Operator == "<" || r.Operator == "<=" || r.Operator == "==") && r.Threshold >= 0 && r.ForSeconds >= 0 && r.CooldownSeconds >= 0
}

var telegramChatIDPattern = regexp.MustCompile(`^-?[0-9]{1,20}$`)
var telegramTokenPattern = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{20,128}$`)

func validNotificationTarget(name, provider, chatID, token string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 128 && provider == "telegram" &&
		telegramChatIDPattern.MatchString(chatID) && telegramTokenPattern.MatchString(token)
}
func redactParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range params {
		if strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "password") {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}
func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func spaHandler(files http.FileSystem) http.Handler {
	fileServer := http.FileServer(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if path != "." {
			f, err := files.Open(path)
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, err := files.Open("index.html")
		if err != nil {
			http.Error(w, "application unavailable", http.StatusInternalServerError)
			return
		}
		defer index.Close()
		data, err := io.ReadAll(io.LimitReader(index, 1<<20))
		if err != nil {
			http.Error(w, "application unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}
