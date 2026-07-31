package app

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

func TestAuthenticatedMonitoringAPI(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	agent := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/snapshot":
			_ = json.NewEncoder(w).Encode(model.Snapshot{
				Timestamp: time.Now(), Hostname: "test-host", Uptime: 100,
				CPU:          model.CPU{Usage: 12, Cores: 4},
				Memory:       model.Memory{Total: 100, Used: 25},
				Processes:    []model.Process{{PID: 123, Name: "worker"}},
				Capabilities: map[string]bool{"systemd": true},
				Freshness:    map[string]time.Time{"fast": time.Now()},
			})
		case r.URL.Path == "/v1/process/123":
			_ = json.NewEncoder(w).Encode(model.ProcessDetail{
				Process:    model.Process{PID: 123, Name: "worker"},
				Executable: "/usr/bin/worker",
			})
		case r.URL.Path == "/v1/action":
			_ = json.NewEncoder(w).Encode(model.ActionResponse{OK: true, Message: "done"})
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = agent.Serve(listener) }()
	t.Cleanup(func() { _ = agent.Close() })

	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.AgentSocket = socket
	_, allowed, _ := net.ParseCIDR("192.0.2.0/24")
	cfg.AllowedCIDRs = []*net.IPNet{allowed}
	server, err := NewServer(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	server.collect()
	if err := server.store.CreateSession("token", "alice", "csrf", "192.0.2.10", time.Now()); err != nil {
		t.Fatal(err)
	}

	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://monitor.example"+path, strings.NewReader(body))
		req.RemoteAddr = "192.0.2.10:1234"
		req.AddCookie(&http.Cookie{Name: "sms_session", Value: "token"})
		if method != http.MethodGet {
			req.Header.Set("Origin", "https://monitor.example")
			req.Header.Set("X-CSRF-Token", "csrf")
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		server.http.Handler.ServeHTTP(response, req)
		return response
	}

	for _, path := range []string{"/api/v1/overview", "/api/v1/processes", "/api/v1/processes/123", "/api/v1/network", "/api/v1/network-usage", "/api/v1/resource-usage", "/api/v1/resource-usage/detail?value=sshd", "/api/v1/settings/network", "/api/v1/settings/resources", "/api/v1/services", "/api/v1/docker", "/api/v1/packages", "/api/v1/package-events", "/api/v1/schedules", "/api/v1/alerts", "/api/v1/notification-targets"} {
		response := request(http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
	sessionResponse := request(http.MethodGet, "/api/v1/auth/session", "")
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), `"username":"alice"`) {
		t.Fatalf("persistent session restore failed: %d %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	dockerResponse := request(http.MethodGet, "/api/v1/docker", "")
	var dockerPayload struct {
		Available bool                `json:"available"`
		Summary   model.DockerSummary `json:"summary"`
	}
	if err := json.Unmarshal(dockerResponse.Body.Bytes(), &dockerPayload); err != nil {
		t.Fatalf("invalid Docker API response: %v", err)
	}
	action := request(http.MethodPost, "/api/v1/actions", `{"action":"process.signal","target":"123","params":{"signal":"TERM"}}`)
	if action.Code != http.StatusOK {
		t.Fatalf("action returned %d: %s", action.Code, action.Body.String())
	}
}
