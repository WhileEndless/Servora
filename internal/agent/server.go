package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/WhileEndless/Servora/internal/app"
	"github.com/WhileEndless/Servora/internal/auth"
	"github.com/WhileEndless/Servora/internal/model"
)

type Server struct {
	socket           string
	cfg              app.Config
	collector        *Collector
	http             *http.Server
	dockerImageMu    sync.Mutex
	dockerImageItems []model.DockerImage
	dockerImagesAt   time.Time
}

func New(socket string, cfg app.Config) *Server {
	collector := NewCollector(cfg.ServiceAllowlist, cfg.ProtectedServices)
	if err := collector.EnableBPF(cfg.NetworkBPFObject); err != nil {
		log.Printf("network attribution: degraded socket-counter fallback: %v", err)
	} else {
		log.Printf("network attribution: exact eBPF accounting enabled")
	}
	s := &Server{socket: socket, cfg: cfg, collector: collector}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshot", s.snapshot)
	mux.HandleFunc("/v1/process/", s.processDetail)
	mux.HandleFunc("/v1/packages/scan", s.packages)
	mux.HandleFunc("/v1/packages/files/", s.packageFiles)
	mux.HandleFunc("/v1/docker/images", s.dockerImages)
	mux.HandleFunc("/v1/action", s.action)
	mux.HandleFunc("/v1/auth", s.authenticate)
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      11 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	return s
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		RefreshMetadata bool `json:"refresh_metadata"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := scanPackages(ctx, input.RefreshMetadata)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) packageFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/packages/files/")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	files, err := packageFiles(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files})
}

func (s *Server) dockerImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.dockerImageMu.Lock()
	defer s.dockerImageMu.Unlock()
	if !s.dockerImagesAt.IsZero() && time.Since(s.dockerImagesAt) < time.Minute {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": true, "items": s.dockerImageItems,
			"freshness": s.dockerImagesAt, "error": "",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, checked, err := readDockerImages(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": len(s.dockerImageItems) > 0, "items": s.dockerImageItems,
			"freshness": s.dockerImagesAt, "error": err.Error(),
		})
		return
	}
	s.dockerImageItems, s.dockerImagesAt = items, checked
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true, "items": items, "freshness": checked, "error": "",
	})
}

func (s *Server) processDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rawPID := strings.TrimPrefix(r.URL.Path, "/v1/process/")
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid PID"})
		return
	}
	detail, err := s.collector.ProcessDetail(pid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remote   string `json:"remote"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false})
		return
	}
	authenticator := auth.PAM{Service: s.cfg.PAMService}
	if err := authenticator.Authenticate(input.Username, input.Password, input.Remote); err != nil ||
		!s.cfg.IsAdmin(input.Username) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) Run() error {
	defer s.collector.Close()
	if err := os.MkdirAll(filepath.Dir(s.socket), 0750); err != nil {
		return err
	}
	if fi, err := os.Lstat(s.socket); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket %s", s.socket)
		}
		if err := os.Remove(s.socket); err != nil {
			return err
		}
	}
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.socket, 0660); err != nil {
		listener.Close()
		return err
	}
	if group, err := user.LookupGroup(s.cfg.AgentGroup); err == nil {
		if gid, convErr := strconv.Atoi(group.Gid); convErr == nil {
			if err := os.Chown(s.socket, 0, gid); err != nil {
				listener.Close()
				return fmt.Errorf("set socket group: %w", err)
			}
		}
	}
	webUser, err := user.Lookup(s.cfg.WebUser)
	if err != nil {
		listener.Close()
		return fmt.Errorf("look up web service user: %w", err)
	}
	webUID, err := strconv.ParseUint(webUser.Uid, 10, 32)
	if err != nil {
		listener.Close()
		return fmt.Errorf("parse web service UID: %w", err)
	}
	listener = &peerListener{Listener: listener, allowedUID: uint32(webUID)}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}()
	log.Printf("privileged agent listening on %s", s.socket)
	err = s.http.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type peerListener struct {
	net.Listener
	allowedUID uint32
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConnection, ok := connection.(*net.UnixConn)
		if !ok {
			connection.Close()
			continue
		}
		raw, err := unixConnection.SyscallConn()
		if err != nil {
			connection.Close()
			continue
		}
		var credential *syscall.Ucred
		var controlErr error
		if err := raw.Control(func(fd uintptr) {
			credential, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		}); err != nil || controlErr != nil || credential == nil {
			connection.Close()
			continue
		}
		if credential.Uid == 0 || credential.Uid == l.allowedUID {
			return connection, nil
		}
		log.Printf("rejected agent socket peer uid=%d", credential.Uid)
		connection.Close()
	}
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.collector.Snapshot(ctx))
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var req model.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.ActionResponse{Message: "invalid request"})
		return
	}
	message, err := s.execute(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.ActionResponse{Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.ActionResponse{OK: true, Message: message})
}

