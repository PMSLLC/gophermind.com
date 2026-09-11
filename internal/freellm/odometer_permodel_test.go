package freellm

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPerModelAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	now := time.Now()

	for _, e := range []Event{
		{TS: now, Profile: "free-groq", Model: "openai/gpt-oss-120b", Tokens: 100, Requests: 1},
		{TS: now, Profile: "free-groq", Model: "openai/gpt-oss-120b", Tokens: 50, Requests: 1},
		{TS: now, Profile: "free-groq", Model: "qwen/qwen3.6-27b", Tokens: 7, Requests: 1},
	} {
		if err := o.Add(path, e); err != nil {
			t.Fatal(err)
		}
	}

	if got := o.ModelReading("free-groq", "openai/gpt-oss-120b"); got.Tokens != 150 || got.Requests != 2 {
		t.Errorf("gpt-oss total = %d/%d, want 150/2", got.Tokens, got.Requests)
	}
	if got := o.ModelReading("free-groq", "qwen/qwen3.6-27b"); got.Tokens != 7 {
		t.Errorf("qwen total = %d, want 7", got.Tokens)
	}
	// The per-provider total still sums both models.
	if got := o.Per["free-groq"].Tokens; got != 157 {
		t.Errorf("per-provider total = %d, want 157", got)
	}
}

// An event with no model still counts toward the provider. Old ring entries
// look like this, and so does any caller that does not know its model.
func TestEventWithoutModelStillCountsProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 10, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if got := o.Per["free-groq"].Tokens; got != 10 {
		t.Errorf("per-provider total = %d, want 10", got)
	}
	if len(o.PerModel) != 0 {
		t.Errorf("a model-less event created %d per-model entries, want 0", len(o.PerModel))
	}
}

// The monotonic guarantee must hold for PerModel exactly as it does for Per.
func TestPerModelNeverDecreases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	now := time.Now()
	if err := o.Add(path, Event{TS: now, Profile: "free-groq", Model: "m", Tokens: 100, Requests: 2}); err != nil {
		t.Fatal(err)
	}

	// A stale in-memory instance writing after a newer file must not lower it.
	stale, _ := LoadOdometer(path)
	fresh, _ := LoadOdometer(path)
	if err := fresh.Add(path, Event{TS: now, Profile: "free-groq", Model: "m", Tokens: 500, Requests: 5}); err != nil {
		t.Fatal(err)
	}
	if err := stale.Add(path, Event{TS: now, Profile: "free-groq", Model: "m", Tokens: 1, Requests: 1}); err != nil {
		t.Fatal(err)
	}

	final, _ := LoadOdometer(path)
	got := final.ModelReading("free-groq", "m")
	if got.Tokens < 600 {
		t.Errorf("per-model reading went backward: %d tokens, want at least 600", got.Tokens)
	}
}

// A legacy file has no per_model object at all; writing to it must work.
func TestPerModelOnLegacyFile(t *testing.T) {
	path := writeLegacyOdometer(t)
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-ovhcloud", Model: "gpt-oss-120b", Tokens: 10, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if got := o.ModelReading("free-ovhcloud", "gpt-oss-120b"); got.Tokens != 10 {
		t.Errorf("per-model on a legacy file = %d, want 10", got.Tokens)
	}
	// And the pre-existing lifetime reading is untouched by the upgrade.
	if tok, _ := o.Reading(); tok != 4516 {
		t.Errorf("lifetime reading = %d, want 4516 (4506 legacy + 10)", tok)
	}
}

func TestModelKeyIsStable(t *testing.T) {
	if got := ModelKey("free-groq", "openai/gpt-oss-120b"); got != "free-groq/openai/gpt-oss-120b" {
		t.Errorf("ModelKey = %q", got)
	}
}
