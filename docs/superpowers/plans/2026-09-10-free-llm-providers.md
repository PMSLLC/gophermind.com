# Free LLM Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user with no API key and no local server run gophermind against a free provider, see which provider is serving them, and watch a lifetime odometer of free tokens and requests used.

**Architecture:** A new `internal/freellm` package embeds a vendored CC0 registry (`data.json` from `mnfst/awesome-free-llm-apis`) and pairs it with a hand-maintained compatibility table. Registry entries surface as `free-*` profiles through the existing `Config.ApplyProfile` seam, so `llm.Client` is untouched. A self-contained odometer file records lifetime totals plus a 31-day event ring that trip meters derive from.

**Tech Stack:** Go 1.25.0, module `gophermind`. Standard library only: `embed`, `encoding/json`, `net/http`, `testing`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-10-free-llm-providers-design.md` (including the odometer addendum at the end)

## Global Constraints

- Module is `gophermind`; import paths are `gophermind/internal/...`.
- Go 1.25.0. Standard library only for this feature. Do not add a dependency.
- **No API key material may ever appear in `compat.go` or any tracked file.** Keys come only from `GOPHERMIND_PROFILE_<NAME>_API_KEY`.
- **No em dashes and no emoji** in any new file, comment, doc, or commit message. Existing files that already use them keep their own style.
- `internal/freellm/data.json` is vendored verbatim and is NEVER hand-edited. Only `scripts/sync-free-providers.sh` rewrites it.
- Every new exported symbol gets a doc comment, matching the density of `internal/usagelog` and `internal/config`.
- Tests use the standard library `testing` package and `t.TempDir()`, matching `internal/usagelog/usagelog_test.go`. No test may make a network call.
- Run `go build ./... && go test ./...` before every commit. Both must be green.
- Commit after each task with the trailer:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## File Structure

| File | Responsibility |
|---|---|
| `internal/freellm/data.json` | Vendored upstream registry. Never hand-edited. |
| `internal/freellm/registry.go` | Embed and parse `data.json`. `Provider`, `Model`, `Load`, `Lookup`, `All`. |
| `internal/freellm/compat.go` | The only hand-maintained data: `Compat` per profile. |
| `internal/freellm/quota.go` | Parse upstream free-text rate limits into `Quota`. |
| `internal/freellm/odometer.go` | Lifetime monotonic totals + 31-day event ring, file-locked. |
| `internal/freellm/usage.go` | Derive trip meters from the ring against quotas. |
| `internal/freellm/attribution.go` | Render provider attribution; enforce referral disclosure. |
| `internal/freellm/check.go` | Probe an endpoint's `/models`. Only networked file in the package. |
| `cmd/gophermind/free.go` | `gophermind free list|show|check|usage`. |
| `scripts/sync-free-providers.sh` | Refresh the vendored registry and regenerate docs. |
| `docs/free-providers.md` | Generated provider table. Never hand-edited. |
| `docs/affiliate-plan.md` | Hand-written policy and outreach plan. |

Tasks 1-8 build `internal/freellm` bottom-up with no dependency on the rest of the repo. Tasks 9-12 wire it in.

---

### Task 1: Vendor the registry and parse it

**Files:**
- Create: `internal/freellm/data.json` (copied from upstream, verbatim)
- Create: `internal/freellm/registry.go`
- Test: `internal/freellm/registry_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Provider struct{ Name, Category, Country, URL, BaseURL, Description string; Models []Model }`, `type Model struct{ ID, Name, Context, MaxOutput, Modality, RateLimit string }`, `func Load() *Registry`, `func (r *Registry) Lookup(name string) (Provider, bool)`, `func (r *Registry) All() []Provider`, `func (r *Registry) LastUpdated() string`.

- [ ] **Step 1: Vendor the data file**

```bash
mkdir -p internal/freellm
curl -fsSL https://raw.githubusercontent.com/mnfst/awesome-free-llm-apis/main/data.json \
  -o internal/freellm/data.json
jq -e '.providers | length > 0' internal/freellm/data.json
```

Expected: prints `true`. If `curl` fails, the repo is public at `github.com/mnfst/awesome-free-llm-apis`; fetch `data.json` from the default branch by any means and do not modify it.

- [ ] **Step 2: Write the failing test**

Create `internal/freellm/registry_test.go`:

```go
package freellm

import "testing"

func TestLoadParsesEmbeddedRegistry(t *testing.T) {
	r := Load()
	if got := len(r.All()); got == 0 {
		t.Fatalf("embedded registry has no providers")
	}
	if r.LastUpdated() == "" {
		t.Error("registry has no lastUpdated date")
	}
}

func TestLookupHitAndMiss(t *testing.T) {
	r := Load()
	p, ok := r.Lookup("Groq")
	if !ok {
		t.Fatal("Groq not found in registry")
	}
	if len(p.Models) == 0 {
		t.Error("Groq has no models")
	}
	if _, ok := r.Lookup("No Such Provider"); ok {
		t.Error("Lookup reported a hit for a provider that does not exist")
	}
}

