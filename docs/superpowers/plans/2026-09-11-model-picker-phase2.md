# Model Picker Phase 2: Catalogue and Settings API - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve one list of every model with its provider, reachability, remaining allowance and links, plus a settings object holding preference order, thresholds, exclusions and custom links. No UI; this phase is verifiable with curl.

**Architecture:** A new `internal/modelcat` package assembles the catalogue from three sources already in the tree (the vendored registry, the compatibility table, the odometer). `internal/serve` gains three routes that expose it. Settings persist as one JSON object beside the odometer.

**Tech Stack:** Go 1.26.5, module `gophermind`. Standard library only.

**Spec:** `docs/superpowers/specs/2026-09-10-model-picker-design.md`, sections 3 and 6, plus the settings inventory in section 5.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only. `go.mod`/`go.sum` byte-unchanged.
- NO em dashes and NO emoji on any line you add. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call.
- `go build ./... && go test ./... -race` green before each commit.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## What already exists

| Symbol | Where |
|---|---|
| `Load() *Registry`, `Provider{Name,URL,BaseURL,Models}`, `Model{ID,Context,MaxOutput,Modality,RateLimit}` | `internal/freellm/registry.go` |
| `Compats() []Compat`, `CompatFor`, `Compat{Profile,Upstream,BaseURL,DefaultModel,Website,Affiliate,NoKey,Supported,Terms,Note}` | `internal/freellm/compat.go` |
| `TermsFlags(Terms) []string` | `internal/freellm/compat.go` |
| `QuotasFor(Compat) []Quota`, `Quota{Unit,Amount,Window}`, `Commas(int64) string` | `internal/freellm/quota.go` |
| `ModelTripMeters(o, c, model, now) []TripMeter`, `TripMeter{Quota,Used,HasQuota}` | `internal/freellm/usage.go` (phase 1) |
| `LoadOdometer(path)`, `OdometerPath()`, `ModelReading(profile, model)` | `internal/freellm/odometer.go` |
| Route pattern: `mux.Handle("GET /models", sessionWrap(modelsHandler(d.ListModels)))` | `internal/serve/webhook.go:330` |
| Handler style: method check, `Content-Type: application/json`, `json.NewEncoder(w).Encode` | `internal/serve/session_serve.go:130` |

---

### Task 1: Settings storage

**Files:**
- Create: `internal/modelcat/settings.go`
- Test: `internal/modelcat/settings_test.go`

**Interfaces produced:**
```go
type Settings struct {
	Order            []string          `json:"order,omitempty"`             // "profile/model" keys, most preferred first
	CycleOnCapacity  bool              `json:"cycle_on_capacity"`
	CapacityPercent  int               `json:"capacity_percent"`            // 0 means the default, 90
	WhenAllFull      string            `json:"when_all_full,omitempty"`     // "stay" (default) or "ask"
	FilterReachable  bool              `json:"filter_reachable"`
	FilterHasCapacity bool             `json:"filter_has_capacity"`
	ExcludedTerms    []string          `json:"excluded_terms,omitempty"`    // "non-commercial", "trains on prompts", "identity check"
	CustomLinks      map[string]string `json:"custom_links,omitempty"`
}

func DefaultSettings() Settings
func SettingsPath() string                       // honors GOPHERMIND_MODEL_SETTINGS, else beside the odometer
func LoadSettings(path string) (Settings, error) // a missing or corrupt file yields defaults, never an error
func SaveSettings(path string, s Settings) error // atomic temp+rename, 0600
func ValidateLink(u string) error                // http/https only
func (s Settings) CapacityThreshold() float64    // CapacityPercent as a fraction, defaulting to 0.9
```

`DefaultSettings` returns `CapacityPercent: 90`, `WhenAllFull: "stay"`, `FilterReachable: true`, everything else zero.

- [ ] **Step 1: Write the failing tests**

Create `internal/modelcat/settings_test.go`:

