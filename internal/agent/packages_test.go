package agent

import "testing"

func TestPackageIDRoundTrip(t *testing.T) {
	id := packageID("apt", "libexample+", "amd64")
	manager, name, architecture, err := decodePackageID(id)
	if err != nil {
		t.Fatal(err)
	}
	if manager != "apt" || name != "libexample+" || architecture != "amd64" {
		t.Fatalf("unexpected round trip: %q %q %q", manager, name, architecture)
	}
	for _, invalid := range []string{"", "not_base64!", packageID("", "name", "amd64")} {
		if _, _, _, err := decodePackageID(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestParseAPTUpdatesUsesReportedArchitecture(t *testing.T) {
	updates := parseAPTUpdates(
		"Inst native [1] (2 Ubuntu:stable [amd64])\nInst common [1] (2 Ubuntu:stable [all])\n",
		"amd64",
	)
	if updates["native\x00amd64"] != "2" || updates["common\x00all"] != "2" {
		t.Fatalf("unexpected updates: %#v", updates)
	}
}
