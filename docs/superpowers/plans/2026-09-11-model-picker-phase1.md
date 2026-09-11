# Model Picker Phase 1: Per-Model Metering - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track free-tier usage per model, not only per provider, so a later phase can show remaining allowance beside each model name and switch away from one before its quota runs out.

**Architecture:** Purely additive to `internal/freellm`. `Event` gains a `Model` field and `Odometer` gains a `PerModel` map beside the existing `Per`. Nothing existing changes shape, so a state file written before this loads with its lifetime reading untouched.

**Tech Stack:** Go 1.26.5, module `gophermind`. Standard library only.

**Spec:** `docs/superpowers/specs/2026-09-10-model-picker-design.md`, sections 1 and 2.

## Global Constraints

- Module is `gophermind`; import paths are `gophermind/internal/...`.
- Go 1.26.5. Standard library only. `go.mod` and `go.sum` must stay byte-unchanged.
- NO em dashes and NO emoji on any line you add. Plain hyphens.
- Every exported symbol gets a doc comment.
- **The odometer's monotonic guarantee is the thing most at risk here, and it has already carried a false claim once.** No change may let any reading decrease. Tests come before the change.
- `go build ./... && go test ./... -race` green.
- Commit after each task, message ending with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## Existing surface this extends

Read these before editing. Signatures are current as of this branch:

| Symbol | File |
|---|---|
| `type Event struct { TS; Profile; Tokens; Requests }` | `internal/freellm/odometer.go` |
| `type Odometer struct { Tokens; Requests; Per; Since; Events }` | `internal/freellm/odometer.go` |
| `func LoadOdometer(path string) (*Odometer, error)` | `odometer.go:95` |
| `func (o *Odometer) Add(path string, e Event) error` | `odometer.go:127` |
| `func (o *Odometer) mergeUp(other *Odometer)` | `odometer.go:157` |
| `func (o *Odometer) addLocked(e Event)` | `odometer.go:201` |
| `func TripMeters(o *Odometer, c Compat, now time.Time) []TripMeter` | `usage.go:48` |
| `func sumWindow(o *Odometer, profile string, unit Unit, now, window) int64` | `usage.go:66` |
| The one real write site | `cmd/gophermind/main.go:1387` |

---

### Task 1: Backward-compatibility test, written FIRST

**Files:**
- Test: `internal/freellm/odometer_compat_test.go` (new)

This task adds no production code. It pins the behavior the next task must not break, against the CURRENT shape, so it is meaningful before `PerModel` exists.

- [ ] **Step 1: Write the test**

Create `internal/freellm/odometer_compat_test.go`:

```go
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
```

- [ ] **Step 2: Run it against the CURRENT code**

Run: `go test ./internal/freellm/ -run TestLegacy -v`
Expected: **PASS.** These describe behavior that already holds. If any fails now, stop and report it: that is a pre-existing bug, not something Task 2 introduced.

- [ ] **Step 3: Commit**

```bash
git add internal/freellm/odometer_compat_test.go
git commit -m "test(freellm): pin odometer backward compatibility before adding per-model

Written against the current on-disk shape, as a JSON literal rather than
a marshalled struct, so it keeps testing the old format after the struct
grows. The odometer has carried a false monotonicity claim once already;
this is the guard for the next change rather than a description of it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 2: Per-model accumulation

**Files:**
- Modify: `internal/freellm/odometer.go`
- Test: `internal/freellm/odometer_permodel_test.go` (new)

**Interfaces:**
- Produces: `Event.Model`, `Odometer.PerModel`, `func ModelKey(profile, model string) string`, `func (o *Odometer) ModelReading(profile, model string) ProviderTotal`.

- [ ] **Step 1: Write the failing tests**

Create `internal/freellm/odometer_permodel_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/freellm/ -run 'TestPerModel|TestEventWithout|TestModelKey' -v`
Expected: FAIL to build, `e.Model undefined` and `o.PerModel undefined`.

- [ ] **Step 3: Implement**

In `internal/freellm/odometer.go`:

Add to `Event`, after `Profile`:
```go
	// Model is the model that served this turn. Empty when the caller does not
	// know it, including every event written before per-model metering existed;
	// such an event still counts toward its provider, just not toward a model.
	Model string `json:"model,omitempty"`
```

Add to `Odometer`, after `Per`:
```go
	// PerModel is the lifetime total per model, keyed by ModelKey. It is
	// additive alongside Per rather than replacing it: a state file written
	// before this field existed loads with PerModel nil and its lifetime
	// reading untouched.
	PerModel map[string]ProviderTotal `json:"per_model,omitempty"`
```

Add:
```go
// ModelKey is the PerModel key for one model at one provider. Profile and
// model are joined rather than nested so mergeUp stays a flat loop over one
// map shape.
func ModelKey(profile, model string) string { return profile + "/" + model }

// ModelReading returns the lifetime total for one model. A model that has
// never been used reads as a zero total rather than a missing entry, so a
// caller can render it without a lookup dance.
func (o *Odometer) ModelReading(profile, model string) ProviderTotal {
	if o == nil || o.PerModel == nil {
		return ProviderTotal{}
	}
	return o.PerModel[ModelKey(profile, model)]
}
```

In `addLocked`, after the existing `o.Per[e.Profile] = t` block, accumulate the model total the same way, guarded on `e.Model != ""`. Allocate `o.PerModel` lazily exactly as `Per` is.

In `mergeUp`, raise `PerModel` with the identical per-field, never-downward logic already applied to `Per`. **Do not write a second merge shape**; if the two loops end up copy-pasted, extract one helper that merges a `map[string]ProviderTotal` and call it twice.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/freellm/ -race -count=1`
Expected: all green, including Task 1's legacy tests unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): meter usage per model, additively