```go
package modelcat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAreTheDocumentedOnes(t *testing.T) {
	d := DefaultSettings()
	if d.CapacityPercent != 90 {
		t.Errorf("CapacityPercent = %d, want 90", d.CapacityPercent)
	}
	if d.WhenAllFull != "stay" {
		t.Errorf("WhenAllFull = %q, want \"stay\"", d.WhenAllFull)
	}
	if !d.FilterReachable {
		t.Error("FilterReachable should default on, so the dropdown opens short")
	}
	if d.CycleOnCapacity {
		t.Error("CycleOnCapacity must default off")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	in := DefaultSettings()
	in.Order = []string{"free-groq/openai/gpt-oss-120b", "free-ovhcloud/gpt-oss-120b"}
	in.ExcludedTerms = []string{"non-commercial"}
	in.CustomLinks = map[string]string{"free-groq": "https://groq.com/docs"}
	if err := SaveSettings(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Order) != 2 || out.Order[0] != in.Order[0] {
		t.Errorf("order did not round-trip: %v", out.Order)
	}
	if out.CustomLinks["free-groq"] != "https://groq.com/docs" {
		t.Errorf("custom link did not round-trip: %v", out.CustomLinks)
	}
}

// A damaged settings file must never block a turn.
func TestCorruptSettingsYieldDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	for _, junk := range []string{"", "{", "not json"} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadSettings(path)
		if err != nil {
			t.Errorf("LoadSettings(%q) errored: %v", junk, err)
		}
		if s.CapacityPercent != 90 {
			t.Errorf("corrupt file did not yield defaults: %+v", s)
		}
	}
}

func TestMissingSettingsYieldDefaults(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.CapacityPercent != 90 {
		t.Errorf("missing file did not yield defaults: %+v", s)
	}
}

// This is a security test, not a formatting one: these URLs are rendered as
// clickable links inside a WebView, where javascript: would execute in the
// app's own origin and file: would reach local disk.
func TestValidateLinkRejectsDangerousSchemes(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"vbscript:msgbox",
		"not a url at all",
		"",
	} {
		if err := ValidateLink(bad); err == nil {
			t.Errorf("ValidateLink(%q) accepted a dangerous or malformed URL", bad)
		}
	}
	for _, good := range []string{"http://example.test", "https://groq.com/docs"} {
		if err := ValidateLink(good); err != nil {
			t.Errorf("ValidateLink(%q) rejected a valid URL: %v", good, err)
		}
	}
}

func TestCapacityThresholdDefaults(t *testing.T) {
	var s Settings // zero value, as an old file would load
	if got := s.CapacityThreshold(); got != 0.9 {
		t.Errorf("zero CapacityPercent gave %v, want 0.9", got)
	}
	s.CapacityPercent = 50
	if got := s.CapacityThreshold(); got != 0.5 {
		t.Errorf("CapacityPercent 50 gave %v, want 0.5", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/modelcat/ -v`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement `internal/modelcat/settings.go`**

Follow `internal/freellm/odometer.go`'s save pattern for the atomic write (temp file in the same directory, `Chmod(0o600)`, `Sync`, rename). `ValidateLink` must parse with `net/url` and require `Scheme` to be exactly `http` or `https` AND a non-empty `Host`; a bare string like `not a url at all` parses without error in Go, so the scheme check alone is not enough.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/modelcat/ -race -count=1`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/modelcat/
git commit -m "feat(modelcat): settings store for model preference and links

One JSON object beside the odometer: preference order, capacity
threshold, term exclusions, filter defaults and custom links. A missing
or corrupt file yields defaults rather than an error, because a damaged
preference file must never block a turn.

ValidateLink accepts only http and https. That is a security boundary,
not tidiness: these URLs render as clickable links in a WebView, where a
javascript: URL would execute in the app's own origin.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 2: Catalogue assembly

**Files:**
- Create: `internal/modelcat/catalogue.go`
- Test: `internal/modelcat/catalogue_test.go`

**Interfaces produced:**
```go
type Entry struct {
	ID, Provider, Profile     string
	Reachable                 bool
	Reason                    string
	Used                      int64
	Quota                     int64
	Unit, Window              string
	Context, Modality         string
	Terms                     []string
	ProviderURL, ModelURL     string
	NearCapacity              bool
}

// Build assembles the catalogue. endpointModels are the ids the active
// configured endpoint serves (nil when it could not be reached); they appear
// with an empty Profile.
func Build(o *freellm.Odometer, s Settings, endpointModels []string, now time.Time) []Entry
func DeriveModelURL(id string) string
func Reachability(c freellm.Compat) (bool, string)
```

**Reachability rules, computed and never guessed:**
- `Supported: false` -> not reachable, `Reason` is the entry's `Note`.
- `NoKey: true` -> reachable.
- otherwise reachable only when `os.Getenv(apiKeyEnv(profile))` is non-empty; `Reason` names that exact variable.

