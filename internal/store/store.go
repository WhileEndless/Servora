package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/WhileEndless/Servora/internal/model"
)

type Store struct {
	db           *sql.DB
	ioSeen       map[string][2]uint64
	settingsMu   sync.RWMutex
	historyCache map[string]bool
}

type Session struct {
	User    string    `json:"user"`
	CSRF    string    `json:"csrf"`
	IP      string    `json:"ip"`
	Created time.Time `json:"created"`
	Seen    time.Time `json:"seen"`
}

type MetricPoint struct {
	Time       time.Time `json:"time"`
	CPU        float64   `json:"cpu"`
	Memory     float64   `json:"memory"`
	Swap       float64   `json:"swap"`
	Load       float64   `json:"load"`
	NetworkRX  uint64    `json:"network_rx"`
	NetworkTX  uint64    `json:"network_tx"`
	Disk       float64   `json:"disk"`
	Processes  int       `json:"processes"`
	Containers int       `json:"containers"`
}

type Watch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Field     string    `json:"field"`
	Pattern   string    `json:"pattern"`
	CreatedBy string    `json:"created_by"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type WatchPoint struct {
	Time       time.Time `json:"time"`
	CPU        float64   `json:"cpu"`
	Memory     uint64    `json:"memory"`
	ReadBytes  uint64    `json:"read_bytes"`
	WriteBytes uint64    `json:"write_bytes"`
	Instances  int       `json:"instances"`
}

type AlertRule struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Source          string  `json:"source"`
	Operator        string  `json:"operator"`
	Severity        string  `json:"severity"`
	TargetIDs       string  `json:"target_ids"`
	Threshold       float64 `json:"threshold"`
	ForSeconds      float64 `json:"for_seconds"`
	CooldownSeconds float64 `json:"cooldown_seconds"`
	Enabled         bool    `json:"enabled"`
	NotifyRecovery  bool    `json:"notify_recovery"`
}

type Alert struct {
	ID        string    `json:"id"`
	RuleID    string    `json:"rule_id"`
	Name      string    `json:"name"`
	Severity  string    `json:"severity"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NotificationTarget struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	ChatID    string `json:"chat_id"`
	SecretRef string `json:"secret_ref"`
	Enabled   bool   `json:"enabled"`
}

type Audit struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	User       string    `json:"user"`
	IP         string    `json:"ip"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Parameters string    `json:"parameters"`
	Result     string    `json:"result"`
	Success    bool      `json:"success"`
}

type NetworkUsage struct {
	Key          string    `json:"key"`
	Process      string    `json:"process"`
	Group        string    `json:"group"`
	User         string    `json:"user"`
	PID          int       `json:"pid"`
	RXBytes      uint64    `json:"rx_bytes"`
	TXBytes      uint64    `json:"tx_bytes"`
	Destinations int       `json:"destinations"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