Event gains an omitempty Model and Odometer gains PerModel beside the
existing Per. A state file written before this loads with PerModel nil
and its lifetime reading untouched; an event with no model still counts
toward its provider, which is what old ring entries look like.

mergeUp raises PerModel with the same never-downward logic as Per,
through one shared helper rather than a second merge shape.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 3: Per-model trip meters, and record the model on a real turn

**Files:**
- Modify: `internal/freellm/usage.go`
- Modify: `cmd/gophermind/main.go` (the write site at ~line 1387)
- Modify: `desktop/deps.go` (its odometer write, if present)
- Test: `internal/freellm/usage_permodel_test.go` (new)

**Interfaces:**
- Produces: `func ModelTripMeters(o *Odometer, c Compat, model string, now time.Time) []TripMeter`.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/usage_permodel_test.go`:

```go
package freellm

import (
	"testing"
	"time"
)

func TestModelTripMetersCountOnlyThatModel(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	c, ok := CompatFor("free-groq") // 30 RPM, 1,000 RPD
	if !ok {
		t.Fatal("free-groq missing")
	}
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "a", Tokens: 10, Requests: 1},
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "b", Tokens: 10, Requests: 1},
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "a", Tokens: 10, Requests: 1},
	}}

	ms := ModelTripMeters(o, c, "a", now)
	if len(ms) == 0 {
		t.Fatal("expected trip meters for model a")
	}
	for _, m := range ms {
		if m.Quota.Unit == UnitRequests && m.Quota.Window == 24*time.Hour && m.Used != 2 {
			t.Errorf("model a per-day requests = %d, want 2 (model b must not count)", m.Used)
		}
	}
}

// An event with no model must not be attributed to a named model.
func TestModelTripMetersIgnoreModellessEvents(t *testing.T) {
	now := time.Now()
	c, _ := CompatFor("free-groq")
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now, Profile: "free-groq", Tokens: 99, Requests: 1},
	}}
	for _, m := range ModelTripMeters(o, c, "a", now) {
		if m.Used != 0 {
			t.Errorf("a model-less event counted toward model a: used = %d", m.Used)
		}
	}
}

// The denominator rule from the free-provider spec still holds per model.
func TestModelTripMetersNeverInventADenominator(t *testing.T) {
	now := time.Now()
	c, ok := CompatFor("free-ollama-cloud") // unpublished limits
	if !ok {
		t.Fatal("free-ollama-cloud missing")
	}
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now, Profile: "free-ollama-cloud", Model: "gpt-oss:120b", Tokens: 5, Requests: 1},
	}}
	ms := ModelTripMeters(o, c, "gpt-oss:120b", now)
	if len(ms) != 1 {
		t.Fatalf("got %d meters, want 1 fallback", len(ms))
	}
	if ms[0].HasQuota {
		t.Error("invented a quota for a provider that publishes none")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/freellm/ -run TestModelTrip -v`
Expected: FAIL to build, `undefined: ModelTripMeters`.

- [ ] **Step 3: Implement**

In `internal/freellm/usage.go`, add a model filter to the window sum and a
`ModelTripMeters` that mirrors `TripMeters`. Do not duplicate `TripMeters`'
body: give `sumWindow` an extra model parameter (empty meaning "any model") and
have both call it, so the quota-less fallback and the warn threshold cannot
drift between the two.

In `cmd/gophermind/main.go`, set `Model: cfg.Model` on the `freellm.Event`
built at the odometer write site. Locate it by content (`odo.Add`), not by line
number. Do the same in `desktop/deps.go` if it writes the odometer.

- [ ] **Step 4: Run everything**

Run: `go build ./... && go test ./... -race`
Expected: all green.

- [ ] **Step 5: See it work end to end**

```bash
go build -o /tmp/gm-p1 ./cmd/gophermind
export GOPHERMIND_ODOMETER=/tmp/odo-p1.json
rm -f "$GOPHERMIND_ODOMETER"*
timeout 90 /tmp/gm-p1 --profile free-ovhcloud ask "Name one colour. One word." 2>&1 | tail -2
/tmp/gm-p1 free usage
python3 -c "import json;d=json.load(open('/tmp/odo-p1.json'));print('per_model:', d.get('per_model'))"
rm -f /tmp/gm-p1 /tmp/odo-p1.json*
```
Expected: `per_model` contains one entry keyed `free-ovhcloud/gpt-oss-120b` with
nonzero tokens. Paste the real output into your report. This is the proof the
whole phase works, not just the unit tests.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(freellm): per-model trip meters, and record the model per turn

ModelTripMeters mirrors TripMeters through a shared sumWindow rather than
a copy, so the quota-less fallback and the warn threshold cannot drift
between them. The denominator rule is unchanged: a provider that
publishes no limit still gets a bare count per model, never a guess.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green.
- [ ] `go.mod` / `go.sum` unchanged: `git diff --stat 6f5a9fc..HEAD -- go.mod go.sum` is empty.
- [ ] A legacy odometer file upgrades in place with its reading intact (Task 1's tests).
- [ ] A real turn writes a `per_model` entry (Task 3 Step 5).
- [ ] No em dashes on added lines: `git diff 6f5a9fc..HEAD -- '*.go' | grep '^+' | grep -c '—'` is 0.
