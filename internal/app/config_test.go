package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WhileEndless/Servora/internal/store"
)

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.conf")
	data := []byte("LISTEN=127.0.0.1:9443\nALLOWED_CIDRS=127.0.0.1/32,10.0.0.0/8\nMAX_DATABASE_MB=128\nSAMPLE_INTERVAL=750ms\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9443" || cfg.MaxDatabaseBytes != 128<<20 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.SampleInterval != 750*time.Millisecond {
		t.Fatalf("unexpected sample interval: %v", cfg.SampleInterval)
	}
	if !containsIP(cfg.AllowedCIDRs, net.ParseIP("10.20.30.40")) {
		t.Fatal("configured network was not accepted")
	}
}

func TestAlertRuleValidation(t *testing.T) {
	valid := store.AlertRule{Name: "RAM", Source: "memory", Operator: ">=", Threshold: 90}
	if !validAlertRule(valid) {
		t.Fatal("valid alert rule rejected")
	}
	valid.Operator = "exec"
	if validAlertRule(valid) {
		t.Fatal("invalid operator accepted")
	}
}

func TestHealthAndEmbeddedApplication(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	_, allowed, _ := net.ParseCIDR("192.0.2.0/24")
	cfg.AllowedCIDRs = []*net.IPNet{allowed}
	server, err := NewServer(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	for _, path := range []string{"/api/v1/health", "/", "/docker", "/processes"} {
		request := httptest.NewRequest("GET", "https://monitor.example"+path, nil)
		request.RemoteAddr = "192.0.2.10:12345"
		response := httptest.NewRecorder()
		server.http.Handler.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
	protected := httptest.NewRequest("GET", "https://monitor.example/api/v1/overview", nil)
	protected.RemoteAddr = "192.0.2.10:12345"
	protectedResponse := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(protectedResponse, protected)
	if protectedResponse.Code != 401 {
		t.Fatalf("protected endpoint returned %d without a session", protectedResponse.Code)
	}

	blocked := httptest.NewRequest("GET", "https://monitor.example/api/v1/health", nil)
	blocked.RemoteAddr = "198.51.100.10:12345"
	blockedResponse := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != 403 {
		t.Fatalf("disallowed network returned %d", blockedResponse.Code)
	}
}

func TestAllAPIRoutesRequireAuthenticationByDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	_, allowed, _ := net.ParseCIDR("192.0.2.0/24")
	cfg.AllowedCIDRs = []*net.IPNet{allowed}
	server, err := NewServer(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	protected := []string{
		"/api/v1/auth/logout", "/api/v1/auth/session", "/api/v1/overview",
		"/api/v1/processes", "/api/v1/processes/123", "/api/v1/network",
		"/api/v1/network-usage", "/api/v1/network-usage/detail", "/api/v1/resource-usage",
		"/api/v1/resource-usage/detail", "/api/v1/settings/network", "/api/v1/settings/resources",
		"/api/v1/ssh", "/api/v1/docker", "/api/v1/docker/images", "/api/v1/services",
		"/api/v1/packages", "/api/v1/packages/refresh", "/api/v1/packages/example",
		"/api/v1/package-events", "/api/v1/schedules", "/api/v1/history", "/api/v1/watches",
		"/api/v1/watches/12345678", "/api/v1/alert-rules", "/api/v1/alert-rules/12345678",
		"/api/v1/alerts", "/api/v1/alerts/12345678/acknowledge", "/api/v1/notification-targets",
		"/api/v1/notification-targets/12345678", "/api/v1/activities", "/api/v1/actions",
		"/api/v1/stream", "/api/v1/modules", "/api/v1/not-a-real-endpoint", "/api/not-a-real-endpoint",
	}
	for _, path := range protected {
		request := httptest.NewRequest("GET", "https://monitor.example"+path, nil)
		request.RemoteAddr = "192.0.2.10:12345"
		response := httptest.NewRecorder()
		server.http.Handler.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Errorf("%s returned %d without a session, want 401", path, response.Code)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestStreamClosesWhenSessionIsDeleted(t *testing.T) {
	previousInterval := streamSessionCheckInterval
	streamSessionCheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { streamSessionCheckInterval = previousInterval })

	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	_, allowed, _ := net.ParseCIDR("192.0.2.0/24")
	cfg.AllowedCIDRs = []*net.IPNet{allowed}
	server, err := NewServer(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if err := server.store.CreateSession("stream-token", "alice", "csrf", "192.0.2.10", time.Now()); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "https://monitor.example/api/v1/stream", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	request.AddCookie(&http.Cookie{Name: "sms_session", Value: "stream-token"})
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.http.Handler.ServeHTTP(response, request)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	server.store.DeleteSession("stream-token")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream remained open after its session was deleted")
	}
	if !strings.Contains(response.Body.String(), "event: auth-expired") {
		t.Fatalf("stream did not announce session expiry: %s", response.Body.String())
	}
}

func TestNotificationTargetValidation(t *testing.T) {
	validToken := "123456789:" + "abcdefghijklmnopqrstuvwxyz_ABCD"
	if !validNotificationTarget("Operations", "telegram", "-1001234567890", validToken) {
		t.Fatal("valid Telegram destination rejected")
	}
	for _, token := range []string{"", "not-a-token", "12345:short", "123456:token/with/slash/that/is/not/valid"} {
		if validNotificationTarget("Operations", "telegram", "123456", token) {
			t.Errorf("invalid Telegram token accepted: %q", token)
		}
	}
}