func TestEveryProviderHasNameAndURL(t *testing.T) {
	for _, p := range Load().All() {
		if p.Name == "" {
			t.Error("a provider has an empty name")
		}
		if p.URL == "" {
			t.Errorf("provider %q has no signup URL", p.Name)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestLoad -v`
Expected: FAIL to build, `undefined: Load`.

- [ ] **Step 4: Write the implementation**

Create `internal/freellm/registry.go`:

```go
// Package freellm exposes a vendored registry of free LLM API providers so
// gophermind can run against a free endpoint without a paid key, and can
// attribute and meter that usage.
//
// The registry data in data.json is vendored verbatim from
// github.com/mnfst/awesome-free-llm-apis (CC0 1.0) and is never hand-edited;
// scripts/sync-free-providers.sh refreshes it. Everything gophermind knows
// that upstream does not lives in compat.go.
package freellm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data.json
var rawRegistry []byte

// Model is one model a provider offers on its free tier. Every field is
// upstream free text; RateLimit is parsed opportunistically by quota.go.
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Context   string `json:"context"`
	MaxOutput string `json:"maxOutput"`
	Modality  string `json:"modality"`
	RateLimit string `json:"rateLimit"`
}

// Provider is one entry in the upstream registry.
type Provider struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Country     string  `json:"country"`
	URL         string  `json:"url"`
	BaseURL     string  `json:"baseUrl"`
	Description string  `json:"description"`
	Models      []Model `json:"models"`
}

// Registry is the parsed contents of the embedded data.json.
type Registry struct {
	lastUpdated string
	providers   []Provider
	byName      map[string]Provider
}

var (
	loadOnce sync.Once
	loaded   *Registry
)

// Load parses the embedded registry, once per process. The data is compiled
// in, so a parse failure is a build defect rather than a runtime condition and
// panics; TestLoadParsesEmbeddedRegistry catches it before release.
func Load() *Registry {
	loadOnce.Do(func() {
		var doc struct {
			LastUpdated string     `json:"lastUpdated"`
			Providers   []Provider `json:"providers"`
		}
		if err := json.Unmarshal(rawRegistry, &doc); err != nil {
			panic(fmt.Sprintf("freellm: embedded data.json is malformed: %v", err))
		}
		byName := make(map[string]Provider, len(doc.Providers))
		for _, p := range doc.Providers {
			byName[p.Name] = p
		}
		loaded = &Registry{lastUpdated: doc.LastUpdated, providers: doc.Providers, byName: byName}
	})
	return loaded
}

// Lookup returns the provider with the given upstream name.
func (r *Registry) Lookup(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// All returns every provider in upstream order.
func (r *Registry) All() []Provider { return r.providers }

// LastUpdated is the date upstream stamped on the vendored data, so callers
// can show how stale the registry is.
func (r *Registry) LastUpdated() string { return r.lastUpdated }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS, three tests.

- [ ] **Step 6: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): vendor and parse the free-provider registry

data.json is vendored verbatim from mnfst/awesome-free-llm-apis (CC0)
and embedded, so the registry works offline and is byte-reproducible.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 2: Compatibility table and the drift test

**Files:**
- Create: `internal/freellm/compat.go`
- Test: `internal/freellm/compat_test.go`

**Interfaces:**
- Consumes: `Load`, `Registry.Lookup`, `Provider` from Task 1.
- Produces: `type Terms struct{ NonCommercial, TrainsOnPrompts, IdentityCheck bool }`, `type Compat struct{ Profile, Upstream, BaseURL, DefaultModel, Website, Affiliate string; NoKey, Supported bool; Terms Terms; Note string }`, `func Compats() []Compat` (sorted: no-key first, then supported, then unsupported, each alphabetical by Profile), `func CompatFor(profile string) (Compat, bool)`, `const ProfilePrefix = "free-"`.

**Note on `Supported`:** every entry below ships `Supported: true` except Cloudflare. Task 8's optional verification step corrects any that fail a live probe. Do not guess; leave them true until a probe says otherwise.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/compat_test.go`:

```go
package freellm

import (
	"sort"
	"strings"
	"testing"
)

// TestCompatUpstreamResolves is the drift guard. If a sync drops or renames a
// provider, this fails and names it, instead of shipping a profile that 404s.
func TestCompatUpstreamResolves(t *testing.T) {
	r := Load()
	for _, c := range Compats() {
		if _, ok := r.Lookup(c.Upstream); !ok {
			t.Errorf("profile %q names upstream provider %q, which is not in data.json", c.Profile, c.Upstream)
		}
	}
}

// TestDefaultModelExistsUpstream is the other half of the drift guard: a model
// upstream renamed must not stay as a profile default.
func TestDefaultModelExistsUpstream(t *testing.T) {
	r := Load()
	for _, c := range Compats() {
		if !c.Supported {
			continue
		}
		p, ok := r.Lookup(c.Upstream)
		if !ok {
			continue // reported by TestCompatUpstreamResolves
		}
		found := false
		for _, m := range p.Models {
			if m.ID == c.DefaultModel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("profile %q default model %q is not listed for %q upstream", c.Profile, c.DefaultModel, c.Upstream)
		}
	}
}

func TestSupportedEntriesAreComplete(t *testing.T) {
	for _, c := range Compats() {
		if !strings.HasPrefix(c.Profile, ProfilePrefix) {
			t.Errorf("profile %q lacks the %q prefix", c.Profile, ProfilePrefix)
		}
		if c.Website == "" {
			t.Errorf("profile %q has no website", c.Profile)
		}
		if !c.Supported {
			if c.Note == "" {
				t.Errorf("unsupported profile %q must explain why in Note", c.Profile)
			}
			continue
		}
		if !strings.HasPrefix(c.BaseURL, "https://") {
			t.Errorf("profile %q base URL %q is not https", c.Profile, c.BaseURL)
		}
		if c.DefaultModel == "" {
			t.Errorf("supported profile %q has no default model", c.Profile)
		}
	}
}

// TestNoKeyMaterial guards the invariant that compat.go never carries secrets.
func TestNoKeyMaterial(t *testing.T) {
	for _, c := range Compats() {
		for _, field := range []string{c.BaseURL, c.Website, c.Affiliate, c.Note} {
			low := strings.ToLower(field)
			for _, bad := range []string{"api_key", "apikey", "secret", "bearer ", "sk-"} {
				if strings.Contains(low, bad) {
					t.Errorf("profile %q field contains key-shaped text %q", c.Profile, bad)
				}
			}
		}
	}
}

func TestCompatsSortedNoKeyFirst(t *testing.T) {
	cs := Compats()
	// No-key entries must all precede key-required entries.
	seenKeyed := false
	for _, c := range cs {
		if !c.NoKey {
			seenKeyed = true
			continue
		}
		if seenKeyed {
			t.Errorf("no-key profile %q sorts after a key-required profile", c.Profile)
		}
	}
	// Within the no-key group, alphabetical.
	var noKey []string
	for _, c := range cs {
		if c.NoKey {
			noKey = append(noKey, c.Profile)
		}
	}
	if !sort.StringsAreSorted(noKey) {
		t.Errorf("no-key group is not alphabetical: %v", noKey)
	}
}

func TestCompatForHitAndMiss(t *testing.T) {
	if _, ok := CompatFor("free-groq"); !ok {
		t.Error("free-groq not found")
	}
	if _, ok := CompatFor("free-nope"); ok {
		t.Error("CompatFor reported a hit for an unknown profile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestCompat -v`
Expected: FAIL to build, `undefined: Compats`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/compat.go`. Every `Upstream` value must match a `name` in `data.json` exactly, and every `DefaultModel` an `id` in that provider's `models`. If a value below does not match the vendored file (upstream moved since this plan was written), fix the value here to match the file, do not edit the file.

```go
package freellm

import "sort"

// ProfilePrefix is the gophermind profile-name prefix reserved for free
// providers, so they never collide with the built-in profiles.
const ProfilePrefix = "free-"

// Terms records free-tier obligations worth warning a user about before they
// point gophermind at someone else's code.
type Terms struct {
	NonCommercial   bool // free tier forbids commercial use
	TrainsOnPrompts bool // provider may train on free-tier prompts
	IdentityCheck   bool // signup requires real-name or account verification
}

// Compat is what gophermind knows about running against a free provider, as
// distinct from what upstream records about the provider itself. This is the
// only hand-maintained data in the package; data.json is never edited.
type Compat struct {
	Profile      string // gophermind profile name, e.g. "free-groq"
	Upstream     string // provider "name" in data.json; must resolve
	BaseURL      string // OpenAI-compatible endpoint; may differ from upstream baseUrl
	DefaultModel string // explicit; free profiles never auto-discover
	Website      string // human-facing site, shown in attribution
	Affiliate    string // referral URL; empty for every provider today
	NoKey        bool   // serves requests anonymously
	Supported    bool   // false => listed for discovery, not runnable as a profile
	Terms        Terms
	Note         string // why this entry overrides upstream, or is unsupported
}

// compats is the table. Ordering here is irrelevant; Compats sorts.
var compats = []Compat{
	{
		Profile: "free-kilocode", Upstream: "Kilo Code",
		BaseURL: "https://api.kilo.ai/api/gateway",
		DefaultModel: "nvidia/nemotron-3-super-120b-a12b:free",
		Website: "https://kilo.ai", NoKey: true, Supported: true,
	},
	{
		Profile: "free-llm7", Upstream: "LLM7.io",
		BaseURL: "https://api.llm7.io/v1",
		DefaultModel: "gpt-oss:20b",
		Website: "https://llm7.io", NoKey: true, Supported: true,
		Note: "anonymous access needs no key; a free token from token.llm7.io raises the limits",
	},
	{
		Profile: "free-ovhcloud", Upstream: "OVHcloud AI Endpoints",
		BaseURL: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1",
		DefaultModel: "gpt-oss-120b",
		Website: "https://endpoints.ai.cloud.ovh.net", NoKey: true, Supported: true,
		Note: "anonymous tier is 2 requests per minute per IP per model, hosted in the EU",
	},
	{
		Profile: "free-aionlabs", Upstream: "Aion Labs",
		BaseURL: "https://api.aionlabs.ai/v1",
		DefaultModel: "aion-labs/aion-3.0",
		Website: "https://www.aionlabs.ai", Supported: true,
	},
	{
		Profile: "free-cloudflare", Upstream: "Cloudflare Workers AI",
		Website: "https://developers.cloudflare.com/workers-ai/", Supported: false,
		Note: "endpoint embeds an account ID and cannot be known statically; set GOPHERMIND_PROFILE_FREE_CLOUDFLARE_BASE_URL to your own /v1 URL",
	},
	{
		Profile: "free-cohere", Upstream: "Cohere",
		BaseURL: "https://api.cohere.ai/compatibility/v1",
		DefaultModel: "command-a-03-2025",
		Website: "https://cohere.com", Supported: true,
		Terms: Terms{NonCommercial: true},
		Note: "upstream records the native /v2 API; this is Cohere's OpenAI compatibility endpoint",
	},
	{
		Profile: "free-gemini", Upstream: "Google Gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		DefaultModel: "gemini-3.5-flash",
		Website: "https://ai.google.dev", Supported: true,
		Terms: Terms{TrainsOnPrompts: true},
		Note: "upstream records the native v1beta API; this is Google's OpenAI compatibility endpoint",
	},
	{
		Profile: "free-groq", Upstream: "Groq",
		BaseURL: "https://api.groq.com/openai/v1",
		DefaultModel: "openai/gpt-oss-120b",
		Website: "https://groq.com", Supported: true,
		Note: "the /openai/v1 path is correct and matches upstream; it is not a typo",
	},
	{
		Profile: "free-huggingface", Upstream: "Hugging Face",
		BaseURL: "https://router.huggingface.co/v1",
		DefaultModel: "Qwen/Qwen2.5-7B-Instruct",
		Website: "https://huggingface.co", Supported: true,
		Note: "free tier is a monthly credit balance, not a token or request quota",
	},
	{
		Profile: "free-mistral", Upstream: "Mistral AI",
		BaseURL: "https://api.mistral.ai/v1",
		DefaultModel: "mistral-small-2603",
		Website: "https://mistral.ai", Supported: true,
		Terms: Terms{TrainsOnPrompts: true},
	},
	{
		Profile: "free-modelscope", Upstream: "ModelScope",
		BaseURL: "https://api-inference.modelscope.cn/v1",
		DefaultModel: "Qwen/Qwen3.5-35B-A3B",
		Website: "https://modelscope.cn", Supported: true,
		Terms: Terms{IdentityCheck: true},
	},
	{
		Profile: "free-nvidia", Upstream: "NVIDIA NIM",
		BaseURL: "https://integrate.api.nvidia.com/v1",
		DefaultModel: "openai/gpt-oss-120b",
		Website: "https://build.nvidia.com", Supported: true,
	},
	{
		Profile: "free-ollama-cloud", Upstream: "Ollama Cloud",
		BaseURL: "https://ollama.com/v1",
		DefaultModel: "gpt-oss:120b",
		Website: "https://ollama.com", Supported: true,
		Note: "upstream records the native /api endpoint; this is Ollama's OpenAI compatibility endpoint",
	},
	{
		Profile: "free-openrouter", Upstream: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		DefaultModel: "nvidia/nemotron-3-super-120b-a12b:free",
		Website: "https://openrouter.ai", Supported: true,
	},
	{
		Profile: "free-siliconflow", Upstream: "SiliconFlow",
		BaseURL: "https://api.siliconflow.cn/v1",
		DefaultModel: "Qwen/Qwen3-8B",
		Website: "https://siliconflow.cn", Supported: true,
		Terms: Terms{IdentityCheck: true},
	},
	{
		Profile: "free-zai", Upstream: "Z AI (Zhipu AI)",
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		DefaultModel: "glm-4.7-flash",
		Website: "https://z.ai", Supported: true,
	},
}

// Compats returns the compatibility table sorted for display: providers that
// need no API key first (the zero-signup path), then supported providers, then
// unsupported ones, each group alphabetical by profile name.
func Compats() []Compat {
	out := make([]Compat, len(compats))
	copy(out, compats)
	rank := func(c Compat) int {
		switch {
		case c.NoKey && c.Supported:
			return 0
		case c.Supported:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].Profile < out[j].Profile
	})
	return out
}

// CompatFor returns the entry for a gophermind profile name.
func CompatFor(profile string) (Compat, bool) {
	for _, c := range compats {
		if c.Profile == profile {
			return c, true
		}
	}
	return Compat{}, false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS.

If `TestCompatUpstreamResolves` or `TestDefaultModelExistsUpstream` fails, upstream has moved since this plan was written. **Fix the value in `compat.go` to match the vendored `data.json`. Never edit `data.json` to match `compat.go`.**

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): compatibility table with upstream drift guard

Four providers do not speak OpenAI at the baseUrl upstream records, so
compat.go carries the corrected endpoint and says why. Cloudflare is
unsupported: its URL embeds an account ID.

The drift tests are the point: a sync that drops a provider or renames a
model fails here by name instead of 404ing at runtime.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 3: Parse upstream rate limits into quotas

**Files:**
- Create: `internal/freellm/quota.go`
- Test: `internal/freellm/quota_test.go`

**Interfaces:**
- Consumes: `Load`, `Provider`, `Model` from Task 1.
- Produces: `type Unit int` with `UnitRequests`/`UnitTokens`, `type Quota struct{ Unit Unit; Amount int64; Window time.Duration }`, `func (q Quota) String() string`, `func ParseRateLimit(s string) []Quota`, `func QuotasFor(c Compat) []Quota`, `var WindowDay/WindowMonth` helpers.

Upstream `rateLimit` strings observed in the vendored file: `"30 RPM, 1,000 RPD"`, `"15 RPM, 20K TPD"`, `"~1 RPS, 500K TPM"`, `"20 RPM"`, `"200 req/hr"`, `"10 RPM, 60 req/hr (anonymous)"`, `"2 RPM (anonymous)"`, `"1,000 RPM, 50,000 TPM"`, `"10K neurons/day (shared)"`, `"Credit-metered"`, `"Session/weekly limits (unpublished)"`, `"2,000 RPD total; <=500 RPD/model (dynamic)"`, `"Dynamic quotas + dynamic concurrency"`, `"1 concurrent request"`, `"40 RPM, 10,000 RPD"`, `"—"`.

Parse only what is unambiguous: `N RPM`, `N RPD`, `N TPM`, `N TPD`, `N req/hr`. Everything else yields nothing.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/quota_test.go`:

```go
package freellm

import (
	"testing"
	"time"
)