type NetworkDestination struct {
	RemoteIP   string    `json:"remote_ip"`
	RemotePort int       `json:"remote_port"`
	Protocol   string    `json:"protocol"`
	RXBytes    uint64    `json:"rx_bytes"`
	TXBytes    uint64    `json:"tx_bytes"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

type NetworkTimelinePoint struct {
	Time    time.Time `json:"time"`
	RXBytes uint64    `json:"rx_bytes"`
	TXBytes uint64    `json:"tx_bytes"`
}

type NetworkStorageInfo struct {
	RetentionDays  int   `json:"retention_days"`
	Rows           int64 `json:"rows"`
	Bytes          int64 `json:"bytes"`
	Oldest         int64 `json:"oldest"`
	Newest         int64 `json:"newest"`
	NetworkEnabled bool  `json:"network_enabled"`
	CPUEnabled     bool  `json:"cpu_enabled"`
	MemoryEnabled  bool  `json:"memory_enabled"`
	DiskIOEnabled  bool  `json:"disk_io_enabled"`
	ResourceRows   int64 `json:"resource_rows"`
	ResourceBytes  int64 `json:"resource_bytes"`
}

type ResourceUsage struct {
	Key        string    `json:"key"`
	Process    string    `json:"process"`
	Group      string    `json:"group"`
	User       string    `json:"user"`
	PID        int       `json:"pid"`
	CPUAverage float64   `json:"cpu_average"`
	CPUMax     float64   `json:"cpu_max"`
	MemoryAvg  uint64    `json:"memory_average"`
	MemoryMax  uint64    `json:"memory_max"`
	ReadBytes  uint64    `json:"read_bytes"`
	WriteBytes uint64    `json:"write_bytes"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

type ResourceTimelinePoint struct {
	Time       time.Time `json:"time"`
	CPUAverage float64   `json:"cpu_average"`
	CPUMax     float64   `json:"cpu_max"`
	MemoryAvg  uint64    `json:"memory_average"`
	MemoryMax  uint64    `json:"memory_max"`
	ReadBytes  uint64    `json:"read_bytes"`
	WriteBytes uint64    `json:"write_bytes"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	s := &Store{db: db, ioSeen: make(map[string][2]uint64), historyCache: make(map[string]bool)}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	s.loadHistorySettings()
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER PRIMARY KEY);
INSERT OR IGNORE INTO schema_version(version) VALUES(1);
CREATE TABLE IF NOT EXISTS metrics(
  ts INTEGER PRIMARY KEY, cpu REAL NOT NULL, memory REAL NOT NULL, swap REAL NOT NULL,
  load REAL NOT NULL, network_rx INTEGER NOT NULL, network_tx INTEGER NOT NULL,
  disk REAL NOT NULL, processes INTEGER NOT NULL, containers INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS metric_rollups(
  bucket INTEGER PRIMARY KEY, cpu_min REAL, cpu_avg REAL, cpu_max REAL,
  memory_min REAL, memory_avg REAL, memory_max REAL, swap_avg REAL,
  load_avg REAL, network_rx_avg INTEGER, network_tx_avg INTEGER, disk_max REAL,
  processes_avg REAL, containers_avg REAL
);
CREATE TABLE IF NOT EXISTS sessions(
  token_hash TEXT PRIMARY KEY, username TEXT NOT NULL, csrf TEXT NOT NULL, ip TEXT NOT NULL,
  created_at INTEGER NOT NULL, seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS login_failures(
  id INTEGER PRIMARY KEY, ip TEXT NOT NULL, username TEXT NOT NULL, ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_login_failures_ip_ts ON login_failures(ip, ts);
CREATE TABLE IF NOT EXISTS bans(
  ip TEXT PRIMARY KEY, until_ts INTEGER NOT NULL, strikes INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS watches(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, field TEXT NOT NULL, pattern TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1, created_by TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS watch_metrics(
  watch_id TEXT NOT NULL, ts INTEGER NOT NULL, cpu REAL NOT NULL, memory INTEGER NOT NULL,
  read_bytes INTEGER NOT NULL, write_bytes INTEGER NOT NULL, instances INTEGER NOT NULL,
  PRIMARY KEY(watch_id,ts), FOREIGN KEY(watch_id) REFERENCES watches(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_watch_metrics_ts ON watch_metrics(ts);
CREATE TABLE IF NOT EXISTS alert_rules(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, source TEXT NOT NULL, operator TEXT NOT NULL,
  threshold REAL NOT NULL, for_seconds REAL NOT NULL DEFAULT 0,
  severity TEXT NOT NULL, cooldown_seconds REAL NOT NULL DEFAULT 900,
  target_ids TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1,
  notify_recovery INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS alerts(
  id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, name TEXT NOT NULL, severity TEXT NOT NULL,
  state TEXT NOT NULL, message TEXT NOT NULL, started_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alerts_updated ON alerts(updated_at DESC);
CREATE TABLE IF NOT EXISTS notification_targets(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, provider TEXT NOT NULL, chat_id TEXT NOT NULL,
  secret_ref TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS audit(
  id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, username TEXT NOT NULL, ip TEXT NOT NULL,
  action TEXT NOT NULL, target TEXT NOT NULL, parameters TEXT NOT NULL,
  success INTEGER NOT NULL, result TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit(ts DESC);
CREATE TABLE IF NOT EXISTS ssh_events(
  id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, event TEXT NOT NULL, username TEXT NOT NULL,
  remote TEXT NOT NULL, session_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS network_flows(
  bucket INTEGER NOT NULL, pid INTEGER NOT NULL, process TEXT NOT NULL,
  group_name TEXT NOT NULL, username TEXT NOT NULL, protocol TEXT NOT NULL,
  remote_ip TEXT NOT NULL, remote_port INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL, tx_bytes INTEGER NOT NULL, samples INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(bucket,pid,process,group_name,username,protocol,remote_ip,remote_port)
);
CREATE INDEX IF NOT EXISTS idx_network_flows_bucket ON network_flows(bucket);
CREATE INDEX IF NOT EXISTS idx_network_flows_process ON network_flows(process,bucket);
CREATE INDEX IF NOT EXISTS idx_network_flows_group ON network_flows(group_name,bucket);
CREATE TABLE IF NOT EXISTS process_resource_history(
  bucket INTEGER NOT NULL, pid INTEGER NOT NULL, process TEXT NOT NULL,
  group_name TEXT NOT NULL, username TEXT NOT NULL,
  cpu_sum REAL NOT NULL, cpu_max REAL NOT NULL,
  memory_sum INTEGER NOT NULL, memory_max INTEGER NOT NULL,
  read_bytes INTEGER NOT NULL, write_bytes INTEGER NOT NULL,
  samples INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(bucket,pid,process,group_name,username)
);
CREATE INDEX IF NOT EXISTS idx_process_resources_bucket ON process_resource_history(bucket);
CREATE INDEX IF NOT EXISTS idx_process_resources_process ON process_resource_history(process,bucket);
CREATE INDEX IF NOT EXISTS idx_process_resources_group ON process_resource_history(group_name,bucket);
CREATE TABLE IF NOT EXISTS application_settings(
  key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL
);
INSERT OR IGNORE INTO application_settings(key,value,updated_at)
  VALUES('network_retention_days','10',strftime('%s','now'));
INSERT OR IGNORE INTO application_settings(key,value,updated_at) VALUES
  ('network_history_enabled','true',strftime('%s','now')),
  ('process_cpu_history_enabled','true',strftime('%s','now')),
  ('process_memory_history_enabled','true',strftime('%s','now')),
  ('process_disk_io_history_enabled','true',strftime('%s','now'));
CREATE TABLE IF NOT EXISTS package_inventory(
  id TEXT PRIMARY KEY, manager TEXT NOT NULL, name TEXT NOT NULL, architecture TEXT NOT NULL,
  installed_version TEXT NOT NULL, candidate_version TEXT NOT NULL DEFAULT '',
  update_state TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  installed_size_bytes INTEGER NOT NULL DEFAULT 0, first_seen INTEGER NOT NULL,
  last_changed INTEGER NOT NULL, last_seen INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_package_inventory_name ON package_inventory(name);
CREATE INDEX IF NOT EXISTS idx_package_inventory_state ON package_inventory(update_state);
CREATE TABLE IF NOT EXISTS package_events(
  id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, package_id TEXT NOT NULL,
  manager TEXT NOT NULL, name TEXT NOT NULL, architecture TEXT NOT NULL,
  event_type TEXT NOT NULL, old_version TEXT NOT NULL DEFAULT '',
  new_version TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_package_events_ts ON package_events(ts DESC);
CREATE TABLE IF NOT EXISTS package_scan_state(
  singleton INTEGER PRIMARY KEY CHECK(singleton=1), hostname TEXT NOT NULL DEFAULT '',
  manager TEXT NOT NULL DEFAULT '', inventory_available INTEGER NOT NULL DEFAULT 0,
  update_check_available INTEGER NOT NULL DEFAULT 0, inventory_scanned_at INTEGER NOT NULL DEFAULT 0,
  metadata_refreshed_at INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO package_scan_state(singleton) VALUES(1);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) SaveMetric(snapshot model.Snapshot) error {
	var rx, tx uint64
	for _, iface := range snapshot.Network {
		rx += iface.RXRate
		tx += iface.TXRate
	}
	memory, swap, disk := percent(snapshot.Memory.Used, snapshot.Memory.Total), percent(snapshot.Memory.SwapUsed, snapshot.Memory.SwapTotal), float64(0)
	for _, d := range snapshot.Disks {
		if d.UsedPercent > disk {
			disk = d.UsedPercent
		}
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO metrics
	  (ts,cpu,memory,swap,load,network_rx,network_tx,disk,processes,containers)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		snapshot.Timestamp.Unix(), snapshot.CPU.Usage, memory, swap, snapshot.CPU.Load[0],
		rx, tx, disk, len(snapshot.Processes), len(snapshot.Containers))
	return err
}

func (s *Store) SaveNetworkFlows(flows []model.NetworkFlow) error {
	if len(flows) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, flow := range flows {
		if flow.RXBytes == 0 && flow.TXBytes == 0 {
			continue
		}
		bucket := (flow.Timestamp.Unix() / 60) * 60
		_, err = tx.Exec(`INSERT INTO network_flows
		  (bucket,pid,process,group_name,username,protocol,remote_ip,remote_port,rx_bytes,tx_bytes,samples)
		  VALUES(?,?,?,?,?,?,?,?,?,?,1)
		  ON CONFLICT(bucket,pid,process,group_name,username,protocol,remote_ip,remote_port)
		  DO UPDATE SET rx_bytes=rx_bytes+excluded.rx_bytes,tx_bytes=tx_bytes+excluded.tx_bytes,samples=samples+1`,
			bucket, flow.PID, flow.Process, flow.Group, flow.User, flow.Protocol,
			flow.RemoteIP, flow.RemotePort, flow.RXBytes, flow.TXBytes)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) NetworkUsage(from, to time.Time, groupBy, query string, limit int) ([]NetworkUsage, error) {
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	keyColumn := "process"
	selectPID := "MAX(pid)"
	if groupBy == "group" {
		keyColumn = "group_name"
		selectPID = "0"
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	statement := fmt.Sprintf(`SELECT %s,MAX(process),MAX(group_name),MAX(username),%s,
	  SUM(rx_bytes),SUM(tx_bytes),COUNT(DISTINCT remote_ip||':'||remote_port),
	  MIN(bucket),MAX(bucket)
	  FROM network_flows WHERE bucket BETWEEN ? AND ?
	  AND (?='' OR lower(process) LIKE ? OR lower(group_name) LIKE ? OR lower(username) LIKE ?)
	  GROUP BY %s ORDER BY SUM(rx_bytes)+SUM(tx_bytes) DESC LIMIT ?`, keyColumn, selectPID, keyColumn)
	rows, err := s.db.Query(statement, from.Unix(), to.Unix(), query, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NetworkUsage
	for rows.Next() {
		var item NetworkUsage
		var first, last int64
		if err := rows.Scan(&item.Key, &item.Process, &item.Group, &item.User, &item.PID,
			&item.RXBytes, &item.TXBytes, &item.Destinations, &first, &last); err != nil {
			return nil, err
		}
		item.FirstSeen, item.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) NetworkDestinations(from, to time.Time, selector, value string, limit int) ([]NetworkDestination, error) {
	column := "process"
	if selector == "group" {
		column = "group_name"
	} else if selector == "pid" {
		column = "pid"
	}
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	statement := fmt.Sprintf(`SELECT remote_ip,remote_port,protocol,SUM(rx_bytes),SUM(tx_bytes),MIN(bucket),MAX(bucket)
	  FROM network_flows WHERE bucket BETWEEN ? AND ? AND %s=?
	  GROUP BY remote_ip,remote_port,protocol ORDER BY SUM(rx_bytes)+SUM(tx_bytes) DESC LIMIT ?`, column)
	rows, err := s.db.Query(statement, from.Unix(), to.Unix(), value, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NetworkDestination
	for rows.Next() {
		var item NetworkDestination
		var first, last int64
		if err := rows.Scan(&item.RemoteIP, &item.RemotePort, &item.Protocol, &item.RXBytes,
			&item.TXBytes, &first, &last); err != nil {
			return nil, err
		}
		item.FirstSeen, item.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) NetworkTimeline(from, to time.Time, selector, value string) ([]NetworkTimelinePoint, error) {
	column := "process"
	if selector == "group" {
		column = "group_name"
	} else if selector == "pid" {
		column = "pid"
	}
	interval := int64(300)
	if span := to.Sub(from); span > 72*time.Hour {
		interval = 6 * 3600
	} else if span > 24*time.Hour {
		interval = 3600
	}
	statement := fmt.Sprintf(`SELECT (bucket/?)*?,SUM(rx_bytes),SUM(tx_bytes)
	  FROM network_flows WHERE bucket BETWEEN ? AND ? AND %s=?
	  GROUP BY (bucket/?) ORDER BY bucket`, column)
	rows, err := s.db.Query(statement, interval, interval, from.Unix(), to.Unix(), value, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NetworkTimelinePoint
	for rows.Next() {
		var item NetworkTimelinePoint
		var timestamp int64
		if err := rows.Scan(&timestamp, &item.RXBytes, &item.TXBytes); err != nil {
			return nil, err
		}
		item.Time = time.Unix(timestamp, 0)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) NetworkRetentionDays() int {
	var raw string
	if s.db.QueryRow("SELECT value FROM application_settings WHERE key='network_retention_days'").Scan(&raw) != nil {
		return 10
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 365 {
		return 10
	}
	return value
}

func (s *Store) SetNetworkRetentionDays(days int) error {
	if days < 1 || days > 365 {
		return errors.New("retention must be between 1 and 365 days")
	}
	_, err := s.db.Exec(`INSERT INTO application_settings(key,value,updated_at) VALUES('network_retention_days',?,?)
	  ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, days, time.Now().Unix())
	return err
}

