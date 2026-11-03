//go:build !windows

package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func verboseMarker(e *briefEnv, id string) string {
	return filepath.Join(e.dir, id, "_state", "verbose")
}

func TestStartVersionVerboseWritesMarker(t *testing.T) {
	e, m := libEnv(t)
	rec, _, err := m.startVersion("csvstat", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if !rec.Verbose {
		t.Error("returned record is not verbose")
	}
	if r, ok := m.readRecord(rec.ID); !ok || !r.Verbose {
		t.Errorf("stored record %+v is not verbose", r)
	}
	if _, err := os.Stat(verboseMarker(e, rec.ID)); err != nil {
		t.Errorf("no verbose marker: %v", err)
	}
}

func TestStartVersionQuietWritesNoMarker(t *testing.T) {
	e, m := libEnv(t)
	rec, _, err := m.startVersion("csvstat", "v1", false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if rec.Verbose {
		t.Error("record is verbose")
	}
	if _, err := os.Stat(verboseMarker(e, rec.ID)); !os.IsNotExist(err) {
		t.Errorf("marker present or odd error: %v", err)
	}
}

func TestResubmitKeepsVerbose(t *testing.T) {
	e, m := libEnv(t)
	rec, _, err := m.startVersion("csvstat", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	again, err := m.resubmitID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if !again.Verbose {
		t.Error("resubmitted record is not verbose")
	}
	if _, err := os.Stat(verboseMarker(e, again.ID)); err != nil {
		t.Errorf("no marker on the resubmitted run: %v", err)
	}
}

func TestUploadVerboseQuery(t *testing.T) {
	e := newBriefEnv(t)
	if w := e.post("/app/briefs?verbose=1", e.briefText("gm-2026-10-23-001"), nil); w.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	rec := e.waitPhase(rid(1), phaseDone, phaseFailed)
	if !rec.Verbose {
		t.Errorf("record %+v is not verbose", rec)
	}
	if _, err := os.Stat(verboseMarker(e, rec.ID)); err != nil {
		t.Errorf("no marker: %v", err)
	}
}
