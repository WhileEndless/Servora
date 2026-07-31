package agent

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	got, err := parseArgs(`["--dry-run","value with spaces"]`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--dry-run", "value with spaces"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for _, invalid := range []string{`"not-an-array"`, `["line\nbreak"]`} {
		if _, err := parseArgs(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestSystemdQuote(t *testing.T) {
	got := systemdQuote(`a "value" with % and \`)
	want := `"a \"value\" with %% and \\"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestIdentifiers(t *testing.T) {
	if !validJobName("daily-report") || validJobName("../escape") {
		t.Fatal("job identifier validation failed")
	}
	if !validUnit("example.service") || validUnit("../../example.service") {
		t.Fatal("unit validation failed")
	}
}

func TestParseHumanBytes(t *testing.T) {
	if got := parseHumanBytes("1.5 GiB"); got != 1610612736 {
		t.Fatalf("got %d", got)
	}
}