func (s *Server) execute(parent context.Context, req model.ActionRequest) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	switch req.Action {
	case "process.signal":
		pid, err := strconv.Atoi(req.Target)
		if err != nil || protectedPID(pid) {
			return "", errors.New("protected or invalid PID")
		}
		sigMap := map[string]syscall.Signal{
			"TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL,
			"STOP": syscall.SIGSTOP, "CONT": syscall.SIGCONT,
		}
		sig, ok := sigMap[req.Params["signal"]]
		if !ok {
			return "", errors.New("unsupported signal")
		}
		if err := syscall.Kill(pid, sig); err != nil {
			return "", err
		}
		return fmt.Sprintf("signal %s sent to PID %d", req.Params["signal"], pid), nil
	case "process.renice":
		pid, err := strconv.Atoi(req.Target)
		nice, nerr := strconv.Atoi(req.Params["nice"])
		if err != nil || nerr != nil || protectedPID(pid) || nice < -20 || nice > 19 {
			return "", errors.New("invalid PID or nice value")
		}
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, nice); err != nil {
			return "", err
		}
		return "priority updated", nil
	case "service":
		if !validUnit(req.Target) || !s.cfg.ServiceAllowlist[req.Target] {
			return "", errors.New("service is not in the action allowlist")
		}
		if s.cfg.ProtectedServices[req.Target] {
			return "", errors.New("service is protected")
		}
		verb := req.Params["verb"]
		if verb != "start" && verb != "stop" && verb != "restart" {
			return "", errors.New("unsupported service action")
		}
		return run(ctx, "systemctl", verb, req.Target)
	case "maintenance.run":
		return run(ctx, "systemctl", "start", s.cfg.MaintenanceService)
	case "docker":
		verb := req.Params["verb"]
		allowed := map[string]bool{"start": true, "stop": true, "restart": true, "pause": true, "unpause": true}
		if !allowed[verb] || !validDockerID(req.Target) {
			return "", errors.New("invalid Docker action")
		}
		return run(ctx, "docker", verb, req.Target)
	case "power.reboot":
		if req.Params["confirm"] != "REBOOT" {
			return "", errors.New("reboot confirmation is required")
		}
		go delayedCommand("systemctl", "reboot")
		return "reboot scheduled", nil
	case "power.shutdown":
		if req.Params["confirm"] != "SHUTDOWN" {
			return "", errors.New("shutdown confirmation is required")
		}
		go delayedCommand("systemctl", "poweroff")
		return "shutdown scheduled", nil
	case "schedule.create":
		return s.createSchedule(ctx, req)
	case "schedule.delete":
		return s.deleteSchedule(ctx, req.Target)
	case "schedule.toggle":
		if !validJobName(req.Target) {
			return "", errors.New("invalid job name")
		}
		verb := "disable"
		if req.Params["enabled"] == "true" {
			verb = "enable"
		}
		return run(ctx, "systemctl", verb, "--now", jobUnit(req.Target)+".timer")
	case "schedule.run":
		if !validJobName(req.Target) {
			return "", errors.New("invalid job name")
		}
		return run(ctx, "systemctl", "start", jobUnit(req.Target)+".service")
	case "secret.write":
		return s.writeSecret(req.Target, req.Params["value"])
	case "secret.delete":
		return s.deleteSecret(req.Target)
	default:
		return "", errors.New("unsupported action")
	}
}

var unitPattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]+\.(service|socket|timer)$`)
var dockerPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var jobPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
var secretPattern = regexp.MustCompile(`^[a-f0-9-]{8,64}$`)

func validUnit(v string) bool     { return unitPattern.MatchString(v) }
func validDockerID(v string) bool { return dockerPattern.MatchString(v) }
func validJobName(v string) bool  { return jobPattern.MatchString(v) }
func jobUnit(v string) string     { return "system-maintenance-job-" + v }

func protectedPID(pid int) bool {
	if pid <= 1 || pid == os.Getpid() {
		return true
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return true
	}
	name := strings.TrimSpace(string(raw))
	protected := map[string]bool{
		"systemd": true, "sshd": true, "systemd-logind": true,
		"NetworkManager": true, "systemd-network": true,
		"system-maintena": true,
	}
	return protected[name]
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", errors.New(text)
	}
	if text == "" {
		text = "operation completed"
	}
	return text, nil
}

func delayedCommand(name string, args ...string) {
	time.Sleep(750 * time.Millisecond)
	_ = exec.Command(name, args...).Start()
}

func (s *Server) createSchedule(ctx context.Context, req model.ActionRequest) (string, error) {
	name := req.Target
	executable := req.Params["executable"]
	calendar := req.Params["calendar"]
	runAs := req.Params["user"]
	if !validJobName(name) || !s.cfg.JobExecutables[executable] {
		return "", errors.New("invalid job name or executable is not allowlisted")
	}
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\r\n\x00") {
		return "", errors.New("invalid executable")
	}
	if runAs == "" {
		runAs = "root"
	}
	if _, err := user.Lookup(runAs); err != nil {
		return "", errors.New("unknown run-as user")
	}
	if calendar == "" || strings.ContainsAny(calendar, "\r\n\x00") {
		return "", errors.New("invalid calendar")
	}
	if _, err := run(ctx, "systemd-analyze", "calendar", calendar); err != nil {
		return "", fmt.Errorf("invalid calendar: %w", err)
	}
	args, err := parseArgs(req.Params["args"])
	if err != nil {
		return "", err
	}
	execLine := systemdQuote(executable)
	for _, arg := range args {
		execLine += " " + systemdQuote(arg)
	}
	base := jobUnit(name)
	service := "[Unit]\nDescription=System Maintenance managed job: " + name +
		"\n\n[Service]\nType=oneshot\nUser=" + runAs +
		"\nExecStart=" + execLine + "\nTimeoutStartSec=1h\nNoNewPrivileges=true\nPrivateTmp=true\n"
	timer := "[Unit]\nDescription=Schedule for " + base +
		"\n\n[Timer]\nOnCalendar=" + calendar +
		"\nPersistent=true\nUnit=" + base + ".service\n\n[Install]\nWantedBy=timers.target\n"
	dir := "/etc/systemd/system"
	if err := atomicWrite(filepath.Join(dir, base+".service"), []byte(service), 0644); err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(dir, base+".timer"), []byte(timer), 0644); err != nil {
		return "", err
	}
	if _, err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return "", err
	}
	return run(ctx, "systemctl", "enable", "--now", base+".timer")
}

func (s *Server) deleteSchedule(ctx context.Context, name string) (string, error) {
	if !validJobName(name) {
		return "", errors.New("invalid job name")
	}
	base := jobUnit(name)
	_, _ = run(ctx, "systemctl", "disable", "--now", base+".timer")
	for _, suffix := range []string{".timer", ".service"} {
		path := filepath.Join("/etc/systemd/system", base+suffix)
		if fi, err := os.Lstat(path); err == nil {
			if !fi.Mode().IsRegular() {
				return "", errors.New("refusing to remove non-regular unit")
			}
			if err := os.Remove(path); err != nil {
				return "", err
			}
		}
	}
	return run(ctx, "systemctl", "daemon-reload")
}

func parseArgs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, errors.New("args must be a JSON string array")
	}
	if len(args) > 64 {
		return nil, errors.New("too many arguments")
	}
	for _, arg := range args {
		if len(arg) > 4096 || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, errors.New("invalid argument")
		}
	}
	return args, nil
}

func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(value) + `"`
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".system-maintenance-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *Server) writeSecret(id, value string) (string, error) {
	if !secretPattern.MatchString(id) || value == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid secret")
	}
	if err := os.MkdirAll(s.cfg.SecretDir(), 0750); err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(s.cfg.SecretDir(), id), []byte(value), 0640); err != nil {
		return "", err
	}
	return "secret stored", nil
}

func (s *Server) deleteSecret(id string) (string, error) {
	if !secretPattern.MatchString(id) {
		return "", errors.New("invalid secret id")
	}
	err := os.Remove(filepath.Join(s.cfg.SecretDir(), id))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return "secret deleted", nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
