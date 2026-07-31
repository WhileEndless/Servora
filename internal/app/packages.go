package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

func pageParams(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 500 {
		perPage = 100
	}
	return page, perPage
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, perPage := pageParams(r)
	status := r.URL.Query().Get("status")
	if status == "all" {
		status = ""
	}
	if status == "updates" {
		status = "update_available"
	}
	if status != "" && status != "update_available" && status != "unknown" && status != "current" {
		writeError(w, 400, "invalid_filter", "Invalid package status")
		return
	}
	items, total, err := s.store.Packages(r.URL.Query().Get("q"), status,
		r.URL.Query().Get("manager"), r.URL.Query().Get("sort"),
		r.URL.Query().Get("order"), page, perPage)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	count, updates, unknown, err := s.store.PackageCounts()
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	state, _ := s.store.PackageScanState()
	s.mu.RLock()
	refreshing := s.packageRefreshing
	s.mu.RUnlock()
	writeJSON(w, 200, map[string]any{
		"items": items, "total": total, "page": page, "per_page": perPage,
		"summary": map[string]int{"installed": count, "updates": updates, "unknown": unknown},
		"status": map[string]any{
			"hostname": state.Hostname, "manager": state.Manager,
			"inventory_available":    state.InventoryAvailable,
			"update_check_available": state.UpdateCheckAvailable,
			"inventory_scanned_at":   state.InventoryScannedAt,
			"metadata_refreshed_at":  state.MetadataRefreshedAt,
			"refreshing":             refreshing, "error": state.Error,
		},
	})
}

func (s *Server) packageItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/packages/")
	id := strings.TrimSuffix(path, "/files")
	item, err := s.store.Package(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "package_not_found", "Package not found")
		return
	}
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !strings.HasSuffix(path, "/files") {
		writeJSON(w, 200, item)
		return
	}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
		"http://agent/v1/packages/files/"+url.PathEscape(id), nil)
	response, err := s.agent.Do(request)
	if err != nil {
		writeError(w, 502, "agent_error", "Package file list is unavailable")
		return
	}
	defer response.Body.Close()
	var payload struct {
		Items []string `json:"items"`
		Error string   `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&payload); err != nil {
		writeError(w, 502, "agent_error", "Agent returned invalid package files")
		return
	}
	if response.StatusCode/100 != 2 {
		writeError(w, 502, "agent_error", payload.Error)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := make([]string, 0, len(payload.Items))
	for _, file := range payload.Items {
		if query == "" || strings.Contains(strings.ToLower(file), query) {
			filtered = append(filtered, file)
		}
	}
	page, perPage := pageParams(r)
	start := (page - 1) * perPage
	if start > len(filtered) {
		start = len(filtered)
	}
	end := min(start+perPage, len(filtered))
	writeJSON(w, 200, map[string]any{
		"items": filtered[start:end], "total": len(filtered), "page": page,
		"per_page": perPage, "package": item,
	})
}

func (s *Server) packageEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	from := time.Now().Add(-30 * 24 * time.Hour)
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, 400, "invalid_time", "Invalid from time")
			return
		}
		from = parsed
	}
	eventType := r.URL.Query().Get("type")
	if eventType != "" && eventType != "installed" && eventType != "removed" && eventType != "version_changed" {
		writeError(w, 400, "invalid_filter", "Invalid event type")
		return
	}
	page, perPage := pageParams(r)
	items, total, err := s.store.PackageEvents(from, r.URL.Query().Get("q"), eventType, page, perPage)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "per_page": perPage})
}

func (s *Server) packageRefreshRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.mu.Lock()
	alreadyRunning := s.packageRefreshing
	if !alreadyRunning {
		s.packageRefreshing = true
		select {
		case s.packageRefresh <- true:
		default:
			alreadyRunning = true
		}
	}
	s.mu.Unlock()
	s.auditResult(r, "package.metadata_refresh", "", map[string]bool{"already_running": alreadyRunning}, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": !alreadyRunning, "already_running": alreadyRunning})
}

func (s *Server) packageLoop() {
	s.runPackageScan(false)
	state, _ := s.store.PackageScanState()
	if state.MetadataRefreshedAt.IsZero() || time.Since(state.MetadataRefreshedAt) >= s.cfg.PackageMetadataRefreshInterval {
		s.runPackageScan(true)
	}
	scanTicker := time.NewTicker(s.cfg.PackageScanInterval)
	metadataTicker := time.NewTicker(s.cfg.PackageMetadataRefreshInterval)
	defer scanTicker.Stop()
	defer metadataTicker.Stop()
	for {
		select {
		case <-scanTicker.C:
			s.runPackageScan(false)
		case <-metadataTicker.C:
			s.runPackageScan(true)
		case refresh := <-s.packageRefresh:
			s.runPackageScan(refresh)
		case <-s.stop:
			return
		}
	}
}

func (s *Server) runPackageScan(refreshMetadata bool) {
	s.mu.Lock()
	s.packageRefreshing = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.packageRefreshing = false
		s.mu.Unlock()
	}()
	raw, _ := json.Marshal(map[string]bool{"refresh_metadata": refreshMetadata})
	ctx, cancel := contextWithTimeoutOrStop(s.stop, 10*time.Minute)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/packages/scan", strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	response, err := s.packageAgent.Do(request)
	if err != nil {
		_ = s.store.SetPackageScanError(err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		var failure map[string]string
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&failure)
		_ = s.store.SetPackageScanError(failure["error"])
		return
	}
	var scan model.PackageScan
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&scan); err != nil {
		_ = s.store.SetPackageScanError(err.Error())
		return
	}
	_ = s.store.SavePackageScan(scan)
	_ = s.store.PrunePackageEvents(time.Now().Add(-s.cfg.PackageEventRetention))
}

func contextWithTimeoutOrStop(stop <-chan struct{}, duration time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (s *Server) dockerImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://agent/v1/docker/images", nil)
	response, err := s.agent.Do(request)
	if err != nil {
		writeJSON(w, 200, map[string]any{"available": false, "items": []model.DockerImage{}, "errors": []string{err.Error()}})
		return
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		var failure map[string]string
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&failure)
		writeJSON(w, 200, map[string]any{"available": false, "items": []model.DockerImage{}, "errors": []string{failure["error"]}})
		return
	}
	var payload struct {
		Items     []model.DockerImage `json:"items"`
		Freshness time.Time           `json:"freshness"`
		Available bool                `json:"available"`
		Error     string              `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&payload); err != nil {
		writeError(w, 502, "agent_error", "Agent returned invalid Docker images")
		return
	}
	errors := []string{}
	if payload.Error != "" {
		errors = append(errors, payload.Error)
	}
	writeJSON(w, 200, map[string]any{"available": payload.Available, "items": payload.Items, "freshness": payload.Freshness, "errors": errors})
}