func TestParseRateLimit(t *testing.T) {
	cases := []struct {
		in   string
		want []Quota
	}{
		{"30 RPM, 1,000 RPD", []Quota{
			{UnitRequests, 30, time.Minute},
			{UnitRequests, 1000, 24 * time.Hour},
		}},
		{"15 RPM, 20K TPD", []Quota{
			{UnitRequests, 15, time.Minute},
			{UnitTokens, 20000, 24 * time.Hour},
		}},
		{"1,000 RPM, 50,000 TPM", []Quota{
			{UnitRequests, 1000, time.Minute},
			{UnitTokens, 50000, time.Minute},
		}},
		{"200 req/hr", []Quota{{UnitRequests, 200, time.Hour}}},
		{"10 RPM, 60 req/hr (anonymous)", []Quota{
			{UnitRequests, 10, time.Minute},
			{UnitRequests, 60, time.Hour},
		}},
		{"2 RPM (anonymous)", []Quota{{UnitRequests, 2, time.Minute}}},
		{"~1 RPS, 500K TPM", []Quota{{UnitTokens, 500000, time.Minute}}},
		// Unparseable: no guessing.
		{"10K neurons/day (shared)", nil},
		{"Credit-metered", nil},
		{"Session/weekly limits (unpublished)", nil},
		{"Dynamic quotas + dynamic concurrency", nil},
		{"1 concurrent request", nil},
		{"—", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := ParseRateLimit(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseRateLimit(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseRateLimit(%q)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestEveryUpstreamRateLimitIsAccountedFor fails when a sync introduces a
// rate-limit format nobody has looked at, rather than silently dropping it.
func TestEveryUpstreamRateLimitIsAccountedFor(t *testing.T) {
	// Formats we have deliberately decided not to parse.
	unparseable := map[string]bool{
		"": true, "—": true,
		"10K neurons/day (shared)":                       true,
		"Credit-metered":                                 true,
		"Session/weekly limits (unpublished)":            true,
		"Dynamic quotas + dynamic concurrency":           true,
		"1 concurrent request":                           true,
		"2,000 RPD total; <=500 RPD/model (dynamic)":     true,
	}
	for _, p := range Load().All() {
		for _, m := range p.Models {
			if len(ParseRateLimit(m.RateLimit)) > 0 || unparseable[m.RateLimit] {
				continue
			}
			t.Errorf("provider %q model %q has unrecognized rateLimit %q: parse it in quota.go or add it to the unparseable list",
				p.Name, m.ID, m.RateLimit)
		}
	}
}

func TestQuotaString(t *testing.T) {
	if got := (Quota{UnitRequests, 1000, 24 * time.Hour}).String(); got != "1,000 RPD" {
		t.Errorf("got %q, want %q", got, "1,000 RPD")
	}
	if got := (Quota{UnitTokens, 20000, 24 * time.Hour}).String(); got != "20,000 TPD" {
		t.Errorf("got %q, want %q", got, "20,000 TPD")
	}
}

func TestQuotasForUsesDefaultModel(t *testing.T) {
	c, ok := CompatFor("free-groq")
	if !ok {
		t.Fatal("free-groq missing")
	}
	if len(QuotasFor(c)) == 0 {
		t.Error("expected Groq's default model to yield at least one quota")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestParseRateLimit -v`
Expected: FAIL to build, `undefined: ParseRateLimit`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/quota.go`:

```go
package freellm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Unit is what a quota counts.
type Unit int

const (
	// UnitRequests counts API calls.
	UnitRequests Unit = iota
	// UnitTokens counts prompt plus completion tokens.
	UnitTokens
)

// Quota is one published free-tier limit: an amount of some unit per window.
type Quota struct {
	Unit   Unit
	Amount int64
	Window time.Duration
}

// String renders a quota the way upstream writes it, e.g. "1,000 RPD".
func (q Quota) String() string {
	u := "R"
	if q.Unit == UnitTokens {
		u = "T"
	}
	var w string
	switch q.Window {
	case time.Minute:
		w = "PM"
	case time.Hour:
		w = "P/hr"
	case 24 * time.Hour:
		w = "PD"
	case 30 * 24 * time.Hour:
		w = "PMo"
	default:
		w = "P?"
	}
	return fmt.Sprintf("%s %s%s", commas(q.Amount), u, w)
}

// quotaRe matches the unambiguous forms: "30 RPM", "1,000 RPD", "20K TPD",
// "50,000 TPM", "200 req/hr". Deliberately narrow: anything it does not match
// yields no quota rather than a guess.
var quotaRe = regexp.MustCompile(`(?i)([0-9][0-9,]*)\s*(K?)\s*(RPM|RPD|TPM|TPD|req/hr)`)

// ParseRateLimit extracts every quota it can recognize from an upstream
// rate-limit string, in the order they appear. An unrecognized string yields
// nil: a trip meter with no denominator is honest, a guessed one is not.
//
// "RPS" is deliberately not parsed. Upstream writes it as "~1 RPS", an
// approximation, and a per-second window is not a useful trip meter.
func ParseRateLimit(s string) []Quota {
	var out []Quota
	for _, m := range quotaRe.FindAllStringSubmatch(s, -1) {
		n, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if strings.EqualFold(m[2], "K") {
			n *= 1000
		}
		q := Quota{Amount: n}
		switch strings.ToLower(m[3]) {
		case "rpm":
			q.Unit, q.Window = UnitRequests, time.Minute
		case "rpd":
			q.Unit, q.Window = UnitRequests, 24*time.Hour
		case "tpm":
			q.Unit, q.Window = UnitTokens, time.Minute
		case "tpd":
			q.Unit, q.Window = UnitTokens, 24*time.Hour
		case "req/hr":
			q.Unit, q.Window = UnitRequests, time.Hour
		default:
			continue
		}
		out = append(out, q)
	}
	return out
}

// QuotasFor returns the quotas that apply to a profile's default model, which
// is the model gophermind will actually use. Returns nil for an unsupported
// profile or an unparseable limit.
func QuotasFor(c Compat) []Quota {
	if !c.Supported {
		return nil
	}
	p, ok := Load().Lookup(c.Upstream)
	if !ok {
		return nil
	}
	for _, m := range p.Models {
		if m.ID == c.DefaultModel {
			return ParseRateLimit(m.RateLimit)
		}
	}
	return nil
}

// commas formats an integer with thousands separators.
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS. If `TestEveryUpstreamRateLimitIsAccountedFor` names a format not in the test's `unparseable` map, decide deliberately: either extend `quotaRe` or add the string to the map. Do not silently widen the regex.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): parse upstream rate limits into structured quotas

Parses only the unambiguous forms (RPM, RPD, TPM, TPD, req/hr). Neurons,
credits and 'unpublished' yield no quota, so a trip meter renders a bare
count rather than a guessed denominator.

A table test over every rateLimit string in data.json fails when a sync
introduces a format nobody has looked at.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 4: The odometer

**Files:**
- Create: `internal/freellm/odometer.go`
- Test: `internal/freellm/odometer_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type Event struct{ TS time.Time; Profile string; Tokens, Requests int64 }`, `type ProviderTotal struct{ Tokens, Requests int64; FirstSeen, LastSeen time.Time }`, `type Odometer struct{ Tokens, Requests int64; Per map[string]ProviderTotal; Since time.Time; Events []Event }`, `func DefaultOdometerPath() string`, `func LoadOdometer(path string) (*Odometer, error)`, `func (o *Odometer) Add(path string, e Event) error`, `func (o *Odometer) Reading() (tokens, requests int64)`, `const ringMaxAge = 31 * 24 * time.Hour`, `const ringMaxEvents = 20000`.

The monotonic invariant is the whole point of this task: `Add` only increases, and a truncated or deleted state file never lowers a reading below what is still provable.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/odometer_test.go`:

```go
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
func TestOdometerNeverGoesBackward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	o, _ := LoadOdometer(path)
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: 1000, Requests: 5}); err != nil {
		t.Fatal(err)
	}

	// A negative event must not reduce the reading.
	if err := o.Add(path, Event{TS: time.Now(), Profile: "free-groq", Tokens: -500, Requests: -2}); err != nil {
		t.Fatal(err)
	}
	tok, req := o.Reading()
	if tok != 1000 || req != 5 {
		t.Fatalf("negative event moved the odometer to %d/%d, want 1000/5", tok, req)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestOdometer -v`
Expected: FAIL to build, `undefined: LoadOdometer`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/odometer.go`:

```go
package freellm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// ringMaxAge bounds the event ring at the longest window any provider
	// publishes (Cohere's monthly quota), plus a day of slack.
	ringMaxAge = 31 * 24 * time.Hour
	// ringMaxEvents hard-caps the ring so a heavy user cannot grow the state
	// file without bound.
	ringMaxEvents = 20000
)

// Event is one turn's free usage, recorded for the trip meters.
type Event struct {
	TS       time.Time `json:"ts"`
	Profile  string    `json:"profile"`
	Tokens   int64     `json:"tokens"`
	Requests int64     `json:"requests"`
}

// ProviderTotal is one provider's lifetime contribution to the odometer.
type ProviderTotal struct {
	Tokens    int64     `json:"tokens"`
	Requests  int64     `json:"requests"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Odometer is the lifetime record of free capacity used. Its readings are
// monotonic: they never decrease, whatever happens to the file on disk.
//
// Events is a bounded ring of recent turns, kept so the trip meters can be
// derived without depending on GOPHERMIND_USAGE_LOG, which is off by default.
type Odometer struct {
	Tokens   int64                    `json:"tokens"`
	Requests int64                    `json:"requests"`
	Per      map[string]ProviderTotal `json:"per"`
	Since    time.Time                `json:"since"`
	Events   []Event                  `json:"events"`
}

// DefaultOdometerPath is where the odometer lives: alongside the completion
// cache, under the OS user cache directory.
func DefaultOdometerPath() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "gophermind", "free-odometer.json")
	}
	return filepath.Join(".gophermind", "free-odometer.json")
}

// LoadOdometer reads the odometer at path. A missing file yields a fresh
// odometer. A corrupt or truncated file also yields a usable odometer rather
// than an error: refusing to start because a cache file is damaged would be
// worse than starting from what is left.
func LoadOdometer(path string) (*Odometer, error) {
	o := &Odometer{Per: map[string]ProviderTotal{}, Since: time.Now()}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return o, nil
		}
		return o, nil
	}
	var stored Odometer
	if err := json.Unmarshal(b, &stored); err != nil {
		return o, nil
	}
	if stored.Per == nil {
		stored.Per = map[string]ProviderTotal{}
	}
	if stored.Since.IsZero() {
		stored.Since = time.Now()
	}
	return &stored, nil
}

// Add records one turn and persists the result. It is monotonic by
// construction: non-positive counts are ignored, and no field is ever written
// lower than its stored value.
//
// The read-modify-write is guarded by a lock file so concurrent gophermind
// sessions sharing a cache directory cannot lose an increment.
func (o *Odometer) Add(path string, e Event) error {
	if e.Tokens <= 0 && e.Requests <= 0 {
		return nil
	}
	if e.Tokens < 0 {
		e.Tokens = 0
	}
	if e.Requests < 0 {
		e.Requests = 0
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("freellm: create odometer dir: %w", err)
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	// Re-read under the lock so a concurrent writer's increments are not lost,
	// then merge our in-memory state up (never down).
	onDisk, _ := LoadOdometer(path)
	o.mergeUp(onDisk)
	o.addLocked(e)
	return o.save(path)
}

// mergeUp raises every field of o to at least the corresponding field of other.
// It never lowers anything, which is what keeps the reading monotonic when a
// stale in-memory copy meets a newer file, or the reverse.
func (o *Odometer) mergeUp(other *Odometer) {
	if other == nil {
		return
	}
	if other.Tokens > o.Tokens {
		o.Tokens = other.Tokens
	}
	if other.Requests > o.Requests {
		o.Requests = other.Requests
	}
	if o.Per == nil {
		o.Per = map[string]ProviderTotal{}
	}
	for k, v := range other.Per {
		cur := o.Per[k]
		if v.Tokens > cur.Tokens {
			cur.Tokens = v.Tokens
		}
		if v.Requests > cur.Requests {
			cur.Requests = v.Requests
		}
		if cur.FirstSeen.IsZero() || (!v.FirstSeen.IsZero() && v.FirstSeen.Before(cur.FirstSeen)) {
			cur.FirstSeen = v.FirstSeen
		}
		if v.LastSeen.After(cur.LastSeen) {
			cur.LastSeen = v.LastSeen
		}
		o.Per[k] = cur
	}
	if !other.Since.IsZero() && (o.Since.IsZero() || other.Since.Before(o.Since)) {
		o.Since = other.Since
	}
	// Keep whichever ring has more events. This is deliberately NOT a union:
	// the lifetime totals above are the monotonic guarantee, and the ring only
	// feeds trip meters, which may under-count slightly when two sessions write
	// concurrently. Under-counting a trip meter is safe; over-counting a
	// lifetime odometer would not be.
	if len(other.Events) > len(o.Events) {
		o.Events = other.Events
	}
}

// addLocked applies one event to the in-memory state, including ring
// maintenance. Callers must hold the lock. Exposed to tests for the ring cap.
func (o *Odometer) addLocked(e Event) {
	o.Tokens += e.Tokens
	o.Requests += e.Requests
	if o.Per == nil {
		o.Per = map[string]ProviderTotal{}
	}
	t := o.Per[e.Profile]
	t.Tokens += e.Tokens
	t.Requests += e.Requests
	if t.FirstSeen.IsZero() {
		t.FirstSeen = e.TS
	}
	if e.TS.After(t.LastSeen) {
		t.LastSeen = e.TS
	}
	o.Per[e.Profile] = t

	o.Events = append(o.Events, e)
	o.pruneRing(time.Now())
}

// pruneRing drops events past ringMaxAge and enforces ringMaxEvents. It only
// ever touches Events; the lifetime totals are unaffected, which is why an
// odometer reading survives ring eviction.
func (o *Odometer) pruneRing(now time.Time) {
	cutoff := now.Add(-ringMaxAge)
	kept := o.Events[:0]
	for _, e := range o.Events {
		if e.TS.After(cutoff) {
			kept = append(kept, e)
		}
	}
	o.Events = kept
	if len(o.Events) > ringMaxEvents {
		o.Events = o.Events[len(o.Events)-ringMaxEvents:]
	}
}

// Reading returns the lifetime totals.
func (o *Odometer) Reading() (tokens, requests int64) { return o.Tokens, o.Requests }

// save writes the odometer atomically: a temp file in the same directory, then
// a rename, so a crash mid-write cannot leave a half-written state file.
func (o *Odometer) save(path string) error {
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("freellm: marshal odometer: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".odo-*")
	if err != nil {
		return fmt.Errorf("freellm: create temp odometer: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
```

Create `internal/freellm/lock_unix.go`:

```go
//go:build !windows

package freellm

import (
	"fmt"
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock, blocking until it is available.
// The returned function releases the lock and closes the file.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("freellm: open odometer lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("freellm: lock odometer: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
```

Create `internal/freellm/lock_windows.go`:

```go
//go:build windows

package freellm

import (
	"fmt"
	"os"
	"time"
)

// lockFile emulates an exclusive lock with an atomically created lock file,
// retrying briefly. Windows has no flock; O_EXCL creation is the portable
// equivalent for this use.
func lockFile(path string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return func() {
				_ = f.Close()
				_ = os.Remove(path)
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("freellm: odometer lock busy: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v -race`
Expected: PASS, including `TestOdometerConcurrentAdd` under `-race`.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): monotonic free-usage odometer with event ring

Lifetime tokens and requests across free providers, in a file-locked,
atomically written state file. Readings never decrease: negative events
are ignored and every load merges upward, so a truncated or corrupt file
cannot roll the reading back.

Carries a 31-day event ring so trip meters need no GOPHERMIND_USAGE_LOG.
Ring eviction deliberately does not touch the lifetime totals.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 5: Trip meters

**Files:**
- Create: `internal/freellm/usage.go`
- Test: `internal/freellm/usage_test.go`

**Interfaces:**
- Consumes: `Quota`, `QuotasFor`, `Unit` (Task 3); `Odometer`, `Event` (Task 4); `Compat` (Task 2).
- Produces: `type TripMeter struct{ Quota Quota; Used int64; HasQuota bool }`, `func (t TripMeter) Fraction() float64`, `func (t TripMeter) Warn() bool`, `func (t TripMeter) String() string`, `func TripMeters(o *Odometer, c Compat, now time.Time) []TripMeter`.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/usage_test.go`:

```go
package freellm

import (
	"strings"
	"testing"
	"time"
)

func newOdo(events ...Event) *Odometer {
	o := &Odometer{Per: map[string]ProviderTotal{}}
	for _, e := range events {
		o.Events = append(o.Events, e)
	}
	return o
}

func TestTripMetersCountOnlyInsideWindow(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	c, ok := CompatFor("free-groq") // 30 RPM, 1,000 RPD
	if !ok {
		t.Fatal("free-groq missing")
	}
	o := newOdo(
		Event{TS: now.Add(-30 * time.Second), Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now.Add(-2 * time.Minute), Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now.Add(-48 * time.Hour), Profile: "free-groq", Tokens: 10, Requests: 1},
	)
	ms := TripMeters(o, c, now)
	if len(ms) == 0 {
		t.Fatal("expected trip meters for free-groq")
	}
	var perMinute, perDay *TripMeter
	for i := range ms {
		switch ms[i].Quota.Window {
		case time.Minute:
			perMinute = &ms[i]
		case 24 * time.Hour:
			perDay = &ms[i]
		}
	}
	if perMinute == nil || perMinute.Used != 1 {
		t.Errorf("per-minute meter used = %v, want 1", perMinute)
	}
	if perDay == nil || perDay.Used != 2 {
		t.Errorf("per-day meter used = %v, want 2 (the 48h-old event is outside)", perDay)
	}
}

func TestTripMetersIgnoreOtherProfiles(t *testing.T) {
	now := time.Now()
	c, _ := CompatFor("free-groq")
	o := newOdo(
		Event{TS: now, Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now, Profile: "free-openrouter", Tokens: 10, Requests: 5},
	)
	for _, m := range TripMeters(o, c, now) {
		if m.Quota.Unit == UnitRequests && m.Used != 1 {
			t.Errorf("meter counted another profile's usage: used = %d, want 1", m.Used)
		}
	}
}

func TestTripMeterWarnsAtEightyPercent(t *testing.T) {
	m := TripMeter{Quota: Quota{UnitRequests, 100, 24 * time.Hour}, Used: 79, HasQuota: true}
	if m.Warn() {
		t.Error("warned at 79%")
	}
	m.Used = 80
	if !m.Warn() {
		t.Error("did not warn at 80%")
	}
}

func TestTripMeterStringWithAndWithoutQuota(t *testing.T) {
	with := TripMeter{Quota: Quota{UnitRequests, 1000, 24 * time.Hour}, Used: 312, HasQuota: true}
	if got := with.String(); got != "312/1,000 RPD" {
		t.Errorf("got %q, want %q", got, "312/1,000 RPD")
	}
	without := TripMeter{Used: 42}
	if got := without.String(); !strings.Contains(got, "42") || strings.Contains(got, "/") {
		t.Errorf("quota-less meter rendered %q; want a bare count with no denominator", got)
	}
}

// A provider whose limit does not parse must still report a count, never a
// guessed denominator.
func TestTripMetersForUnparseableQuota(t *testing.T) {
	now := time.Now()
	c, ok := CompatFor("free-ollama-cloud") // "Session/weekly limits (unpublished)"
	if !ok {
		t.Fatal("free-ollama-cloud missing")
	}
	o := newOdo(Event{TS: now, Profile: "free-ollama-cloud", Tokens: 500, Requests: 3})
	ms := TripMeters(o, c, now)
	if len(ms) != 1 {
		t.Fatalf("got %d meters, want 1 fallback meter", len(ms))
	}
	if ms[0].HasQuota {
		t.Error("fallback meter claims to have a quota")
	}
	if ms[0].Used != 3 {
		t.Errorf("fallback meter used = %d, want 3 requests", ms[0].Used)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestTrip -v`
Expected: FAIL to build, `undefined: TripMeters`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/usage.go`:

```go
package freellm

import (
	"fmt"
	"time"
)

// warnFraction is the point at which a trip meter starts warning. gophermind
// never blocks a request on this: the local count can drift from the
// provider's when another client shares the key, and refusing a request that
// would have succeeded is worse than a 429.
const warnFraction = 0.8

// TripMeter is consumption inside one quota window. HasQuota is false when
// upstream's limit did not parse, in which case Used is still meaningful but
// there is no denominator to show.
type TripMeter struct {
	Quota    Quota
	Used     int64
	HasQuota bool
}

// Fraction is consumption as a share of the quota, or 0 when there is none.
func (t TripMeter) Fraction() float64 {
	if !t.HasQuota || t.Quota.Amount <= 0 {
		return 0
	}
	return float64(t.Used) / float64(t.Quota.Amount)
}

// Warn reports whether this meter has reached the warning threshold.
func (t TripMeter) Warn() bool { return t.HasQuota && t.Fraction() >= warnFraction }

// String renders "312/1,000 RPD" with a quota, or a bare "42 requests today"
// without one. It never invents a denominator.
func (t TripMeter) String() string {
	if t.HasQuota {
		return fmt.Sprintf("%s/%s", commas(t.Used), t.Quota.String())
	}
	return fmt.Sprintf("%s requests (no published limit)", commas(t.Used))
}

// TripMeters returns one meter per published quota for the profile, counting
// only that profile's events inside each window.
//
// When no quota parses, it returns a single quota-less meter carrying the
// day's request count, so the user still sees activity.
func TripMeters(o *Odometer, c Compat, now time.Time) []TripMeter {
	quotas := QuotasFor(c)
	if len(quotas) == 0 {
		return []TripMeter{{Used: sumWindow(o, c.Profile, UnitRequests, now, 24*time.Hour)}}
	}
	out := make([]TripMeter, 0, len(quotas))
	for _, q := range quotas {
		out = append(out, TripMeter{
			Quota:    q,
			Used:     sumWindow(o, c.Profile, q.Unit, now, q.Window),
			HasQuota: true,
		})
	}
	return out
}

// sumWindow totals one profile's usage of one unit within the window ending at
// now.
func sumWindow(o *Odometer, profile string, unit Unit, now time.Time, window time.Duration) int64 {
	if o == nil {
		return 0
	}
	cutoff := now.Add(-window)
	var total int64
	for _, e := range o.Events {
		if e.Profile != profile || !e.TS.After(cutoff) || e.TS.After(now) {
			continue
		}
		if unit == UnitTokens {
			total += e.Tokens
		} else {
			total += e.Requests
		}
	}
	return total
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): trip meters derived from the odometer event ring

One meter per published quota, counting only that profile's events inside
each window. A provider whose limit does not parse gets a bare count with
no denominator rather than a guessed one.

Warns at 80 percent but never blocks: the local count can drift from the
provider's when a key is shared, and refusing a request that would have
succeeded is worse than a 429.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 6: Attribution and the referral-disclosure invariant

**Files:**
- Create: `internal/freellm/attribution.go`
- Test: `internal/freellm/attribution_test.go`

**Interfaces:**
- Consumes: `Compat`, `CompatFor` (Task 2).
- Produces: `type Attribution struct{ Model, Provider, Link string; IsReferral, Free bool }`, `func AttributionFor(profile, model string) (Attribution, bool)`, `func (a Attribution) Line() string`, `func (a Attribution) Short() string`, `const referralMarker = "(referral link)"`, `const NoAffiliateEnv = "GOPHERMIND_NO_AFFILIATE"`.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/attribution_test.go`:

```go
package freellm

import (
	"strings"
	"testing"
)

func TestAttributionLine(t *testing.T) {
	a, ok := AttributionFor("free-groq", "openai/gpt-oss-120b")
	if !ok {
		t.Fatal("free-groq has no attribution")
	}
	line := a.Line()
	for _, want := range []string{"openai/gpt-oss-120b", "Groq", "https://groq.com", "free tier"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, referralMarker) {
		t.Errorf("line %q claims a referral link where none is configured", line)
	}
}

func TestAttributionMissForPaidProfile(t *testing.T) {
	if _, ok := AttributionFor("openai", "gpt-4o-mini"); ok {
		t.Error("a built-in paid profile must not produce free attribution")
	}
	if _, ok := AttributionFor("", "some-model"); ok {
		t.Error("an empty profile must not produce free attribution")
	}
}

// TestReferralAlwaysDisclosed is the invariant that makes an affiliate link
// safe to add later: there is no code path that emits one unmarked.
func TestReferralAlwaysDisclosed(t *testing.T) {
	for _, c := range Compats() {
		if c.Affiliate == "" {
			continue
		}
		a, ok := AttributionFor(c.Profile, c.DefaultModel)
		if !ok {
			t.Errorf("profile %q has an affiliate link but no attribution", c.Profile)
			continue
		}
		for name, out := range map[string]string{"Line": a.Line(), "Short": a.Short()} {
			if !strings.Contains(out, referralMarker) {
				t.Errorf("profile %q %s() = %q, which emits a referral link without %q",
					c.Profile, name, out, referralMarker)
			}
		}
	}
}

// The same invariant, proven against a synthetic entry, so it holds even while
// every shipped Affiliate is empty.
func TestReferralDisclosureWithSyntheticEntry(t *testing.T) {
	a := Attribution{
		Model: "m", Provider: "P",
		Link: "https://example.test/ref/123", IsReferral: true, Free: true,
	}
	for name, out := range map[string]string{"Line": a.Line(), "Short": a.Short()} {
		if !strings.Contains(out, referralMarker) {
			t.Errorf("%s() = %q, missing %q", name, out, referralMarker)
		}
	}
}

func TestNoAffiliateEnvForcesWebsite(t *testing.T) {
	t.Setenv(NoAffiliateEnv, "1")
	a := attributionFrom(Compat{
		Profile: "free-x", Upstream: "X", Website: "https://x.test",
		Affiliate: "https://x.test/ref/1", Supported: true,
	}, "m")
	if a.Link != "https://x.test" {
		t.Errorf("link = %q, want the plain website with %s set", a.Link, NoAffiliateEnv)
	}
	if a.IsReferral {
		t.Error("IsReferral is true with the opt-out set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestAttribution -v`
Expected: FAIL to build, `undefined: AttributionFor`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/attribution.go`:

```go
package freellm

import (
	"fmt"
	"os"
	"strings"
)

// referralMarker is appended to every rendering of a referral link. The
// invariant it encodes: no code path emits an affiliate URL without saying so.
const referralMarker = "(referral link)"

// NoAffiliateEnv, when set to a non-empty value, forces the plain provider
// website even where a referral link is configured.
const NoAffiliateEnv = "GOPHERMIND_NO_AFFILIATE"

// Attribution names the provider serving the current model, and where to find
// them. Link is the plain website unless IsReferral is true.
type Attribution struct {
	Model      string
	Provider   string
	Link       string
	IsReferral bool
	Free       bool
}

// AttributionFor returns attribution for a free profile. The second result is
// false for a paid or unknown profile, which has no free attribution to show.
func AttributionFor(profile, model string) (Attribution, bool) {
	c, ok := CompatFor(profile)
	if !ok {
		return Attribution{}, false
	}
	return attributionFrom(c, model), true
}

// attributionFrom builds the attribution, applying the affiliate opt-out. Split
// out so tests can exercise a synthetic entry while every shipped Affiliate is
// empty.
func attributionFrom(c Compat, model string) Attribution {
	if model == "" {
		model = c.DefaultModel
	}
	a := Attribution{Model: model, Provider: c.Upstream, Link: c.Website, Free: true}
	if c.Affiliate != "" && os.Getenv(NoAffiliateEnv) == "" {
		a.Link = c.Affiliate
		a.IsReferral = true
	}
	return a
}

// Line is the full one-line form, for the startup banner:
//
//	openai/gpt-oss-120b via Groq (free tier) https://groq.com
func (a Attribution) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s via %s", a.Model, a.Provider)
	if a.Free {
		b.WriteString(" (free tier)")
	}
	if a.Link != "" {
		fmt.Fprintf(&b, " %s", a.Link)
		if a.IsReferral {
			fmt.Fprintf(&b, " %s", referralMarker)
		}
	}
	return b.String()
}

// Short is the compact form, for the status line: the provider name only, with
// the referral marker when one applies.
func (a Attribution) Short() string {
	if a.IsReferral {
		return fmt.Sprintf("%s %s", a.Provider, referralMarker)
	}
	return a.Provider
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): provider attribution with enforced referral disclosure

Every rendering of an affiliate URL carries a (referral link) marker, and
a test asserts it for every entry plus a synthetic one, so the invariant
holds while all shipped Affiliate fields are still empty.

GOPHERMIND_NO_AFFILIATE forces the plain website.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 7: Endpoint probe

**Files:**
- Create: `internal/freellm/check.go`
- Test: `internal/freellm/check_test.go`

**Interfaces:**
- Consumes: `Compat` (Task 2).
- Produces: `func Check(ctx context.Context, baseURL, apiKey string, hc *http.Client) ([]string, error)`.

This is the only file in the package that makes a network call. No test may reach the real network; all use `httptest`.

- [ ] **Step 1: Write the failing test**

Create `internal/freellm/check_test.go`:

```go
package freellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckReturnsModelIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("requested %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k123" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-a"},{"id":"m-b"}]}`))
	}))
	defer srv.Close()

	ids, err := Check(context.Background(), srv.URL, "k123", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "m-a" || ids[1] != "m-b" {
		t.Fatalf("ids = %v, want [m-a m-b]", ids)
	}
}

func TestCheckOmitsAuthWhenNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("sent an Authorization header with no key: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if _, err := Check(context.Background(), srv.URL, "", srv.Client()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	_, err := Check(context.Background(), srv.URL, "secret-key-value", srv.Client())
	if err == nil {
		t.Fatal("expected an error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not mention the status", err)
	}
	if strings.Contains(err.Error(), "secret-key-value") {
		t.Errorf("error leaked the API key: %q", err)
	}
}

func TestCheckSurfacesMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := Check(context.Background(), srv.URL, "", srv.Client()); err == nil {
		t.Fatal("expected an error for a malformed body")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/freellm/ -run TestCheck -v`
Expected: FAIL to build, `undefined: Check`.

- [ ] **Step 3: Write the implementation**

Create `internal/freellm/check.go`:

```go
package freellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// checkTimeout bounds a probe so a hung endpoint cannot stall the CLI.
const checkTimeout = 15 * time.Second

// Check probes an OpenAI-compatible endpoint by listing its models, returning
// the model IDs it advertises. It is how a user proves an endpoint works before
// trusting it, and how the sync script proves compat.go is still accurate.
//
// The API key is sent as a bearer token when non-empty and never appears in a
// returned error. hc may be nil, in which case a bounded default client is used.
func Check(ctx context.Context, baseURL, apiKey string, hc *http.Client) ([]string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: checkTimeout}
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is the provider's, not ours, so it cannot contain our key.
		return nil, fmt.Errorf("GET %s returned %d %s: %s",
			url, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
	}

	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("GET %s returned a body that is not an OpenAI model list: %w", url, err)
	}
	ids := make([]string, 0, len(doc.Data))
	for _, m := range doc.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/freellm/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/freellm/
git commit -m "feat(freellm): probe an endpoint's model list

Check GETs {base}/models so compatibility is proven rather than claimed.
The API key is sent as a bearer token and never appears in a returned
error; a test asserts that against a 401 body.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 8: Wire free profiles into config

**Files:**
- Modify: `internal/config/config.go` (in `ApplyProfile`, and add `FreeProfileNames`)
- Test: `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: `CompatFor`, `Compat`, `ProfilePrefix` (Task 2).
- Produces: `func FreeProfileNames() [][2]string` returning `{profile, baseURL}` pairs in `Compats()` order, supported entries only.

Read `ApplyProfile` at `internal/config/config.go:427` before editing. The branch goes after `builtin, isBuiltin := builtinProfiles[c.Profile]` and `envBase := os.Getenv(prefix + "_BASE_URL")`, replacing the `if !isBuiltin && envBase == ""` error.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestApplyFreeProfile(t *testing.T) {
	c := Config{Profile: "free-groq"}
	got, err := c.ApplyProfile()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("BaseURL = %q", got.BaseURL)
	}
	if got.Model != "openai/gpt-oss-120b" {
		t.Errorf("Model = %q, want the explicit default (never auto-discovery)", got.Model)
	}
	if got.APIKey != "" {
		t.Errorf("APIKey = %q, want empty with no env var set", got.APIKey)
	}
}

func TestFreeProfileEnvOverridesWin(t *testing.T) {
	t.Setenv("GOPHERMIND_PROFILE_FREE_GROQ_BASE_URL", "https://proxy.internal/v1")
	t.Setenv("GOPHERMIND_PROFILE_FREE_GROQ_MODEL", "my-model")
	t.Setenv("GOPHERMIND_PROFILE_FREE_GROQ_API_KEY", "k")
	got, err := Config{Profile: "free-groq"}.ApplyProfile()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://proxy.internal/v1" || got.Model != "my-model" || got.APIKey != "k" {
		t.Errorf("env overrides did not win: %+v", got)
	}
}

func TestUnsupportedFreeProfileErrorsWithNote(t *testing.T) {
	_, err := Config{Profile: "free-cloudflare"}.ApplyProfile()
	if err == nil {
		t.Fatal("expected an error for an unsupported free profile")
	}
	if !strings.Contains(err.Error(), "free-cloudflare") {
		t.Errorf("error %q does not name the profile", err)
	}
	if !strings.Contains(err.Error(), "GOPHERMIND_PROFILE_FREE_CLOUDFLARE_BASE_URL") {
		t.Errorf("error %q does not point at the override", err)
	}
}

func TestUnknownFreeProfileStillErrors(t *testing.T) {
	if _, err := (Config{Profile: "free-nope"}).ApplyProfile(); err == nil {
		t.Error("expected an error for an unknown free profile")
	}
}

func TestFreeProfileNamesAreSupportedOnly(t *testing.T) {
	names := FreeProfileNames()
	if len(names) == 0 {
		t.Fatal("no free profiles listed")
	}
	for _, p := range names {
		if p[0] == "free-cloudflare" {
			t.Error("an unsupported profile appears in FreeProfileNames")
		}
		if p[1] == "" {
			t.Errorf("profile %q has no base URL", p[0])
		}
	}
}

// Built-in profiles must be unaffected.
func TestBuiltinProfilesUnchangedByFreeSupport(t *testing.T) {
	got, err := Config{Profile: "openai"}.ApplyProfile()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://api.openai.com/v1" || got.Model != "gpt-4o-mini" {
		t.Errorf("built-in openai profile changed: %+v", got)
	}
}
```

Ensure `strings` and the `gophermind/internal/freellm` import are present in the test file as needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestApplyFree|TestFreeProfile|TestUnsupportedFree|TestUnknownFree' -v`
Expected: FAIL, either a build error on `FreeProfileNames` or `unknown profile "free-groq"`.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add the import `"gophermind/internal/freellm"`.

Replace this block inside `ApplyProfile`:

```go
	// A custom profile is recognized only if it defines at least a base URL
	// via env. Otherwise the name is unknown and we fail loudly.
	envBase := os.Getenv(prefix + "_BASE_URL")
	if !isBuiltin && envBase == "" {
		return Config{}, fmt.Errorf("unknown profile %q: no built-in profile and %s_BASE_URL is not set", c.Profile, prefix)
	}

	c.BaseURL = firstNonEmpty(envBase, builtin.BaseURL)
	c.Model = firstNonEmpty(os.Getenv(prefix+"_MODEL"), builtin.Model)
```

with:

```go
	// A custom profile is recognized if it defines at least a base URL via env,
	// or if it names a free provider from the vendored registry. Otherwise the
	// name is unknown and we fail loudly.
	envBase := os.Getenv(prefix + "_BASE_URL")
	free, isFree := freellm.CompatFor(c.Profile)
	if isFree && !free.Supported && envBase == "" {
		return Config{}, fmt.Errorf("profile %q is not runnable as-is: %s (set %s_BASE_URL)", c.Profile, free.Note, prefix)
	}
	if !isBuiltin && !isFree && envBase == "" {
		return Config{}, fmt.Errorf("unknown profile %q: no built-in profile and %s_BASE_URL is not set", c.Profile, prefix)
	}

	// Free profiles always carry an explicit model: an empty Model triggers
	// auto-discovery from /v1/models, and several free endpoints list paid
	// models alongside free ones, so discovery could select a billable model.
	freeBase, freeModel := "", ""
	if isFree {
		freeBase, freeModel = free.BaseURL, free.DefaultModel
	}

	c.BaseURL = firstNonEmpty(envBase, builtin.BaseURL, freeBase)
	c.Model = firstNonEmpty(os.Getenv(prefix+"_MODEL"), builtin.Model, freeModel)
```

Then add, next to `BuiltinProfileNames`:

```go
// FreeProfileNames returns the runnable free-provider profiles as
// {name, baseURL} pairs, in display order (no-key providers first). Kept
// separate from BuiltinProfileNames so sixteen free entries never bury the
// three built-in ones in the setup wizard's menu.
func FreeProfileNames() [][2]string {
	cs := freellm.Compats()
	pairs := make([][2]string, 0, len(cs))
	for _, c := range cs {
		if !c.Supported {
			continue
		}
		pairs = append(pairs, [2]string{c.Profile, c.BaseURL})
	}
	return pairs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ ./internal/freellm/ -v`
Expected: PASS, including the pre-existing config tests.

- [ ] **Step 5: Verify the whole repo still builds and passes**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 6: Optional but recommended, verify the endpoints for real**

This is the step the spec's provisional `Supported: true` depends on. It needs
network access and, for the keyed providers, keys. At minimum run the three
no-key providers:

```bash
go run ./cmd/gophermind free check free-ovhcloud
go run ./cmd/gophermind free check free-llm7
go run ./cmd/gophermind free check free-kilocode
```

(Task 9 adds this command; run this step after Task 9 if executing in order.)
For any that fail with a 404 or a non-OpenAI body, set `Supported: false` in
`compat.go` with a `Note` recording the observed status, and commit that
separately. **Record what you actually observed. Do not mark an entry verified
that you did not probe.**

- [ ] **Step 7: Commit**

```bash
git add internal/config/
git commit -m "feat(config): resolve free-* profiles from the vendored registry

ApplyProfile falls back to freellm for a name that is neither built in
nor backed by env vars, so --profile free-groq works with no setup. Env
overrides still win per field and API keys are still read only from
GOPHERMIND_PROFILE_<NAME>_API_KEY, never from the registry.

Free profiles always carry an explicit model. An empty Model triggers
auto-discovery, and several free endpoints list paid models alongside
free ones, so discovery could select a billable model.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 9: The `gophermind free` subcommand

**Files:**
- Create: `cmd/gophermind/free.go`
- Modify: `cmd/gophermind/main.go` (add a `case "free":` to the subcommand switch, near `case "secaudit":` at line 952)
- Test: `cmd/gophermind/free_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2-7, plus `config.Config`.
- Produces: `func runFree(args []string, out io.Writer) int`.

`runFree` takes an `io.Writer` and returns an exit code so it is testable without a subprocess. `main.go` calls `os.Exit(runFree(os.Args[2:], os.Stdout))`.

- [ ] **Step 1: Write the failing test**

Create `cmd/gophermind/free_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFreeListShowsNoKeyProvidersFirst(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"list"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{"free-ovhcloud", "free-groq", "no key"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	// A no-key provider must appear before a key-required one.
	if strings.Index(out, "free-ovhcloud") > strings.Index(out, "free-groq") {
		t.Error("key-required provider listed before a no-key one")
	}
}

func TestFreeListMarksTerms(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"list"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "non-commercial") {
		t.Errorf("list does not flag Cohere's non-commercial terms:\n%s", out)
	}
}

func TestFreeShowPrintsExportLines(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-groq"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{
		"https://groq.com",
		"openai/gpt-oss-120b",
		"GOPHERMIND_PROFILE_FREE_GROQ_API_KEY",
		"--profile free-groq",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestFreeShowNoKeyProviderOmitsKeyLine(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"show", "free-ovhcloud"}, &buf)
	if strings.Contains(buf.String(), "API_KEY") {
		t.Errorf("show told the user to set a key for a no-key provider:\n%s", buf.String())
	}
}

func TestFreeShowUnknownProfileErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-nope"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown profile")
	}
}

func TestFreeUsageRunsWithNoOdometer(t *testing.T) {
	t.Setenv("GOPHERMIND_ODOMETER", t.TempDir()+"/odo.json")
	var buf bytes.Buffer
	if code := runFree([]string{"usage"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(buf.String(), "0") {
		t.Errorf("usage output does not show a zero reading:\n%s", buf.String())
	}
}

func TestFreeUnknownSubcommandErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"wat"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown subcommand")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/gophermind/ -run TestFree -v`
Expected: FAIL to build, `undefined: runFree`.

- [ ] **Step 3: Write the implementation**

Create `cmd/gophermind/free.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"gophermind/internal/freellm"
)

// odometerPath resolves where the free-usage odometer lives, honoring
// GOPHERMIND_ODOMETER so tests and alternate installs can redirect it.
func odometerPath() string {
	if p := strings.TrimSpace(os.Getenv("GOPHERMIND_ODOMETER")); p != "" {
		return p
	}
	return freellm.DefaultOdometerPath()
}

// runFree implements "gophermind free". It writes to out and returns a process
// exit code, so it is testable without spawning a subprocess.
func runFree(args []string, out io.Writer) int {
	if len(args) == 0 {
		return freeUsageText(out)
	}
	switch args[0] {
	case "list":
		return freeList(out)
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(out, "usage: gophermind free show <profile>")
			return 2
		}
		return freeShow(out, args[1])
	case "check":
		if len(args) < 2 {
			fmt.Fprintln(out, "usage: gophermind free check <profile>")
			return 2
		}
		return freeCheck(out, args[1])
	case "usage":
		return freeUsage(out)
	default:
		fmt.Fprintf(out, "unknown subcommand %q\n", args[0])
		return freeUsageText(out)
	}
}

func freeUsageText(out io.Writer) int {
	fmt.Fprintln(out, "usage: gophermind free <list|show|check|usage>")
	fmt.Fprintln(out, "  list           every free provider, no-key ones first")
	fmt.Fprintln(out, "  show <profile> full details and the exact env to set")
	fmt.Fprintln(out, "  check <profile> probe the endpoint's model list")
	fmt.Fprintln(out, "  usage          the free-usage odometer and trip meters")
	return 2
}

// termsFlags renders the free-tier obligations worth warning about.
func termsFlags(t freellm.Terms) string {
	var f []string
	if t.NonCommercial {
		f = append(f, "non-commercial")
	}
	if t.TrainsOnPrompts {
		f = append(f, "trains on prompts")
	}
	if t.IdentityCheck {
		f = append(f, "identity check")
	}
	return strings.Join(f, ", ")
}

func freeList(out io.Writer) int {
	r := freellm.Load()
	fmt.Fprintf(out, "Free LLM providers (registry vendored %s from github.com/mnfst/awesome-free-llm-apis)\n\n", r.LastUpdated())
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tPROVIDER\tKEY\tDEFAULT MODEL\tTERMS\tLINK")
	for _, c := range freellm.Compats() {
		key := "required"
		if c.NoKey {
			key = "no key"
		}
		model := c.DefaultModel
		if !c.Supported {
			key, model = "-", "not runnable"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Profile, c.Upstream, key, model, termsFlags(c.Terms), c.Website)
	}
	tw.Flush()
	fmt.Fprintln(out, "\nRun one with:  gophermind --profile <profile> ask \"hello\"")
	fmt.Fprintln(out, "Details with:  gophermind free show <profile>")
	return 0
}

func freeShow(out io.Writer, profile string) int {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		fmt.Fprintf(out, "unknown free profile %q; run: gophermind free list\n", profile)
		return 1
	}
	p, hasUpstream := freellm.Load().Lookup(c.Upstream)

	fmt.Fprintf(out, "%s  (%s)\n", c.Upstream, c.Profile)
	fmt.Fprintf(out, "Website:  %s\n", c.Website)
	if hasUpstream {
		fmt.Fprintf(out, "Signup:   %s\n", p.URL)
		fmt.Fprintf(out, "About:    %s\n", p.Description)
	}
	if !c.Supported {
		fmt.Fprintf(out, "\nNot runnable as a profile: %s\n", c.Note)
		return 0
	}
	fmt.Fprintf(out, "Endpoint: %s\n", c.BaseURL)
	fmt.Fprintf(out, "Default:  %s\n", c.DefaultModel)
	if f := termsFlags(c.Terms); f != "" {
		fmt.Fprintf(out, "Terms:    %s\n", f)
	}
	if c.Note != "" {
		fmt.Fprintf(out, "Note:     %s\n", c.Note)
	}
	if c.Affiliate != "" {
		fmt.Fprintf(out, "Link:     %s (referral link)\n", c.Affiliate)
	}

	if hasUpstream {
		fmt.Fprintln(out, "\nModels:")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  ID\tCONTEXT\tMAX OUT\tMODALITY\tRATE LIMIT")
		for _, m := range p.Models {
			if m.ID == "" {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", m.ID, m.Context, m.MaxOutput, m.Modality, m.RateLimit)
		}
		tw.Flush()
	}

	fmt.Fprintln(out, "\nTo use it:")
	if !c.NoKey {
		fmt.Fprintf(out, "  export %s='<your key>'\n", apiKeyEnvFor(c.Profile))
	}
	fmt.Fprintf(out, "  gophermind --profile %s ask \"hello\"\n", c.Profile)
	return 0
}

// apiKeyEnvFor mirrors config.profileEnvKey: the per-profile env var prefix.
func apiKeyEnvFor(profile string) string {
	up := strings.ToUpper(profile)
	up = strings.ReplaceAll(up, "-", "_")
	return "GOPHERMIND_PROFILE_" + up + "_API_KEY"
}

func freeCheck(out io.Writer, profile string) int {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		fmt.Fprintf(out, "unknown free profile %q; run: gophermind free list\n", profile)
		return 1
	}
	if !c.Supported {
		fmt.Fprintf(out, "%s is not runnable as a profile: %s\n", c.Profile, c.Note)
		return 1
	}
	key := os.Getenv(apiKeyEnvFor(c.Profile))
	if key == "" && !c.NoKey {
		fmt.Fprintf(out, "%s needs a key. Set %s, then run this again.\n", c.Profile, apiKeyEnvFor(c.Profile))
		return 1
	}
	fmt.Fprintf(out, "Probing %s ...\n", c.BaseURL)
	ids, err := freellm.Check(context.Background(), c.BaseURL, key, nil)
	if err != nil {
		fmt.Fprintf(out, "FAILED: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "OK: %d models advertised\n", len(ids))
	for i, id := range ids {
		if i >= 20 {
			fmt.Fprintf(out, "  ... and %d more\n", len(ids)-20)
			break
		}
		fmt.Fprintf(out, "  %s\n", id)
	}
	var found bool
	for _, id := range ids {
		if id == c.DefaultModel {
			found = true
			break
		}
	}
	if !found && len(ids) > 0 {
		fmt.Fprintf(out, "\nWarning: the default model %q is not in this list.\n", c.DefaultModel)
	}
	return 0
}

func freeUsage(out io.Writer) int {
	path := odometerPath()
	o, err := freellm.LoadOdometer(path)
	if err != nil {
		fmt.Fprintf(out, "could not read the odometer at %s: %v\n", path, err)
		return 1
	}
	tokens, requests := o.Reading()
	fmt.Fprintln(out, "Free usage odometer")
	fmt.Fprintf(out, "  %s tokens over %s requests\n", commaInt(tokens), commaInt(requests))
	if !o.Since.IsZero() {
		fmt.Fprintf(out, "  since %s\n", o.Since.Format("2006-01-02"))
	}

	if len(o.Per) > 0 {
		fmt.Fprintln(out, "\nBy provider:")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  PROFILE\tTOKENS\tREQUESTS\tLAST USED")
		for _, c := range freellm.Compats() {
			t, ok := o.Per[c.Profile]
			if !ok {
				continue
			}
			last := "-"
			if !t.LastSeen.IsZero() {
				last = t.LastSeen.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", c.Profile, commaInt(t.Tokens), commaInt(t.Requests), last)
		}
		tw.Flush()

		fmt.Fprintln(out, "\nCurrent quota windows:")
		now := time.Now()
		for _, c := range freellm.Compats() {
			if _, used := o.Per[c.Profile]; !used || !c.Supported {
				continue
			}
			var parts []string
			for _, m := range freellm.TripMeters(o, c, now) {
				s := m.String()
				if m.Warn() {
					s += "  <- near the limit"
				}
				parts = append(parts, s)
			}
			fmt.Fprintf(out, "  %-20s %s\n", c.Profile, strings.Join(parts, "   "))
		}
	}
	return 0
}

// commaInt formats an int64 with thousands separators.
func commaInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
```

In `cmd/gophermind/main.go`, add to the subcommand switch alongside `case "secaudit":` (line 952):

```go
	case "free":
		os.Exit(runFree(os.Args[2:], os.Stdout))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/gophermind/ -run TestFree -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 5: See it work**

Run: `go run ./cmd/gophermind free list`
Expected: a table with `free-kilocode`, `free-llm7`, `free-ovhcloud` at the top marked `no key`.

Run: `go run ./cmd/gophermind free show free-groq`
Expected: Groq's card, its model table, and the two lines needed to run it.

- [ ] **Step 6: Commit**

```bash
git add cmd/gophermind/
git commit -m "feat(cli): add gophermind free (list, show, check, usage)

list puts the three no-key providers first, since those are the actual
zero-signup path. show prints the exact env to set, and omits the key
line entirely for providers that need none. check probes /models so
compatibility is proven rather than claimed.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 10: Record usage into the odometer

**Files:**
- Modify: `internal/usagelog/usagelog.go` (two fields on `Record`)
- Modify: `cmd/gophermind/main.go:1312-1318` (the usage-append block)
- Test: `internal/usagelog/usagelog_test.go` (append)

**Interfaces:**
- Consumes: `Odometer.Add`, `Event` (Task 4); `CompatFor` (Task 2); `odometerPath` (Task 9).
- Produces: `usagelog.Record.Profile`, `usagelog.Record.Provider`.

- [ ] **Step 1: Write the failing test**

Append to `internal/usagelog/usagelog_test.go`:

```go
func TestRecordCarriesProfileAndProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r := Record{
		Time: time.Now(), Model: "openai/gpt-oss-120b",
		PromptTokens: 10, CompletionTokens: 5,
		Profile: "free-groq", Provider: "Groq",
	}
	if err := Append(path, r); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records", len(recs))
	}
	if recs[0].Profile != "free-groq" || recs[0].Provider != "Groq" {
		t.Errorf("profile/provider not round-tripped: %+v", recs[0])
	}
}

// A line written before these fields existed must still parse, and must read
// as paid rather than as a free record with an empty provider.
func TestOldRecordsStillParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old := `{"time":"2026-07-10T09:00:00Z","model":"m1","prompt_tokens":100,"completion_tokens":50,"cost_usd":0.01}`
	if err := os.WriteFile(path, []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Model != "m1" {
		t.Fatalf("old record did not parse: %+v", recs)
	}
	if recs[0].Profile != "" {
		t.Errorf("old record gained a profile: %q", recs[0].Profile)
	}
}
```

Add `"os"` to that file's imports if it is not already there.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/usagelog/ -run 'TestRecordCarries|TestOldRecords' -v`
Expected: FAIL to build, `unknown field Profile`.

- [ ] **Step 3: Write the implementation**

In `internal/usagelog/usagelog.go`, extend `Record`:

```go
// Record is one run's usage.
type Record struct {
	Time             time.Time `json:"time"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	// Profile and Provider identify a free-provider run, so free usage can be
	// separated from paid. Both are omitempty: records written before these
	// fields existed parse unchanged and read as paid.
	Profile  string `json:"profile,omitempty"`
	Provider string `json:"provider,omitempty"`
}
```

In `cmd/gophermind/main.go`, replace the usage-append block at lines 1312-1318:

```go
		// Persist usage for the cost dashboard when GOPHERMIND_USAGE_LOG is set.
		u := ag.Usage()
		freeCompat, isFree := freellm.CompatFor(cfg.Profile)
		if lp := strings.TrimSpace(os.Getenv("GOPHERMIND_USAGE_LOG")); lp != "" {
			rec := usagelog.Record{
				Time: time.Now(), Model: cfg.Model,
				PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, CostUSD: u.CostUSD,
			}
			if isFree {
				rec.Profile, rec.Provider = freeCompat.Profile, freeCompat.Upstream
			}
			_ = usagelog.Append(lp, rec)
```

(leave the budget-alert block that follows exactly as it is, and its closing brace).

Then, immediately after that `if` block closes, add the odometer update. It is
deliberately outside the `GOPHERMIND_USAGE_LOG` guard: the odometer is
self-contained and must count whether or not the cost log is enabled.

```go
		// The free-usage odometer counts unconditionally: unlike the cost log
		// it needs no opt-in env var. Failure to record is never fatal to a run.
		if isFree {
			if odo, err := freellm.LoadOdometer(odometerPath()); err == nil {
				_ = odo.Add(odometerPath(), freellm.Event{
					TS:       time.Now(),
					Profile:  freeCompat.Profile,
					Tokens:   int64(u.PromptTokens + u.CompletionTokens),
					Requests: 1,
				})
			}
		}
```

Add `"gophermind/internal/freellm"` to `main.go`'s imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/usagelog/ ./cmd/gophermind/ -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 5: Verify the odometer actually moves**

```bash
export GOPHERMIND_ODOMETER=/tmp/odo-check.json
go run ./cmd/gophermind free usage          # expect a zero reading
# A real turn against a no-key provider (needs network):
go run ./cmd/gophermind --profile free-ovhcloud ask "say hi in three words"
go run ./cmd/gophermind free usage          # expect a nonzero reading
rm -f /tmp/odo-check.json /tmp/odo-check.json.lock
```

Expected: the second `free usage` shows nonzero tokens and one request against
`free-ovhcloud`, plus a trip meter. If the network is unavailable, note that
this step was not run rather than reporting it as verified.

- [ ] **Step 6: Commit**

```bash
git add internal/usagelog/ cmd/gophermind/
git commit -m "feat: record free usage into the odometer

usagelog.Record gains omitempty Profile/Provider so free runs can be told
from paid ones and old JSONL lines parse unchanged.

The odometer update sits outside the GOPHERMIND_USAGE_LOG guard on
purpose: that log is off by default, and a counter that silently does
nothing for most users is not a counter.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 11: Attribution in the banner, status line, and /provider

**Files:**
- Modify: `internal/banner/banner.go` (add an attribution line)
- Modify: `internal/tui/view.go:36-40` (status line)
- Modify: `internal/tui/model.go:146,207` (carry profile and hyperlink support)
- Modify: `internal/tui/run.go:116` (pass the profile through)
- Modify: `internal/tui/update.go` (add `/provider`)
- Create: `internal/tui/provider.go`
- Test: `internal/tui/provider_test.go`, `internal/banner/banner_attribution_test.go`

**Interfaces:**
- Consumes: `AttributionFor`, `Attribution.Line`, `TripMeters`, `LoadOdometer` (Tasks 4-6).
- Produces: `func providerCard(profile, model, odometerPath string, now time.Time) string`, `func osc8(url, text string) string`.

Read each file before editing. Match the surrounding style; `internal/banner/banner.go` already uses lipgloss teal `#5AA6BC` for the tagline, and the attribution line reuses it.

**Critical constraint:** the existing TUI golden files must not change. The status-line additions are conditional on the profile being free, and the OSC 8 wrapper is off unless `hyperlinks` is true, which defaults false.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/provider_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderCardShowsAttributionAndOdometer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	card := providerCard("free-groq", "openai/gpt-oss-120b", path, time.Now())
	for _, want := range []string{"Groq", "https://groq.com", "openai/gpt-oss-120b", "odometer"} {
		if !strings.Contains(strings.ToLower(card), strings.ToLower(want)) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
}

func TestProviderCardForPaidProfile(t *testing.T) {
	card := providerCard("openai", "gpt-4o-mini", filepath.Join(t.TempDir(), "odo.json"), time.Now())
	if !strings.Contains(strings.ToLower(card), "not a free provider") {
		t.Errorf("paid profile should say so:\n%s", card)
	}
}

func TestOSC8WrapsWhenEnabled(t *testing.T) {
	got := osc8("https://groq.com", "model-x")
	if !strings.Contains(got, "\x1b]8;;https://groq.com") || !strings.Contains(got, "model-x") {
		t.Errorf("osc8 did not produce a hyperlink: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b]8;;\x1b\\") {
		t.Errorf("osc8 did not terminate the hyperlink: %q", got)
	}
}
```

Create `internal/banner/banner_attribution_test.go`.

The real signature is `RenderWith(o Options) string` (`internal/banner/banner.go:36`)
and `Options` currently holds only `Fortune` and `Tip` bools. Add two string
fields rather than changing the signature, so the existing `Render()` and every
current caller keep compiling:

```go
package banner

import (
	"strings"
	"testing"
)

func TestAttributionLineAppearsForFreeProfile(t *testing.T) {
	out := RenderWith(Options{Profile: "free-groq", Model: "openai/gpt-oss-120b"})
	if !strings.Contains(out, "Groq") || !strings.Contains(out, "https://groq.com") {
		t.Errorf("banner missing free-provider attribution:\n%s", out)
	}
}

func TestAttributionLineAbsentForPaidProfile(t *testing.T) {
	out := RenderWith(Options{Profile: "openai", Model: "gpt-4o-mini"})
	if strings.Contains(out, "free tier") {
		t.Error("banner shows free attribution for a paid profile")
	}
}

// Options{} with no profile must render exactly as it does today, which is
// what keeps every existing banner test and caller passing.
func TestZeroOptionsAddNoAttribution(t *testing.T) {
	if strings.Contains(RenderWith(Options{}), "free tier") {
		t.Error("a zero Options value produced attribution")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ ./internal/banner/ -run 'TestProvider|TestOSC8|TestAttributionLine' -v`
Expected: FAIL to build.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/provider.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"gophermind/internal/freellm"
)

// osc8 wraps text in an OSC 8 terminal hyperlink. Terminals without OSC 8
// support render the text unchanged, so this degrades rather than corrupts.
func osc8(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// providerCard renders the /provider readout: who is serving the current
// model, the free-tier terms, the lifetime odometer, and the trip meters.
func providerCard(profile, model, odoPath string, now time.Time) string {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		return fmt.Sprintf("Model %s is not from a free provider (profile %q).\nRun `gophermind free list` to see the free options.", model, profile)
	}
	a, _ := freellm.AttributionFor(profile, model)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", a.Line())
	fmt.Fprintf(&b, "Endpoint:  %s\n", c.BaseURL)
	if t := termsLine(c.Terms); t != "" {
		fmt.Fprintf(&b, "Terms:     %s\n", t)
	}
	if c.Note != "" {
		fmt.Fprintf(&b, "Note:      %s\n", c.Note)
	}

	odo, err := freellm.LoadOdometer(odoPath)
	if err == nil {
		tokens, requests := odo.Reading()
		fmt.Fprintf(&b, "\nFree odometer: %d tokens over %d requests (all free providers)\n", tokens, requests)
		var parts []string
		for _, m := range freellm.TripMeters(odo, c, now) {
			s := m.String()
			if m.Warn() {
				s += " <- near the limit"
			}
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "This window:   %s\n", strings.Join(parts, "   "))
		}
	}
	return b.String()
}

// termsLine renders the free-tier obligations worth warning about.
func termsLine(t freellm.Terms) string {
	var f []string
	if t.NonCommercial {
		f = append(f, "non-commercial use only")
	}
	if t.TrainsOnPrompts {
		f = append(f, "provider may train on prompts")
	}
	if t.IdentityCheck {
		f = append(f, "identity verification required")
	}
	return strings.Join(f, "; ")
}
```

In `internal/tui/model.go`, add two fields to the `model` struct and set them in
`newModel`. Extend `newModel`'s signature with `profile string` and add
`hyperlinks bool` (default false, set from `os.Getenv("TERM_PROGRAM") != ""` in
`run.go` only if you choose to enable it; leaving it false is acceptable for v1):

```go
	profile    string // active config profile; "" for the default endpoint
	hyperlinks bool   // terminal supports OSC 8; false keeps output plain
```

In `internal/tui/view.go`, replace lines 36 and 40's status construction so the
free segments appear only for a free profile. Keep the existing format exactly
when they do not:

```go
	name := m.model
	free := ""
	if a, ok := freellm.AttributionFor(m.profile, m.model); ok {
		if m.hyperlinks && a.Link != "" {
			name = osc8(a.Link, m.model)
		}
		free = " - " + a.Short()
	}
```

then use `name` in place of `m.model` and append `free` after it, in both the
`stateWorking` and `default` branches (`internal/tui/view.go:36` and `:40`).
Leave the `stateApproval` branch alone; it does not name the model. Add
`"gophermind/internal/freellm"` to that file's imports.

In `internal/tui/run.go:116`, pass `cfg.Profile` into `newModel`.

In `internal/tui/update.go`, add a case to the same switch that holds `/help`
(line 393), following its exact three-line shape:

```go
	case "/provider":
		m.appendLine(providerCard(m.profile, m.model, odometerPathTUI(), time.Now()))
		m.sync()
		return m, nil
```

Add `odometerPathTUI()` to `internal/tui/provider.go`, mirroring
`cmd/gophermind/free.go`'s `odometerPath`:

```go
// odometerPathTUI resolves the odometer location, honoring GOPHERMIND_ODOMETER
// so tests and alternate installs can redirect it.
func odometerPathTUI() string {
	if p := strings.TrimSpace(os.Getenv("GOPHERMIND_ODOMETER")); p != "" {
		return p
	}
	return freellm.DefaultOdometerPath()
}
```

Add `"os"` to that file's imports. Also add `/provider` to `helpLine()` so it is
discoverable.

For the banner, extend `Options` (`internal/banner/banner.go:19`) with two
string fields and emit the line inside `RenderWith` right after
`version.String()`, reusing the existing `taglineStyle` teal:

```go
type Options struct {
	Fortune bool   // include a random fortune under the banner
	Tip     bool   // include a rotating tip-of-the-day line
	Profile string // active config profile; a free-* one adds an attribution line
	Model   string // model in use, named in that attribution line
}
```

```go
	// Name the free provider serving this model, so the user always knows whose
	// free tier they are spending. Renders nothing for a paid or unset profile,
	// which keeps Render() and every existing caller byte-identical.
	if a, ok := freellm.AttributionFor(o.Profile, o.Model); ok {
		b.WriteString(taglineStyle.Render(a.Line()))
		b.WriteByte('\n')
	}
```

Then pass `cfg.Profile` and `cfg.Model` at whichever call site builds the
startup banner; find it with `git grep -n 'banner.Render'`.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... && go build ./...`
Expected: all green, **including the existing TUI golden tests unchanged**. If a
golden file now differs, the status-line change is leaking into the paid path;
fix the guard rather than re-recording the golden.

- [ ] **Step 5: See it work**

```bash
export GOPHERMIND_ODOMETER=/tmp/odo-demo.json
go run ./cmd/gophermind --profile free-ovhcloud
# then type: /provider
```

Expected: the provider card with OVHcloud, its link, the odometer, and a trip meter.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ internal/banner/
git commit -m "feat(tui): attribute the active model to its free provider

Adds a /provider card (attribution, terms, odometer, trip meters), a
startup banner line, and a status-line segment naming the provider with
an optional OSC 8 hyperlink on the model name.

Every addition is guarded on the profile being free, so the paid path and
the existing golden files are byte-identical.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 12: Sync script, generated docs, and the affiliate plan

**Files:**
- Create: `scripts/sync-free-providers.sh`
- Create: `docs/free-providers.md` (generated by that script)
- Create: `docs/affiliate-plan.md` (hand-written)
- Modify: `README.md` (a short free-provider section, linking to `docs/free-providers.md`)
- Modify: `CREDITS.md` (credit the upstream registry)

- [ ] **Step 1: Write the sync script**

Create `scripts/sync-free-providers.sh`:

```bash
#!/usr/bin/env bash
# Refresh the vendored free-provider registry from upstream and regenerate the
# docs table. The drift tests in internal/freellm are the point of step 5: a
# sync that invalidates compat.go fails here instead of at runtime.
set -euo pipefail

UPSTREAM="https://raw.githubusercontent.com/mnfst/awesome-free-llm-apis/main/data.json"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$REPO_ROOT/internal/freellm/data.json"
DOCS="$REPO_ROOT/docs/free-providers.md"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Fetching $UPSTREAM"
curl -fsSL "$UPSTREAM" -o "$tmp"

echo "Validating"
jq -e '.providers | length > 0' "$tmp" >/dev/null || {
  echo "FAILED: upstream data.json has no providers; leaving the vendored copy alone" >&2
  exit 1
}

if cmp -s "$tmp" "$DEST"; then
  echo "Already up to date ($(jq -r .lastUpdated "$DEST"))"
else
  cp "$tmp" "$DEST"
  echo "Updated to $(jq -r .lastUpdated "$DEST")"
fi

echo "Regenerating $DOCS"
"$REPO_ROOT/scripts/gen-free-providers-doc.sh" > "$DOCS"

echo "Running drift tests"
cd "$REPO_ROOT"
go test ./internal/freellm/... || {
  echo "" >&2
  echo "FAILED: the vendored registry no longer matches compat.go." >&2
  echo "Fix internal/freellm/compat.go to match the new data.json." >&2
  echo "Do NOT edit data.json to match compat.go." >&2
  exit 1
}
echo "Sync complete."
```

Create `scripts/gen-free-providers-doc.sh`:

```bash
#!/usr/bin/env bash
# Emit docs/free-providers.md from the vendored registry. Called by
# sync-free-providers.sh; the output is generated and must not be hand-edited.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA="$REPO_ROOT/internal/freellm/data.json"

cat <<EOF
# Free LLM providers

Generated from \`internal/freellm/data.json\`. Do not edit by hand; run
\`scripts/sync-free-providers.sh\`.

Registry vendored from [mnfst/awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis)
(CC0 1.0), upstream \`lastUpdated\`: $(jq -r .lastUpdated "$DATA").

Run one with \`gophermind --profile <profile> ask "hello"\`, or browse them with
\`gophermind free list\`.

| Provider | Models | Free tier | Link |
|---|---|---|---|
EOF

jq -r '.providers[] | "| \(.name) | \(.models | length) | \(.description | gsub("\\|"; "/")) | \(.url) |"' "$DATA"

cat <<'EOF'

## Providers that need no API key

These serve requests anonymously, which makes them the zero-signup way to try
gophermind:

- `free-ovhcloud` - OVHcloud AI Endpoints, 2 requests per minute per IP, EU-hosted
- `free-llm7` - LLM7.io, 10 requests per minute
- `free-kilocode` - Kilo Code, 200 requests per hour

## Free-tier terms worth reading

Some free tiers carry obligations beyond a rate limit. `gophermind free show
<profile>` prints them in full, and `gophermind free list` flags them.

- Non-commercial use only: Cohere
- Provider may train on your prompts: Google Gemini, Mistral AI
- Identity or real-name verification required at signup: ModelScope, SiliconFlow
EOF
```

- [ ] **Step 2: Run the script and verify it is idempotent**

```bash
chmod +x scripts/sync-free-providers.sh scripts/gen-free-providers-doc.sh
./scripts/sync-free-providers.sh
git diff --stat
./scripts/sync-free-providers.sh   # second run
git diff --stat                     # must be identical to the first
```

Expected: the script succeeds, `docs/free-providers.md` exists, and a second run
produces no further change. If `go test ./internal/freellm/...` fails here,
upstream has drifted; fix `compat.go`, not `data.json`.

- [ ] **Step 3: Write the affiliate plan**

Create `docs/affiliate-plan.md`:

```markdown
# Affiliate and referral links: policy and plan

**Status as of 2026-09-10: gophermind ships no affiliate links.** Every
`Affiliate` field in `internal/freellm/compat.go` is empty.

## Why there are none yet

A survey on 2026-09-10 found no public affiliate or referral program for any of
the sixteen providers in the registry. OpenRouter's referral arrangement is not
public. The rest are free tiers with no program at all.

## Policy, which holds whether or not a link ever exists

1. **A referral link is always disclosed.** `Attribution.Line` and
   `Attribution.Short` append `(referral link)` whenever `Affiliate` is set.
   There is no code path that emits one without the marker, and
   `TestReferralAlwaysDisclosed` fails the build if someone adds one.
2. **A user can always opt out.** Setting `GOPHERMIND_NO_AFFILIATE` to any
   non-empty value forces the plain provider website.
3. **A link's destination never changes silently.** Adding, removing, or
   repointing an `Affiliate` value requires a CHANGELOG entry naming the
   provider.
4. **Provider ranking is never influenced by revenue.** `Compats()` sorts by
   whether a key is needed, then whether the provider is supported, then
   alphabetically. Revenue is not an input and must not become one.
5. **No link is added without reading the program terms**, specifically whether
   the program requires disclosure language we are not using, and whether it
   permits use in an open-source tool.

## Outreach order, when the time comes

Ranked by plausible revenue against effort. All three are inference
marketplaces where a referred user may go on to spend, which is the only shape
of provider here where an affiliate arrangement makes sense at all.

1. **OpenRouter** - the largest paid surface of any provider in the registry, and
   the one most likely to convert a free-tier user into a paying one. Contact
   through their support channel; there is no public program to sign up for.
2. **Hugging Face** - a credit-metered free tier that naturally leads to paid
   Inference Providers. Their partner program is the route to ask about.
3. **NVIDIA (build.nvidia.com)** - the NVIDIA Developer Program is the existing
   relationship; ask whether it has any referral component.

Not worth approaching: the no-key providers (nothing to refer), the
national-cloud providers (ModelScope, SiliconFlow, Z AI), and Cohere (whose
free tier is explicitly non-commercial).

## What to do first

Read each program's terms against policy items 4 and 5 above before contacting
anyone. If a program requires ranking influence or forbids disclosure, decline
it. The disclosure marker is not negotiable.
```

- [ ] **Step 4: Update README.md and CREDITS.md**

In `README.md`, add a short section after the installation instructions. Read
the surrounding style first and match it, including whether that file uses em
dashes:

```markdown
## Run it for free

gophermind ships a registry of free LLM API providers. Three of them need no API
key at all:

    gophermind free list
    gophermind --profile free-ovhcloud ask "hello"

`gophermind free show <profile>` prints a provider's models, rate limits, and
free-tier terms. `gophermind free usage` shows a lifetime odometer of the free
tokens and requests you have used.

See [docs/free-providers.md](docs/free-providers.md) for the full table.
```

In `CREDITS.md`, add an entry matching that file's existing format:

```markdown
- [awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis) by mnfst,
  CC0 1.0. The free-provider registry vendored at `internal/freellm/data.json`.
```

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add scripts/ docs/ README.md CREDITS.md
git commit -m "feat: sync script, generated provider docs, and affiliate policy

sync-free-providers.sh refreshes the vendored registry and then runs the
drift tests, so a sync that invalidates compat.go fails at sync time
rather than at runtime.

The affiliate plan records that no provider has a public program today,
and the policy that holds if one ever does: disclosure is enforced by a
test, opt-out via GOPHERMIND_NO_AFFILIATE, and provider ranking never
takes revenue as an input.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

## Final verification

- [ ] `go build ./... && go test ./... -race` is green.
- [ ] `go run ./cmd/gophermind free list` lists the no-key providers first.
- [ ] `go run ./cmd/gophermind free show free-cohere` shows the non-commercial term.
- [ ] `go run ./cmd/gophermind free check free-ovhcloud` reports a real model list, or its failure is recorded in `compat.go`.
- [ ] A real turn against a free profile moves `gophermind free usage`.
- [ ] `./scripts/sync-free-providers.sh` is idempotent on a second run.
- [ ] `git grep -n '—' -- internal/freellm cmd/gophermind/free.go docs/affiliate-plan.md` returns nothing (no em dashes in new files).
- [ ] `git grep -nE 'sk-|api_key|Bearer ' -- internal/freellm/compat.go` returns nothing.
- [ ] The existing TUI golden files are unchanged: `git diff --stat` on `internal/tui/testdata` is empty.
