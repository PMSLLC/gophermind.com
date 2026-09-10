package freellm

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOdometerAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := o.Add(path, Event{TS: now, Profile: "free-groq", Tokens: 100, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if err := o.Add(path, Event{TS: now, Profile: "free-groq", Tokens: 50, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	tok, req := o.Reading()
	if tok != 150 || req != 2 {
		t.Fatalf("reading = %d tokens / %d requests, want 150/2", tok, req)
	}
	if o.Per["free-groq"].Tokens != 150 {
		t.Errorf("per-provider total = %d, want 150", o.Per["free-groq"].Tokens)
	}
}

func TestOdometerPersistsAcrossLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 900, Requests: 3}); err != nil {
		t.Fatal(err)
	}
	again, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	tok, req := again.Reading()
	if tok != 900 || req != 3 {
		t.Fatalf("reloaded reading = %d/%d, want 900/3", tok, req)
	}
}

// TestOdometerNeverGoesBackward is the defining test. A corrupted or emptied
// state file must not zero a reading, and Add must never decrease one.
//
// It exercises the three paths that actually carry the monotonic invariant:
// the per-field clamp on a mixed-sign event, mergeUp reconciling a stale
// in-memory handle against a newer on-disk state, and recovery after the
// on-disk file is destroyed outright.
func TestOdometerNeverGoesBackward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 1000, Requests: 5}); err != nil {
		t.Fatal(err)
	}

	// A negative event must not reduce the reading. Both fields are
	// non-positive here, so this is caught by Add's early-return guard
	// before any clamping, merging, or disk I/O runs.
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: -500, Requests: -2}); err != nil {
		t.Fatal(err)
	}
	tok, req := o.Reading()
	if tok != 1000 || req != 5 {
		t.Fatalf("negative event moved the odometer to %d/%d, want 1000/5", tok, req)
	}

	// Mixed sign: one field positive, the other negative on the same event.
	// This does not hit the early-return guard (Tokens > 0), so it exercises
	// the per-field clamp instead: Tokens must still rise, and Requests must
	// be clamped to a no-op rather than subtracted.
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 100, Requests: -2}); err != nil {
		t.Fatal(err)
	}
	tok, req = o.Reading()
	if tok != 1100 || req != 5 {
		t.Fatalf("mixed-sign event produced %d/%d, want 1100/5 (tokens raised, requests clamped away)", tok, req)
	}

	// Stale instance vs newer disk: a second handle on the same path writes
	// ahead of the first. The first handle's in-memory state is now stale.
	// Its next Add must merge up to at least what the second handle already
	// persisted (the mergeUp path) before applying its own event, so the
	// reading can only go up, never back down to the stale in-memory value.
	second, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 200, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	secondTok, secondReq := second.Reading()

	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 10, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	tok, req = o.Reading()
	if tok < secondTok || req < secondReq {
		t.Fatalf("stale handle's Add produced %d/%d, below what the other handle already persisted (%d/%d)", tok, req, secondTok, secondReq)
	}
	if wantTok, wantReq := secondTok+10, secondReq+1; tok != wantTok || req != wantReq {
		t.Fatalf("stale handle's Add produced %d/%d, want %d/%d (merge up to the newer disk state, then apply its own event)", tok, req, wantTok, wantReq)
	}

	// Corruption mid-life: after real usage, the file on disk is destroyed
	// outright. This is the documented limit of the guarantee, not something
	// Add can paper over: nothing else records this data, so a destroyed
	// file legitimately restarts at zero. What must still hold is that Add
	// keeps working afterward and never moves a reading backward once it has
	// one again.
	if err := os.WriteFile(path, []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := corrupt.Reading(); tok != 0 {
		t.Fatalf("post-corruption load read %d, want 0 (destroyed file legitimately restarts)", tok)
	}
	if err := corrupt.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 5, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if tok, req := corrupt.Reading(); tok != 5 || req != 1 {
		t.Fatalf("Add after corruption produced %d/%d, want 5/1", tok, req)
	}
}

func TestOdometerRecoversFromCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	for _, junk := range []string{"", "{", "not json at all", `{"tokens":`} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		o, err := LoadOdometer(path)
		if err != nil {
			t.Fatalf("LoadOdometer(%q) returned error: %v", junk, err)
		}
		if tok, _ := o.Reading(); tok != 0 {
			t.Errorf("corrupt file %q yielded nonzero reading %d", junk, tok)
		}
		// It must still be usable.
		if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 10, Requests: 1}); err != nil {
			t.Errorf("Add after corrupt load failed: %v", err)
		}
	}
}

func TestOdometerConcurrentAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := LoadOdometer(path)
			if err != nil {
				t.Error(err)
				return
			}
			if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 10, Requests: 1}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	final, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	tok, req := final.Reading()
	if tok != 10*n || req != n {
		t.Fatalf("concurrent adds lost increments: %d/%d, want %d/%d", tok, req, 10*n, n)
	}
}

func TestOdometerRingEvictsOldEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := o.Add(path, Event{TS: old, Profile: "free-groq", Tokens: 100, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 100, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if len(o.Events) != 1 {
		t.Errorf("ring holds %d events, want 1 after evicting the 40-day-old one", len(o.Events))
	}
	// Eviction must NOT lower the lifetime reading.
	if tok, _ := o.Reading(); tok != 200 {
		t.Errorf("eviction changed the lifetime reading to %d, want 200", tok)
	}
}

func TestOdometerRingCapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	now := time.Now()
	for i := 0; i < ringMaxEvents+100; i++ {
		o.addLocked(Event{TS: now, Profile: "free-groq", Tokens: 1, Requests: 1})
	}
	if len(o.Events) > ringMaxEvents {
		t.Errorf("ring holds %d events, cap is %d", len(o.Events), ringMaxEvents)
	}
}