**Link resolution order** (first match wins): `Settings.CustomLinks["profile/model"]`, then `CustomLinks["profile"]`, then `DeriveModelURL(id)` for `ModelURL`, then `Compat.Website` for `ProviderURL`, then empty.

**`DeriveModelURL`** returns a Hugging Face URL for an `owner/name` id (exactly one slash, both parts non-empty, no spaces) and `""` otherwise.

- [ ] **Step 1: Write the failing tests**

Create `internal/modelcat/catalogue_test.go` covering, at minimum:

```go
package modelcat

import (
	"strings"
	"testing"
	"time"

	"gophermind/internal/freellm"
)

func TestBuildIncludesEveryRegistryModel(t *testing.T) {
	entries := Build(nil, DefaultSettings(), nil, time.Now())
	if len(entries) < 100 {
		t.Fatalf("catalogue has %d entries, expected the full registry (100+)", len(entries))
	}
}

func TestNoKeyProviderIsReachable(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Profile == "free-ovhcloud" {
			if !e.Reachable {
				t.Errorf("a no-key provider reported unreachable: %s", e.Reason)
			}
			return
		}
	}
	t.Fatal("free-ovhcloud not in the catalogue")
}

func TestKeyedProviderReachabilityFollowsTheEnvVar(t *testing.T) {
	find := func(entries []Entry) *Entry {
		for i := range entries {
			if entries[i].Profile == "free-groq" {
				return &entries[i]
			}
		}
		return nil
	}
	e := find(Build(nil, DefaultSettings(), nil, time.Now()))
	if e == nil {
		t.Fatal("free-groq not in the catalogue")
	}
	if e.Reachable {
		t.Skip("GOPHERMIND_PROFILE_FREE_GROQ_API_KEY is set in this environment")
	}
	if !strings.Contains(e.Reason, "GOPHERMIND_PROFILE_FREE_GROQ_API_KEY") {
		t.Errorf("reason does not name the variable to set: %q", e.Reason)
	}

	t.Setenv("GOPHERMIND_PROFILE_FREE_GROQ_API_KEY", "k")
	if e2 := find(Build(nil, DefaultSettings(), nil, time.Now())); e2 == nil || !e2.Reachable {
		t.Error("setting the key did not make the provider reachable")
	}
}

func TestUnsupportedProviderCarriesItsNote(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Profile == "free-cloudflare" {
			if e.Reachable {
				t.Error("an unsupported provider reported reachable")
			}
			if e.Reason == "" {
				t.Error("unsupported provider has no reason")
			}
			return
		}
	}
}

// The denominator rule, again, at the catalogue layer.
func TestNoQuotaIsInventedOrNearCapacity(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Quota == 0 && e.NearCapacity {
			t.Errorf("%s/%s is NearCapacity with no published quota", e.Profile, e.ID)
		}
	}
}

// A model on the user's own endpoint has no provider and no quota.
func TestEndpointModelsAppearWithNoProfile(t *testing.T) {
	entries := Build(nil, DefaultSettings(), []string{"qwen3.6-35b-a3b"}, time.Now())
	for _, e := range entries {
		if e.ID == "qwen3.6-35b-a3b" && e.Profile == "" {
			if e.Quota != 0 {
				t.Error("a local endpoint model reported a quota")
			}
			if !e.Reachable {
				t.Error("a model the endpoint serves reported unreachable")
			}
			return
		}
	}
	t.Fatal("endpoint model missing from the catalogue")
}

func TestDeriveModelURL(t *testing.T) {
	if got := DeriveModelURL("Qwen/Qwen3-8B"); got != "https://huggingface.co/Qwen/Qwen3-8B" {
		t.Errorf("got %q", got)
	}
	for _, id := range []string{"gpt-oss-120b", "glm-4.7-flash", "a/b/c", "", "has space/x"} {
		if got := DeriveModelURL(id); got != "" {
			t.Errorf("DeriveModelURL(%q) = %q, want empty rather than a guess", id, got)
		}
	}
}

func TestCustomLinkBeatsDerived(t *testing.T) {
	s := DefaultSettings()
	s.CustomLinks = map[string]string{"free-huggingface/Qwen/Qwen2.5-7B-Instruct": "https://example.test/mine"}
	for _, e := range Build(nil, s, nil, time.Now()) {
		if e.Profile == "free-huggingface" && e.ID == "Qwen/Qwen2.5-7B-Instruct" {
			if e.ModelURL != "https://example.test/mine" {
				t.Errorf("custom link did not win: %q", e.ModelURL)
			}
			return
		}
	}
	t.Fatal("target model not in the catalogue")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/modelcat/ -run 'TestBuild|TestDerive|TestCustom|TestNoKey|TestKeyed|TestUnsupported|TestNoQuota|TestEndpoint' -v`
