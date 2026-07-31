package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	value, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	return value
}

func TestSessionLifecycle(t *testing.T) {
	db := newTestStore(t)
	now := time.Now()
	if err := db.CreateSession("secret-token", "alice", "csrf", "127.0.0.1", now); err != nil {
		t.Fatal(err)
	}
	got, err := db.Session("secret-token", time.Hour, 12*time.Hour, now.Add(time.Minute))
	if err != nil || got.User != "alice" {
		t.Fatalf("unexpected session: %#v, %v", got, err)
	}
	if _, err := db.Session("wrong-token", time.Hour, 12*time.Hour, now); err == nil {
		t.Fatal("unknown token accepted")
	}
}

func TestSessionActiveDoesNotExtendIdleLifetime(t *testing.T) {
	db := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.CreateSession("stream-token", "alice", "csrf", "127.0.0.1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionActive("stream-token", time.Minute, time.Hour, now.Add(30*time.Second)); err != nil {
		t.Fatalf("active stream session rejected early: %v", err)
	}
	if _, err := db.SessionActive("stream-token", time.Minute, time.Hour, now.Add(61*time.Second)); err == nil {
		t.Fatal("passive stream validation extended the idle lifetime")
	}
}

func TestLoginBan(t *testing.T) {
	db := newTestStore(t)
	now := time.Now()
	for attempt := 0; attempt < 4; attempt++ {
		if _, banned := db.LoginFailed("192.0.2.1", "alice", now); banned {
			t.Fatal("IP banned too early")
		}
	}
	until, banned := db.LoginFailed("192.0.2.1", "alice", now)
	if !banned || until.Sub(now) < 29*time.Minute {
		t.Fatalf("expected 30-minute ban, got %v %v", until, banned)
	}
}

func TestMetricsAndWatchAggregation(t *testing.T) {
	db := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := model.Snapshot{
		Timestamp: now, CPU: model.CPU{Usage: 42, Load: [3]float64{1.5}},
		Memory:    model.Memory{Total: 100, Used: 60},
		Processes: []model.Process{{Name: "nginx", Command: "/usr/sbin/nginx", CPU: 10, Memory: 20}},
	}
	if err := db.SaveMetric(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.PutWatch(Watch{ID: "12345678", Name: "nginx", Field: "name", Pattern: "^nginx$", Enabled: true, CreatedBy: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWatchMetrics(snapshot); err != nil {
		t.Fatal(err)
	}
	points, err := db.WatchHistory("12345678", now.Add(-time.Minute), 10)
	if err != nil || len(points) != 1 || points[0].Instances != 1 {
		t.Fatalf("unexpected watch points: %#v, %v", points, err)
	}
}

func TestNetworkFlowAggregationGroupingAndRetention(t *testing.T) {
	db := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	flows := []model.NetworkFlow{
		{Timestamp: now, PID: 10, Process: "sshd", Group: "ssh", User: "root", Protocol: "tcp", RemoteIP: "192.0.2.1", RemotePort: 50000, RXBytes: 10, TXBytes: 20},
		{Timestamp: now.Add(10 * time.Second), PID: 11, Process: "sshd", Group: "ssh", User: "root", Protocol: "tcp", RemoteIP: "192.0.2.2", RemotePort: 50001, RXBytes: 30, TXBytes: 40},
	}
	if err := db.SaveNetworkFlows(flows); err != nil {
		t.Fatal(err)
	}
	processes, err := db.NetworkUsage(now.Add(-time.Hour), now.Add(time.Hour), "process", "", 10)
	if err != nil || len(processes) != 1 || processes[0].RXBytes != 40 || processes[0].TXBytes != 60 {
		t.Fatalf("unexpected process usage: %#v, %v", processes, err)
	}
	groups, err := db.NetworkUsage(now.Add(-time.Hour), now.Add(time.Hour), "group", "ssh", 10)
	if err != nil || len(groups) != 1 || groups[0].Destinations != 2 {
		t.Fatalf("unexpected grouped usage: %#v, %v", groups, err)
	}
	destinations, err := db.NetworkDestinations(now.Add(-time.Hour), now.Add(time.Hour), "group", "ssh", 10)
	if err != nil || len(destinations) != 2 {
		t.Fatalf("unexpected destinations: %#v, %v", destinations, err)
	}
	pidDestinations, err := db.NetworkDestinations(now.Add(-time.Hour), now.Add(time.Hour), "pid", "10", 10)
	if err != nil || len(pidDestinations) != 1 || pidDestinations[0].RXBytes != 10 || pidDestinations[0].TXBytes != 20 {
		t.Fatalf("unexpected PID destinations: %#v, %v", pidDestinations, err)
	}
	if err := db.SetNetworkRetentionDays(7); err != nil || db.NetworkRetentionDays() != 7 {
		t.Fatalf("retention was not saved: %v", err)
	}
	if err := db.ClearNetworkFlows(); err != nil || db.NetworkStorage().Rows != 0 {
		t.Fatalf("network history was not cleared: %v", err)
	}
}

func TestProcessResourceHistoryAndCollectorSettings(t *testing.T) {
	db := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	process := model.Process{
		PID: 42, Name: "sshd", User: "root", CPU: 12.5, Memory: 64 << 20,
		ReadBytes: 1000, WriteBytes: 2000, StartTime: now.Add(-time.Hour),
	}
	if err := db.SaveProcessResources(model.Snapshot{Timestamp: now, Processes: []model.Process{process}}); err != nil {
		t.Fatal(err)
	}
	process.CPU, process.Memory, process.ReadBytes, process.WriteBytes = 25, 96<<20, 1500, 3000
	if err := db.SaveProcessResources(model.Snapshot{Timestamp: now.Add(10 * time.Second), Processes: []model.Process{process}}); err != nil {
		t.Fatal(err)
	}
	items, err := db.ResourceUsage(now.Add(-time.Hour), now.Add(time.Hour), "group", "ssh", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("unexpected resource usage: %#v, %v", items, err)
	}
	if items[0].CPUMax != 25 || items[0].MemoryMax != 96<<20 || items[0].ReadBytes != 500 || items[0].WriteBytes != 1000 {
		t.Fatalf("incorrect resource aggregation: %#v", items[0])
	}
	if err := db.SetHistoryCollectors(false, true, false, true); err != nil {
		t.Fatal(err)
	}
	settings := db.NetworkStorage()
	if settings.NetworkEnabled || !settings.CPUEnabled || settings.MemoryEnabled || !settings.DiskIOEnabled {
		t.Fatalf("collector settings were not retained: %#v", settings)
	}
	if err := db.ClearResourceHistory(); err != nil || db.NetworkStorage().ResourceRows != 0 {
		t.Fatalf("resource history was not cleared: %v", err)
	}
}
