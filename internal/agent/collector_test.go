package agent

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCollectorSnapshotHasCoreMetrics(t *testing.T) {
	collector := NewCollector(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot := collector.Snapshot(ctx)
	if snapshot.Timestamp.IsZero() || snapshot.Hostname == "" {
		t.Fatalf("missing snapshot identity: %#v", snapshot)
	}
	if snapshot.CPU.Cores < 1 || snapshot.Memory.Total == 0 || snapshot.Uptime <= 0 {
		t.Fatalf("missing core metrics: %#v", snapshot)
	}
	if snapshot.Processes == nil || snapshot.Network == nil {
		t.Fatal("process and network inventories must be initialized")
	}
	detail, err := collector.ProcessDetail(os.Getpid())
	if err != nil {
		t.Fatalf("current process details unavailable: %v", err)
	}
	if detail.Process.PID != os.Getpid() || detail.Executable == "" || detail.Status["Name"] == "" {
		t.Fatalf("incomplete process detail: %#v", detail)
	}
}
