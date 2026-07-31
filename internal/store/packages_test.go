package store

import (
	"testing"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

func TestPackageInventoryBaselineAndChanges(t *testing.T) {
	st, err := Open(t.TempDir() + "/monitor.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first := time.Unix(1_700_000_000, 0)
	scan := model.PackageScan{
		Hostname: "host", Manager: "apt", InventoryAvailable: true,
		UpdateCheckAvailable: true, InventoryScannedAt: first,
		Items: []model.Package{
			{ID: "a", Manager: "apt", Name: "alpha", Architecture: "amd64", InstalledVersion: "1", UpdateState: "current"},
			{ID: "b", Manager: "apt", Name: "beta", Architecture: "amd64", InstalledVersion: "1", CandidateVersion: "2", UpdateState: "update_available"},
		},
	}
	if err := st.SavePackageScan(scan); err != nil {
		t.Fatal(err)
	}
	events, total, err := st.PackageEvents(time.Unix(0, 0), "", "", 1, 100)
	if err != nil || total != 0 || len(events) != 0 {
		t.Fatalf("baseline generated events: total=%d items=%v err=%v", total, events, err)
	}
	count, updates, unknown, err := st.PackageCounts()
	if err != nil || count != 2 || updates != 1 || unknown != 0 {
		t.Fatalf("unexpected counts: %d %d %d %v", count, updates, unknown, err)
	}

	scan.InventoryScannedAt = first.Add(time.Hour)
	scan.Items = []model.Package{
		{ID: "a", Manager: "apt", Name: "alpha", Architecture: "amd64", InstalledVersion: "2", UpdateState: "current"},
		{ID: "c", Manager: "apt", Name: "charlie", Architecture: "amd64", InstalledVersion: "1", UpdateState: "unknown"},
	}
	if err := st.SavePackageScan(scan); err != nil {
		t.Fatal(err)
	}
	events, total, err = st.PackageEvents(time.Unix(0, 0), "", "", 1, 100)
	if err != nil || total != 3 {
		t.Fatalf("unexpected change events: total=%d err=%v", total, err)
	}
	types := map[string]bool{}
	for _, event := range events {
		types[event.EventType] = true
	}
	for _, eventType := range []string{"installed", "removed", "version_changed"} {
		if !types[eventType] {
			t.Fatalf("missing %s event: %#v", eventType, events)
		}
	}
	items, total, err := st.Packages("alpha", "", "", "name", "asc", 1, 100)
	if err != nil || total != 1 || len(items) != 1 || items[0].InstalledVersion != "2" {
		t.Fatalf("unexpected package query: total=%d items=%#v err=%v", total, items, err)
	}
}
