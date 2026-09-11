package freellm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A state file in the shape written BEFORE per-model metering existed. It is a
// literal, not a marshalled struct, so it keeps testing the old on-disk shape
// even after the struct grows.
const legacyOdometerJSON = `{
  "tokens": 4506,
  "requests": 3,
  "per": {
    "free-ovhcloud": {
      "tokens": 4506,
      "requests": 3,
      "first_seen": "2026-09-10T12:00:00Z",
      "last_seen": "2026-09-10T12:40:00Z"
    }
  },
  "since": "2026-09-10T12:00:00Z",
  "events": [
    {"ts": "2026-09-10T12:40:00Z", "profile": "free-ovhcloud", "tokens": 4506, "requests": 1}
  ]
}`

func writeLegacyOdometer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "odo.json")
	if err := os.WriteFile(path, []byte(legacyOdometerJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The reading a user already has must survive the upgrade untouched. This is
// the whole point of the additive design.
func TestLegacyOdometerLoadsWithReadingIntact(t *testing.T) {
	o, err := LoadOdometer(writeLegacyOdometer(t))
	if err != nil {
		t.Fatal(err)
	}
	tok, req := o.Reading()
	if tok != 4506 || req != 3 {
		t.Fatalf("legacy reading = %d/%d, want 4506/3", tok, req)
	}
	if got := o.Per["free-ovhcloud"].Tokens; got != 4506 {
		t.Errorf("per-provider total = %d, want 4506", got)
	}
	if len(o.Events) != 1 {
		t.Errorf("legacy ring has %d events, want 1", len(o.Events))
	}
}

// Adding to a legacy file must raise the reading, never reset it.
func TestLegacyOdometerAcceptsNewWrites(t *testing.T) {
	path := writeLegacyOdometer(t)
	o, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-ovhcloud", Tokens: 100, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	tok, req := o.Reading()
	if tok != 4606 || req != 4 {
		t.Fatalf("after add, reading = %d/%d, want 4606/4", tok, req)
	}

	reloaded, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := reloaded.Reading(); tok != 4606 {
		t.Errorf("reloaded reading = %d, want 4606", tok)
	}
}

// A legacy file must round-trip through load and save without losing fields a
// future reader still needs.
func TestLegacyOdometerRoundTripKeepsKnownFields(t *testing.T) {
	path := writeLegacyOdometer(t)
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 5, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("saved odometer is not valid JSON: %v", err)
	}
	for _, k := range []string{"tokens", "requests", "per", "since", "events"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("saved odometer dropped the %q field", k)
		}
	}
}
