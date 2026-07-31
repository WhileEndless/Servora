package app

import (
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
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
