package app

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen                         string
	TLSCert                        string
	TLSKey                         string
	DataDir                        string
	AgentSocket                    string
	NetworkBPFObject               string
	AgentGroup                     string
	WebUser                        string
	AdminGroup                     string
	PAMService                     string
	AllowedCIDRs                   []*net.IPNet
	TrustedProxies                 []*net.IPNet
	SessionIdle                    time.Duration
	SessionAbsolute                time.Duration
	SampleInterval                 time.Duration
	RawRetention                   time.Duration
	RollupRetention                time.Duration
	MaxDatabaseBytes               int64
	ServiceAllowlist               map[string]bool
	ProtectedServices              map[string]bool
	JobExecutables                 map[string]bool
	MaintenanceService             string
	PackageScanInterval            time.Duration
	PackageMetadataRefreshInterval time.Duration
	PackageEventRetention          time.Duration
}

func DefaultConfig() Config {
	return Config{
		Listen:                         "0.0.0.0:8443",
		AllowedCIDRs:                   loopbackCIDRs(),
		TLSCert:                        "/etc/system-maintenance/tls/server.crt",
		TLSKey:                         "/etc/system-maintenance/tls/server.key",
		DataDir:                        "/var/lib/system-maintenance-monitor",
		AgentSocket:                    "/run/system-maintenance/agent.sock",
		NetworkBPFObject:               "/opt/system-maintenance/lib/network_accounting.bpf.o",
		AgentGroup:                     "system-maintenance-agent",
		WebUser:                        "system-maintenance",
		AdminGroup:                     "system-maintenance-admin",
		PAMService:                     "system-maintenance",
		SessionIdle:                    30 * time.Minute,
		SessionAbsolute:                12 * time.Hour,
		SampleInterval:                 time.Second,
		RawRetention:                   30 * 24 * time.Hour,
		RollupRetention:                365 * 24 * time.Hour,
		MaxDatabaseBytes:               2 << 30,
		MaintenanceService:             "system-maintenance.service",
		PackageScanInterval:            15 * time.Minute,
		PackageMetadataRefreshInterval: 12 * time.Hour,
		PackageEventRetention:          365 * 24 * time.Hour,
		ServiceAllowlist: map[string]bool{
			"system-maintenance.service": true,
		},
		ProtectedServices: map[string]bool{
			"system-maintenance-monitor.service": true,
			"system-maintenance-agent.service":   true,
			"sshd.service":                       true,
			"ssh.service":                        true,
			"systemd-logind.service":             true,
			"NetworkManager.service":             true,
			"systemd-networkd.service":           true,
		},
		JobExecutables: map[string]bool{
			"/opt/system-maintenance/bin/system-maintenance": true,
		},
	}
}

// loopbackCIDRs is the fail-closed default for ALLOWED_CIDRS. A config that
// omits the key grants local access only, rather than exposing the service to
// a network the operator never named. The installer always writes the key.
func loopbackCIDRs() []*net.IPNet {
	var out []*net.IPNet
	for _, item := range []string{"127.0.0.1/32", "::1/128"} {
		if _, network, err := net.ParseCIDR(item); err == nil {
			out = append(out, network)
		}
	}
	return out
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	cfg.AllowedCIDRs = loopbackCIDRs()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("invalid config line: %q", line)
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	set := func(key string, dst *string) {
		if v, ok := values[key]; ok {
			*dst = v
		}
	}
	set("LISTEN", &cfg.Listen)
	set("TLS_CERT", &cfg.TLSCert)
	set("TLS_KEY", &cfg.TLSKey)
	set("DATA_DIR", &cfg.DataDir)
	set("AGENT_SOCKET", &cfg.AgentSocket)
	set("NETWORK_BPF_OBJECT", &cfg.NetworkBPFObject)
	set("AGENT_GROUP", &cfg.AgentGroup)
	set("WEB_USER", &cfg.WebUser)
	set("ADMIN_GROUP", &cfg.AdminGroup)
	set("PAM_SERVICE", &cfg.PAMService)
	set("MAINTENANCE_SERVICE", &cfg.MaintenanceService)
	if v := values["ALLOWED_CIDRS"]; v != "" {
		cfg.AllowedCIDRs, err = parseCIDRs(v)
		if err != nil {
			return cfg, fmt.Errorf("ALLOWED_CIDRS: %w", err)
		}
	}
	if v := values["TRUSTED_PROXIES"]; v != "" {
		cfg.TrustedProxies, err = parseCIDRs(v)
		if err != nil {
			return cfg, fmt.Errorf("TRUSTED_PROXIES: %w", err)
		}
	}
	if v := values["SERVICE_ALLOWLIST"]; v != "" {
		cfg.ServiceAllowlist = csvSet(v)
	}
	if v := values["PROTECTED_SERVICES"]; v != "" {
		for item := range csvSet(v) {
			cfg.ProtectedServices[item] = true
		}
	}
	if v := values["JOB_EXECUTABLES"]; v != "" {
		cfg.JobExecutables = csvSet(v)
	}
	if v := values["MAX_DATABASE_MB"]; v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 64 {
			return cfg, fmt.Errorf("MAX_DATABASE_MB must be at least 64")
		}
		cfg.MaxDatabaseBytes = n << 20
	}
	if v := values["SAMPLE_INTERVAL"]; v != "" {
		duration, parseErr := time.ParseDuration(v)
		if parseErr != nil || duration < 500*time.Millisecond || duration > time.Minute {
			return cfg, fmt.Errorf("SAMPLE_INTERVAL must be between 500ms and 1m")
		}
		cfg.SampleInterval = duration
	}
	if v := values["PACKAGE_SCAN_INTERVAL"]; v != "" {
		duration, parseErr := time.ParseDuration(v)
		if parseErr != nil || duration < 5*time.Minute || duration > 24*time.Hour {
			return cfg, fmt.Errorf("PACKAGE_SCAN_INTERVAL must be between 5m and 24h")
		}
		cfg.PackageScanInterval = duration
	}
	if v := values["PACKAGE_METADATA_REFRESH_INTERVAL"]; v != "" {
		duration, parseErr := time.ParseDuration(v)
		if parseErr != nil || duration < time.Hour || duration > 7*24*time.Hour {
			return cfg, fmt.Errorf("PACKAGE_METADATA_REFRESH_INTERVAL must be between 1h and 168h")
		}
		cfg.PackageMetadataRefreshInterval = duration
	}
	if v := values["PACKAGE_EVENT_RETENTION_DAYS"]; v != "" {
		days, parseErr := strconv.Atoi(v)
		if parseErr != nil || days < 1 || days > 3650 {
			return cfg, fmt.Errorf("PACKAGE_EVENT_RETENTION_DAYS must be between 1 and 3650")
		}
		cfg.PackageEventRetention = time.Duration(days) * 24 * time.Hour
	}
	return cfg, nil
}

func parseCIDRs(value string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if ip := net.ParseIP(item); ip != nil {
			if ip.To4() != nil {
				item += "/32"
			} else {
				item += "/128"
			}
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, err
		}
		out = append(out, network)
	}
	return out, nil
}

func csvSet(value string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out[item] = true
		}
	}
	return out
}

func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "monitor.db") }
func (c Config) SecretDir() string    { return filepath.Join(c.DataDir, "secrets") }

func (c Config) IsAdmin(username string) bool {
	u, err := user.Lookup(username)
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	group, err := user.LookupGroup(c.AdminGroup)
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == group.Gid {
			return true
		}
	}
	return false
}