func (s *Store) settingEnabled(key string) bool {
	s.settingsMu.RLock()
	enabled, exists := s.historyCache[key]
	s.settingsMu.RUnlock()
	if exists {
		return enabled
	}
	var value string
	if s.db.QueryRow("SELECT value FROM application_settings WHERE key=?", key).Scan(&value) != nil {
		return true
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		enabled = true
	}
	s.settingsMu.Lock()
	s.historyCache[key] = enabled
	s.settingsMu.Unlock()
	return enabled
}

func (s *Store) loadHistorySettings() {
	for _, key := range []string{
		"network_history_enabled", "process_cpu_history_enabled",
		"process_memory_history_enabled", "process_disk_io_history_enabled",
	} {
		_ = s.settingEnabled(key)
	}
}

func (s *Store) NetworkHistoryEnabled() bool {
	return s.settingEnabled("network_history_enabled")
}

func (s *Store) SetHistoryCollectors(network, cpu, memory, diskIO bool) error {
	values := map[string]bool{
		"network_history_enabled":         network,
		"process_cpu_history_enabled":     cpu,
		"process_memory_history_enabled":  memory,
		"process_disk_io_history_enabled": diskIO,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, enabled := range values {
		if _, err = tx.Exec(`INSERT INTO application_settings(key,value,updated_at) VALUES(?,?,?)
		  ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`,
			key, strconv.FormatBool(enabled), time.Now().Unix()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.settingsMu.Lock()
	for key, enabled := range values {
		s.historyCache[key] = enabled
	}
	s.settingsMu.Unlock()
	return nil
}

func (s *Store) SaveProcessResources(snapshot model.Snapshot) error {
	cpuEnabled := s.settingEnabled("process_cpu_history_enabled")
	memoryEnabled := s.settingEnabled("process_memory_history_enabled")
	diskEnabled := s.settingEnabled("process_disk_io_history_enabled")
	if !cpuEnabled && !memoryEnabled && !diskEnabled {
		clear(s.ioSeen)
		return nil
	}
	bucket := (snapshot.Timestamp.Unix() / 60) * 60
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	active := make(map[string]bool, len(snapshot.Processes))
	for _, process := range snapshot.Processes {
		key := fmt.Sprintf("%d/%d", process.PID, process.StartTime.UnixNano())
		active[key] = true
		previous, seen := s.ioSeen[key]
		s.ioSeen[key] = [2]uint64{process.ReadBytes, process.WriteBytes}
		var readBytes, writeBytes uint64
		if diskEnabled && seen {
			if process.ReadBytes >= previous[0] {
				readBytes = process.ReadBytes - previous[0]
			}
			if process.WriteBytes >= previous[1] {
				writeBytes = process.WriteBytes - previous[1]
			}
		}
		cpu := process.CPU
		memory := process.Memory
		if !cpuEnabled {
			cpu = 0
		}
		if !memoryEnabled {
			memory = 0
		}
		_, err = tx.Exec(`INSERT INTO process_resource_history
		  (bucket,pid,process,group_name,username,cpu_sum,cpu_max,memory_sum,memory_max,read_bytes,write_bytes,samples)
		  VALUES(?,?,?,?,?,?,?,?,?,?,?,1)
		  ON CONFLICT(bucket,pid,process,group_name,username) DO UPDATE SET
		  cpu_sum=cpu_sum+excluded.cpu_sum,cpu_max=MAX(cpu_max,excluded.cpu_max),
		  memory_sum=memory_sum+excluded.memory_sum,memory_max=MAX(memory_max,excluded.memory_max),
		  read_bytes=read_bytes+excluded.read_bytes,write_bytes=write_bytes+excluded.write_bytes,
		  samples=samples+1`,
			bucket, process.PID, process.Name, resourceProcessGroup(process.Name), process.User,
			cpu, cpu, memory, memory, readBytes, writeBytes)
		if err != nil {
			return err
		}
	}
	for key := range s.ioSeen {
		if !active[key] {
			delete(s.ioSeen, key)
		}
	}
	return tx.Commit()
}

func resourceProcessGroup(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "ssh"):
		return "ssh"
	case strings.Contains(lower, "servora"), strings.Contains(lower, "system-maintenance"):
		return "servora"
	case strings.Contains(lower, "docker"), strings.Contains(lower, "containerd"):
		return "docker"
	case strings.HasPrefix(lower, "postgres"):
		return "postgresql"
	case strings.HasPrefix(lower, "mysql"), strings.HasPrefix(lower, "mariadb"):
		return "mysql"
	default:
		return lower
	}
}

func (s *Store) ResourceUsage(from, to time.Time, groupBy, query string, limit int) ([]ResourceUsage, error) {
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	keyColumn, pidColumn := "process", "MAX(pid)"
	if groupBy == "group" {
		keyColumn, pidColumn = "group_name", "0"
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	statement := fmt.Sprintf(`WITH per_bucket AS (
	  SELECT bucket,%s AS resource_key,MAX(process) process,MAX(group_name) group_name,
	  MAX(username) username,%s pid,
	  SUM(cpu_sum/NULLIF(samples,0)) cpu_average,SUM(cpu_max) cpu_peak,
	  SUM(memory_sum/NULLIF(samples,0)) memory_average,SUM(memory_max) memory_peak,
	  SUM(read_bytes) read_bytes,SUM(write_bytes) write_bytes
	  FROM process_resource_history WHERE bucket BETWEEN ? AND ?
	  AND (?='' OR lower(process) LIKE ? OR lower(group_name) LIKE ? OR lower(username) LIKE ?)
	  GROUP BY bucket,%s
	)
	SELECT resource_key,MAX(process),MAX(group_name),MAX(username),MAX(pid),
	  AVG(cpu_average),MAX(cpu_peak),CAST(AVG(memory_average) AS INTEGER),MAX(memory_peak),
	  SUM(read_bytes),SUM(write_bytes),MIN(bucket),MAX(bucket)
	FROM per_bucket GROUP BY resource_key
	ORDER BY MAX(cpu_peak) DESC,MAX(memory_peak) DESC LIMIT ?`,
		keyColumn, pidColumn, keyColumn)
	rows, err := s.db.Query(statement, from.Unix(), to.Unix(), query, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ResourceUsage
	for rows.Next() {
		var item ResourceUsage
		var first, last int64
		if err := rows.Scan(&item.Key, &item.Process, &item.Group, &item.User, &item.PID,
			&item.CPUAverage, &item.CPUMax, &item.MemoryAvg, &item.MemoryMax,
			&item.ReadBytes, &item.WriteBytes, &first, &last); err != nil {
			return nil, err
		}
		item.FirstSeen, item.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ResourceTimeline(from, to time.Time, selector, value string) ([]ResourceTimelinePoint, error) {
	column := "process"
	if selector == "group" {
		column = "group_name"
	}
	interval := int64(300)
	if span := to.Sub(from); span > 72*time.Hour {
		interval = 6 * 3600
	} else if span > 24*time.Hour {
		interval = 3600
	}
	statement := fmt.Sprintf(`WITH per_bucket AS (
	  SELECT bucket,SUM(cpu_sum/NULLIF(samples,0)) cpu_average,SUM(cpu_max) cpu_peak,
	  SUM(memory_sum/NULLIF(samples,0)) memory_average,SUM(memory_max) memory_peak,
	  SUM(read_bytes) read_bytes,SUM(write_bytes) write_bytes
	  FROM process_resource_history WHERE bucket BETWEEN ? AND ? AND %s=?
	  GROUP BY bucket
	)
	SELECT (bucket/?)*?,AVG(cpu_average),MAX(cpu_peak),CAST(AVG(memory_average) AS INTEGER),
	  MAX(memory_peak),SUM(read_bytes),SUM(write_bytes)
	FROM per_bucket GROUP BY (bucket/?) ORDER BY bucket`, column)
	rows, err := s.db.Query(statement, from.Unix(), to.Unix(), value, interval, interval, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ResourceTimelinePoint
	for rows.Next() {
		var item ResourceTimelinePoint
		var timestamp int64
		if err := rows.Scan(&timestamp, &item.CPUAverage, &item.CPUMax, &item.MemoryAvg,
			&item.MemoryMax, &item.ReadBytes, &item.WriteBytes); err != nil {
			return nil, err
		}
		item.Time = time.Unix(timestamp, 0)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ClearResourceHistory() error {
	if _, err := s.db.Exec("DELETE FROM process_resource_history"); err != nil {
		return err
	}
	clear(s.ioSeen)
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	_, err := s.db.Exec("VACUUM")
	return err
}

func (s *Store) NetworkStorage() NetworkStorageInfo {
	info := NetworkStorageInfo{
		RetentionDays:  s.NetworkRetentionDays(),
		NetworkEnabled: s.NetworkHistoryEnabled(),
		CPUEnabled:     s.settingEnabled("process_cpu_history_enabled"),
		MemoryEnabled:  s.settingEnabled("process_memory_history_enabled"),
		DiskIOEnabled:  s.settingEnabled("process_disk_io_history_enabled"),
	}
	_ = s.db.QueryRow("SELECT COUNT(*),COALESCE(MIN(bucket),0),COALESCE(MAX(bucket),0) FROM network_flows").
		Scan(&info.Rows, &info.Oldest, &info.Newest)
	_ = s.db.QueryRow("SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name IN ('network_flows','idx_network_flows_bucket','idx_network_flows_process','idx_network_flows_group')").
		Scan(&info.Bytes)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM process_resource_history").Scan(&info.ResourceRows)
	_ = s.db.QueryRow("SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name IN ('process_resource_history','idx_process_resources_bucket','idx_process_resources_process','idx_process_resources_group')").
		Scan(&info.ResourceBytes)
	return info
}

func (s *Store) NetworkTotal(since time.Time, direction string) (float64, bool) {
	expression := "rx_bytes+tx_bytes"
	if direction == "rx" {
		expression = "rx_bytes"
	} else if direction == "tx" {
		expression = "tx_bytes"
	}
	var total float64
	statement := fmt.Sprintf("SELECT COALESCE(SUM(%s),0) FROM network_flows WHERE bucket>=?", expression)
	if s.db.QueryRow(statement, since.Unix()).Scan(&total) != nil {
		return 0, false
	}
	return total, true
}

func (s *Store) PruneNetworkFlows(now time.Time) error {
	cutoff := now.AddDate(0, 0, -s.NetworkRetentionDays()).Unix()
	if _, err := s.db.Exec("DELETE FROM network_flows WHERE bucket < ?", cutoff); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM process_resource_history WHERE bucket < ?", cutoff)
	return err
}

func (s *Store) ClearNetworkFlows() error {
	if _, err := s.db.Exec("DELETE FROM network_flows"); err != nil {
		return err
	}
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	_, err := s.db.Exec("VACUUM")
	return err
}

func (s *Store) SaveWatchMetrics(snapshot model.Snapshot) error {
	watches, err := s.Watches()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, watch := range watches {
		if !watch.Enabled {
			continue
		}
		matcher, err := regexp.Compile(watch.Pattern)
		if err != nil {
			continue
		}
		point := WatchPoint{Time: snapshot.Timestamp}
		for _, process := range snapshot.Processes {
			value := process.Name
			switch watch.Field {
			case "command":
				value = process.Command
			case "executable":
				fields := strings.Fields(process.Command)
				if len(fields) == 0 {
					continue
				}
				value = fields[0]
			}
			if !matcher.MatchString(value) {
				continue
			}
			point.Instances++
			point.CPU += process.CPU
			point.Memory += process.Memory
			point.ReadBytes += process.ReadBytes
			point.WriteBytes += process.WriteBytes
			if point.Instances >= 200 {
				break
			}
		}
		_, err = tx.Exec(`INSERT OR REPLACE INTO watch_metrics
		  (watch_id,ts,cpu,memory,read_bytes,write_bytes,instances) VALUES(?,?,?,?,?,?,?)`,
			watch.ID, point.Time.Unix(), point.CPU, point.Memory, point.ReadBytes, point.WriteBytes, point.Instances)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) WatchHistory(id string, from time.Time, limit int) ([]WatchPoint, error) {
	if limit < 1 || limit > 10000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT ts,cpu,memory,read_bytes,write_bytes,instances
	  FROM watch_metrics WHERE watch_id=? AND ts>=? ORDER BY ts LIMIT ?`, id, from.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchPoint
	for rows.Next() {
		var point WatchPoint
		var ts int64
		if err := rows.Scan(&ts, &point.CPU, &point.Memory, &point.ReadBytes, &point.WriteBytes, &point.Instances); err != nil {
			return nil, err
		}
		point.Time = time.Unix(ts, 0)
		out = append(out, point)
	}
	return out, rows.Err()
}

func (s *Store) History(from, to time.Time, limit int) ([]MetricPoint, error) {
	if limit < 1 || limit > 10000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT ts,cpu,memory,swap,load,network_rx,network_tx,disk,processes,containers FROM (
	  SELECT bucket AS ts,cpu_avg AS cpu,memory_avg AS memory,swap_avg AS swap,load_avg AS load,
	    network_rx_avg AS network_rx,network_tx_avg AS network_tx,disk_max AS disk,
	    CAST(processes_avg AS INTEGER) AS processes,CAST(containers_avg AS INTEGER) AS containers
	    FROM metric_rollups WHERE bucket BETWEEN ? AND ?
	  UNION ALL
	  SELECT ts,cpu,memory,swap,load,network_rx,network_tx,disk,processes,containers
	    FROM metrics WHERE ts BETWEEN ? AND ?
	  ) ORDER BY ts ASC LIMIT ?`, from.Unix(), to.Unix(), from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		var ts int64
		if err := rows.Scan(&ts, &p.CPU, &p.Memory, &p.Swap, &p.Load, &p.NetworkRX, &p.NetworkTX, &p.Disk, &p.Processes, &p.Containers); err != nil {
			return nil, err
		}
		p.Time = time.Unix(ts, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RollupAndPrune(now time.Time, rawRetention, rollupRetention time.Duration, maxBytes int64, dbPath string) error {
	rawCutoff := now.Add(-rawRetention).Unix()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO metric_rollups
	  SELECT (ts/300)*300, MIN(cpu),AVG(cpu),MAX(cpu),MIN(memory),AVG(memory),MAX(memory),
	  AVG(swap),AVG(load),AVG(network_rx),AVG(network_tx),MAX(disk),AVG(processes),AVG(containers)
	  FROM metrics WHERE ts < ? GROUP BY (ts/300)*300`, rawCutoff)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec("DELETE FROM metrics WHERE ts < ?", rawCutoff)
	_, _ = s.db.Exec("DELETE FROM watch_metrics WHERE ts < ?", rawCutoff)
	_, _ = s.db.Exec("DELETE FROM metric_rollups WHERE bucket < ?", now.Add(-rollupRetention).Unix())
	_, _ = s.db.Exec("DELETE FROM sessions WHERE created_at < ?", now.Add(-24*time.Hour).Unix())
	_, _ = s.db.Exec("DELETE FROM login_failures WHERE ts < ?", now.Add(-24*time.Hour).Unix())
	_, _ = s.db.Exec("DELETE FROM bans WHERE until_ts < ?", now.Unix())
	_, _ = s.db.Exec("DELETE FROM audit WHERE ts < ?", now.Add(-rollupRetention).Unix())
	if fi, e := os.Stat(dbPath); e == nil && fi.Size() > maxBytes {
		for i := 0; i < 10 && fi != nil && fi.Size() > maxBytes; i++ {
			_, _ = s.db.Exec(`DELETE FROM metrics WHERE ts IN (SELECT ts FROM metrics ORDER BY ts LIMIT 10000)`)
			_, _ = s.db.Exec(`DELETE FROM metric_rollups WHERE bucket IN (SELECT bucket FROM metric_rollups ORDER BY bucket LIMIT 1000)`)
			_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			fi, _ = os.Stat(dbPath)
		}
	}
	return nil
}

func (s *Store) CreateSession(rawToken, username, csrf, ip string, now time.Time) error {
	_, err := s.db.Exec("INSERT INTO sessions(token_hash,username,csrf,ip,created_at,seen_at) VALUES(?,?,?,?,?,?)",
		hash(rawToken), username, csrf, ip, now.Unix(), now.Unix())
	return err
}

func (s *Store) Session(rawToken string, idle, absolute time.Duration, now time.Time) (Session, error) {
	out, err := s.session(rawToken, idle, absolute, now)
	if err != nil {
		return Session{}, err
	}
	_, _ = s.db.Exec("UPDATE sessions SET seen_at=? WHERE token_hash=?", now.Unix(), hash(rawToken))
	out.Seen = now
	return out, nil
}

// SessionActive validates an existing session without extending its idle
// lifetime. Long-lived streams use it so passive telemetry cannot keep a
// browser session alive forever.
func (s *Store) SessionActive(rawToken string, idle, absolute time.Duration, now time.Time) (Session, error) {
	return s.session(rawToken, idle, absolute, now)
}

func (s *Store) session(rawToken string, idle, absolute time.Duration, now time.Time) (Session, error) {
	var out Session
	var created, seen int64
	err := s.db.QueryRow("SELECT username,csrf,ip,created_at,seen_at FROM sessions WHERE token_hash=?", hash(rawToken)).
		Scan(&out.User, &out.CSRF, &out.IP, &created, &seen)
	if err != nil {
		return out, err
	}
	out.Created, out.Seen = time.Unix(created, 0), time.Unix(seen, 0)
	if now.Sub(out.Created) > absolute || now.Sub(out.Seen) > idle {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE token_hash=?", hash(rawToken))
		return Session{}, errors.New("session expired")
	}
	return out, nil
}

func (s *Store) DeleteSession(rawToken string) {
	_, _ = s.db.Exec("DELETE FROM sessions WHERE token_hash=?", hash(rawToken))
}

func (s *Store) IsBanned(ip string, now time.Time) (time.Time, bool) {
	var until int64
	if s.db.QueryRow("SELECT until_ts FROM bans WHERE ip=?", ip).Scan(&until) != nil {
		return time.Time{}, false
	}
	return time.Unix(until, 0), until > now.Unix()
}

func (s *Store) LoginFailed(ip, username string, now time.Time) (time.Time, bool) {
	tx, err := s.db.Begin()
	if err != nil {
		return time.Time{}, false
	}
	defer tx.Rollback()
	_, _ = tx.Exec("INSERT INTO login_failures(ip,username,ts) VALUES(?,?,?)", ip, username, now.Unix())
	var count int
	_ = tx.QueryRow("SELECT COUNT(*) FROM login_failures WHERE ip=? AND ts>=?", ip, now.Add(-15*time.Minute).Unix()).Scan(&count)
	if count < 5 {
		_ = tx.Commit()
		return time.Time{}, false
	}
	var strikes int
	_ = tx.QueryRow("SELECT strikes FROM bans WHERE ip=?", ip).Scan(&strikes)
	strikes++
	if strikes < 1 {
		strikes = 1
	}
	if strikes > 6 {
		strikes = 6
	}
	until := now.Add(30 * time.Minute * time.Duration(1<<(strikes-1)))
	_, _ = tx.Exec(`INSERT INTO bans(ip,until_ts,strikes) VALUES(?,?,?)
	  ON CONFLICT(ip) DO UPDATE SET until_ts=excluded.until_ts,strikes=excluded.strikes`, ip, until.Unix(), strikes)
	_ = tx.Commit()
	return until, true
}

func (s *Store) ClearFailures(ip string) {
	_, _ = s.db.Exec("DELETE FROM login_failures WHERE ip=?", ip)
}

func (s *Store) Audit(username, ip, action, target string, params any, success bool, result string) {
	raw, _ := json.Marshal(params)
	if len(raw) > 4096 {
		raw = []byte(`{"redacted":"too large"}`)
	}
	if len(result) > 4096 {
		result = result[:4096]
	}
	_, _ = s.db.Exec(`INSERT INTO audit(ts,username,ip,action,target,parameters,success,result)
	  VALUES(?,?,?,?,?,?,?,?)`, time.Now().Unix(), username, ip, action, target, string(raw), success, result)
}

func (s *Store) Audits(limit int) ([]Audit, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.Query("SELECT id,ts,username,ip,action,target,parameters,success,result FROM audit ORDER BY ts DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		var a Audit
		var ts int64
		if err := rows.Scan(&a.ID, &ts, &a.User, &a.IP, &a.Action, &a.Target, &a.Parameters, &a.Success, &a.Result); err != nil {
			return nil, err
		}
		a.Time = time.Unix(ts, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Watches() ([]Watch, error) {
	rows, err := s.db.Query("SELECT id,name,field,pattern,enabled,created_by,created_at FROM watches ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watch
	for rows.Next() {
		var w Watch
		var created int64
		if err := rows.Scan(&w.ID, &w.Name, &w.Field, &w.Pattern, &w.Enabled, &w.CreatedBy, &created); err != nil {
			return nil, err
		}
		w.CreatedAt = time.Unix(created, 0)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) PutWatch(w Watch) error {
	_, err := s.db.Exec(`INSERT INTO watches(id,name,field,pattern,enabled,created_by,created_at) VALUES(?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET name=excluded.name,field=excluded.field,pattern=excluded.pattern,enabled=excluded.enabled`,
		w.ID, w.Name, w.Field, w.Pattern, w.Enabled, w.CreatedBy, time.Now().Unix())
	return err
}
func (s *Store) DeleteWatch(id string) error {
	_, err := s.db.Exec("DELETE FROM watches WHERE id=?", id)
	return err
}

func (s *Store) AlertRules() ([]AlertRule, error) {
	rows, err := s.db.Query(`SELECT id,name,source,operator,threshold,for_seconds,severity,cooldown_seconds,target_ids,enabled,notify_recovery FROM alert_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Source, &r.Operator, &r.Threshold, &r.ForSeconds, &r.Severity, &r.CooldownSeconds, &r.TargetIDs, &r.Enabled, &r.NotifyRecovery); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) PutAlertRule(r AlertRule) error {
	_, err := s.db.Exec(`INSERT INTO alert_rules(id,name,source,operator,threshold,for_seconds,severity,cooldown_seconds,target_ids,enabled,notify_recovery)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,source=excluded.source,operator=excluded.operator,
	 threshold=excluded.threshold,for_seconds=excluded.for_seconds,severity=excluded.severity,cooldown_seconds=excluded.cooldown_seconds,
	 target_ids=excluded.target_ids,enabled=excluded.enabled,notify_recovery=excluded.notify_recovery`,
		r.ID, r.Name, r.Source, r.Operator, r.Threshold, r.ForSeconds, r.Severity, r.CooldownSeconds, r.TargetIDs, r.Enabled, r.NotifyRecovery)
	return err
}
func (s *Store) DeleteAlertRule(id string) error {
	_, err := s.db.Exec("DELETE FROM alert_rules WHERE id=?", id)
	return err
}

func (s *Store) Alerts(limit int) ([]Alert, error) {
	rows, err := s.db.Query("SELECT id,rule_id,name,severity,state,message,started_at,updated_at FROM alerts ORDER BY updated_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var st, up int64
		if err := rows.Scan(&a.ID, &a.RuleID, &a.Name, &a.Severity, &a.State, &a.Message, &st, &up); err != nil {
			return nil, err
		}
		a.StartedAt = time.Unix(st, 0)
		a.UpdatedAt = time.Unix(up, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ActiveAlert(ruleID string) (Alert, bool) {
	var a Alert
	var st, up int64
	err := s.db.QueryRow("SELECT id,rule_id,name,severity,state,message,started_at,updated_at FROM alerts WHERE rule_id=? AND state IN ('pending','firing','acknowledged') ORDER BY updated_at DESC LIMIT 1", ruleID).
		Scan(&a.ID, &a.RuleID, &a.Name, &a.Severity, &a.State, &a.Message, &st, &up)
	if err != nil {
		return Alert{}, false
	}
	a.StartedAt = time.Unix(st, 0)
	a.UpdatedAt = time.Unix(up, 0)
	return a, true
}
func (s *Store) PutAlert(a Alert) error {
	_, err := s.db.Exec(`INSERT INTO alerts(id,rule_id,name,severity,state,message,started_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET state=excluded.state,message=excluded.message,updated_at=excluded.updated_at`,
		a.ID, a.RuleID, a.Name, a.Severity, a.State, a.Message, a.StartedAt.Unix(), a.UpdatedAt.Unix())
	return err
}
func (s *Store) AcknowledgeAlert(id string) error {
	_, err := s.db.Exec("UPDATE alerts SET state='acknowledged',updated_at=? WHERE id=? AND state='firing'", time.Now().Unix(), id)
	return err
}

func (s *Store) NotificationTargets() ([]NotificationTarget, error) {
	rows, err := s.db.Query("SELECT id,name,provider,chat_id,secret_ref,enabled FROM notification_targets")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationTarget
	for rows.Next() {
		var t NotificationTarget
		if err := rows.Scan(&t.ID, &t.Name, &t.Provider, &t.ChatID, &t.SecretRef, &t.Enabled); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) PutNotificationTarget(t NotificationTarget) error {
	_, err := s.db.Exec(`INSERT INTO notification_targets(id,name,provider,chat_id,secret_ref,enabled) VALUES(?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET name=excluded.name,provider=excluded.provider,chat_id=excluded.chat_id,secret_ref=excluded.secret_ref,enabled=excluded.enabled`,
		t.ID, t.Name, t.Provider, t.ChatID, t.SecretRef, t.Enabled)
	return err
}
func (s *Store) DeleteNotificationTarget(id string) error {
	_, err := s.db.Exec("DELETE FROM notification_targets WHERE id=?", id)
	return err
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Store) DB() *sql.DB { return s.db }
func (s *Store) Ping() error { return s.db.Ping() }
func Wrap(err error, operation string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