Expected: FAIL to build, `undefined: Build`.

- [ ] **Step 3: Implement `internal/modelcat/catalogue.go`**

Use `freellm.ModelTripMeters` for `Used`/`Quota`/`Unit`/`Window` and for `NearCapacity` (compare the meter's fraction against `s.CapacityThreshold()`, and only when `HasQuota`). Reuse `freellm.TermsFlags` for `Terms`; do NOT reimplement that mapping.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/modelcat/ -race -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/modelcat/
git commit -m "feat(modelcat): assemble the model catalogue

One entry per model, from the vendored registry, the compatibility table
and the odometer. Reachability is computed rather than guessed: a no-key
provider is reachable, a keyed one only when its env var is set, and the
reason names the exact variable.

The denominator rule carries over: no published quota means no quota
field and never NearCapacity, so nothing can be auto-switched away from
on an invented number. Model URLs are derived only for owner/name ids and
otherwise omitted rather than guessed into a 404.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 3: The three routes

**Files:**
- Create: `internal/serve/catalogue.go`
- Modify: `internal/serve/webhook.go` (register the routes, extend `Deps`)
- Test: `internal/serve/catalogue_test.go`

**Interfaces produced:** `Deps.EndpointModels func() []string` (optional; nil means the catalogue omits local-endpoint entries).

Routes, registered beside `GET /models` and behind the same `sessionWrap` auth:

```
GET   /models/catalogue
GET   /models/settings
PATCH /models/settings
```

`PATCH` accepts a partial object and merges it over the stored one. It must reject any custom link failing `modelcat.ValidateLink` with **400** and a body naming the offending scheme, storing nothing.

- [ ] **Step 1: Write the failing tests**

Cover: catalogue returns JSON with entries and requires the bearer token; settings GET returns defaults on a fresh store; PATCH merges a partial object and persists; PATCH with a `javascript:` custom link returns 400 and does not persist; a method other than GET/PATCH returns 405. Use `httptest` and point the settings path at `t.TempDir()` via the env var.

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Implement**

Match the handler style in `internal/serve/session_serve.go:130`: method check first, `Content-Type: application/json`, `json.NewEncoder(w).Encode`.

- [ ] **Step 4: Run the whole suite**

Run: `go build ./... && go test ./... -race`

- [ ] **Step 5: Verify with curl against a real server**

```bash
go build -o /tmp/gm-p2 ./cmd/gophermind
GOPHERMIND_SERVE_TOKEN=t GOPHERMIND_SERVE_ADDR=127.0.0.1:8097 \
  GOPHERMIND_MODEL_SETTINGS=/tmp/ms.json nohup /tmp/gm-p2 serve >/tmp/p2.log 2>&1 &
sleep 3
echo "--- catalogue size ---"
curl -s -H 'Authorization: Bearer t' http://127.0.0.1:8097/models/catalogue | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("entries",[])), "entries")'
echo "--- one reachable entry ---"
curl -s -H 'Authorization: Bearer t' http://127.0.0.1:8097/models/catalogue | python3 -c 'import json,sys; [print(json.dumps(e)) for e in json.load(sys.stdin)["entries"] if e.get("reachable")][:1]' | head -1
echo "--- settings defaults ---"
curl -s -H 'Authorization: Bearer t' http://127.0.0.1:8097/models/settings
echo "--- a dangerous link must be refused ---"
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH -H 'Authorization: Bearer t' -H 'Content-Type: application/json' \
  -d '{"custom_links":{"free-groq":"javascript:alert(1)"}}' http://127.0.0.1:8097/models/settings
pkill -f 'gm-p2 serve'; rm -f /tmp/gm-p2 /tmp/p2.log /tmp/ms.json
```
Expected: a three-figure entry count, one reachable entry printed, defaults with `capacity_percent: 90`, and **400** for the javascript link. Paste the real output.

- [ ] **Step 6: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green.
- [ ] `go.mod`/`go.sum` unchanged.
- [ ] The curl run above produced a catalogue, defaults, and a 400 for a dangerous link.
- [ ] No em dashes on added lines.
