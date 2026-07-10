package telemetry

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDisabledByDefault(t *testing.T) {
	if New("", false) != nil {
		t.Error("telemetry must be nil when disabled")
	}
	if New(filepath.Join(t.TempDir(), "t.json"), false) != nil {
		t.Error("telemetry must be nil when enabled=false")
	}
	// nil recorder Incr must not panic.
	var r *Recorder
	r.Incr("x")
}

func TestRecordsAndReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.json")
	r := New(path, true)
	r.Incr("run")
	r.Incr("run")
	r.Incr("ask")

	// A fresh recorder loads existing counts (persistence).
	r2 := New(path, true)
	r2.Incr("run")

	out, err := Report(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "run: 3") || !strings.Contains(out, "ask: 1") {
		t.Errorf("report wrong:\n%s", out)
	}
}

func TestReportMissing(t *testing.T) {
	out, err := Report(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || !strings.Contains(out, "no telemetry") {
		t.Errorf("missing telemetry report: %q err=%v", out, err)
	}
}
