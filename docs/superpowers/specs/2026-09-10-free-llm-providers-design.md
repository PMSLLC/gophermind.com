# Free LLM Providers for gophermind - Design

**Date:** 2026-09-10
**Status:** Approved
**Upstream:** `github.com/mnfst/awesome-free-llm-apis` (CC0 1.0), `data.json` @ `lastUpdated: 2026-08-21`

> Style note: this document uses plain hyphens, not em dashes, per the global
> writing rule for newly created documents. Older specs in this directory
> predate that rule.

## Problem

Gophermind requires an OpenAI-compatible endpoint and bakes in none
(`internal/config/config.go:56`, `defaultBaseURL = ""`). `Validate` fails
without one. A new user therefore cannot run gophermind at all until they have
either a local llama.cpp server or a paid API key.

The three built-in profiles do not close that gap: `local-llama` needs a server
the user has to stand up, `openai` needs a paid key, and `anthropic-proxy` is a
placeholder pointing at `127.0.0.1`.

There is no free option to run against, and no way to discover one.

Separately, the model in use is displayed as a bare string
(`internal/tui/view.go:36-40`) with no indication of who is serving it or where
that provider lives.

## Goals

1. A user with no API key and no local server can run gophermind against a free
   endpoint, discoverable from inside the tool.
2. The provider serving the current model is always attributable: named, linked,
   and honest about its free-tier terms.
3. The registry can be refreshed from upstream without hand-editing, and a
   refresh that invalidates our assumptions fails loudly.
4. Affiliate revenue is possible later without ever making a link deceptive.

## Non-goals

- Not a provider abstraction layer. `llm.Client` already speaks OpenAI-compatible
  HTTP and is not modified by this work.
- Not automatic provider failover. `Client.Fallbacks` already exists for models;
  cross-provider fallback is a separate concern.
- Not key management. API keys continue to come only from the environment.
- Not a fork of upstream. `data.json` is vendored verbatim and never hand-edited.

## Key finding: three providers need no API key at all

This is the feature that actually satisfies "a free option to run against".
Three entries serve requests anonymously:

| Provider | Base URL | Anonymous limit |
|---|---|---|
| OVHcloud AI Endpoints | `https://oai.endpoints.kepler.ai.cloud.ovh.net/v1` | 2 RPM per IP per model, EU-hosted |
| LLM7.io | `https://api.llm7.io/v1` | 10 RPM, 60 req/hr |
| Kilo Code | `https://api.kilo.ai/api/gateway` | 200 req/hr |

These get first-class treatment: sorted to the top of `free list`, marked
`no key needed`, and named in the setup wizard and README as the zero-signup path.

## Architecture

### New package: `internal/freellm`

| File | Responsibility |
|---|---|
| `data.json` | Vendored upstream registry, verbatim, CC0. Never hand-edited. |
| `registry.go` | `//go:embed data.json`; `Provider`/`Model` types; `Load()` parsed once via `sync.Once`; `Lookup(name)`, `All()`. |
| `compat.go` | The only hand-maintained data: `map[string]Compat` keyed by profile name. |
| `attribution.go` | `Attribution(model, profile string) Attribution` and its renderers. |
| `check.go` | `Check(ctx, Compat, apiKey) ([]string, error)` - `GET {base}/models`, returns model IDs. |

`registry.go` and `compat.go` are separate on purpose: a sync overwrites the
first and must never touch the second.

#### Types

```go
// Compat records what gophermind knows about running against a free provider,
// as distinct from what upstream records about the provider itself.
type Compat struct {
    Profile      string // gophermind profile name, e.g. "free-groq"
    Upstream     string // provider "name" in data.json; must resolve
    BaseURL      string // OpenAI-compatible endpoint (may differ from upstream baseUrl)
    DefaultModel string // explicit; never left empty (see "Explicit default models")
    Website      string // human-facing site, shown in attribution
    Affiliate    string // referral URL; empty for all providers today
    NoKey        bool   // serves requests anonymously
    Supported    bool   // false => listed for discovery, not runnable as a profile
    Terms        Terms  // free-tier obligations worth warning about
    Note         string // one line explaining an override or a Supported:false
}

type Terms struct {
    NonCommercial  bool // free tier forbids commercial use
    TrainsOnPrompts bool // provider may train on free-tier prompts
    IdentityCheck  bool // signup requires real-name / account verification
}
```

### Endpoint overrides

Three providers do not speak OpenAI at the `baseUrl` upstream records. `compat.go`
carries the corrected URL and a `Note` saying why. A fourth, Groq, needs no
correction but gets a `Note` because its `/openai/v1` path looks like an error:

| Provider | Upstream `baseUrl` | gophermind `BaseURL` |
|---|---|---|
| Google Gemini | `.../v1beta` | `https://generativelanguage.googleapis.com/v1beta/openai/` |
| Cohere | `https://api.cohere.com/v2` | `https://api.cohere.ai/compatibility/v1` |
| Ollama Cloud | `https://ollama.com/api` | `https://ollama.com/v1` |
| Groq | `https://api.groq.com/openai/v1` | unchanged, but non-obvious; noted |

Cloudflare Workers AI is `Supported: false`: its endpoint embeds an account ID
(`/accounts/{account_id}/ai/run`) that cannot be known statically. The `Note`
directs the user to the existing per-profile override,
`GOPHERMIND_PROFILE_FREE_CLOUDFLARE_BASE_URL`, which already works today and
needs no new machinery.

### Explicit default models

Every `Supported` entry sets `DefaultModel`. This is deliberate: leaving `Model`
empty triggers auto-discovery from `/v1/models`, and several of these endpoints
list paid models alongside free ones, so discovery could silently select a model
that bills the user. Free profiles never auto-discover.

Selection rule, applied in order: prefer a model whose free-tier rate limit is
highest, then prefer general-purpose text over roleplay/code specialists, then
prefer the larger context window.

| Profile | Default model | Key? |
|---|---|---|
| `free-ovhcloud` | `gpt-oss-120b` | no |
| `free-llm7` | `gpt-oss:20b` | no |
| `free-kilocode` | `nvidia/nemotron-3-super-120b-a12b:free` | no |
| `free-groq` | `openai/gpt-oss-120b` | yes |
| `free-nvidia` | `openai/gpt-oss-120b` | yes |
| `free-gemini` | `gemini-3.5-flash` | yes |
| `free-openrouter` | `nvidia/nemotron-3-super-120b-a12b:free` | yes |
| `free-mistral` | `mistral-small-2603` | yes |
| `free-ollama-cloud` | `gpt-oss:120b` | yes |
| `free-zai` | `glm-4.7-flash` | yes |
| `free-cohere` | `command-a-03-2025` | yes |
| `free-huggingface` | `Qwen/Qwen2.5-7B-Instruct` | yes |
| `free-modelscope` | `Qwen/Qwen3.5-35B-A3B` | yes |
| `free-siliconflow` | `Qwen/Qwen3-8B` | yes |
| `free-aionlabs` | `aion-labs/aion-3.0` | yes |
| `free-cloudflare` | n/a | `Supported: false` |

`Supported` starts `true` for the fifteen above and is corrected downward by the
implementation task that runs `free check` against each one. The probe is the
arbiter; no entry ships claiming compatibility that was never observed.

### Config integration

`ApplyProfile` (`internal/config/config.go:427`) gains one branch. Current logic:

```
per-profile env var  >  built-in profile default
unknown name + no env base URL  =>  error
```

New logic inserts a lookup between those two: a name that is not built in and has
no env base URL is looked up in `freellm`. If found and `Supported`, its
`BaseURL`, `DefaultModel` and timeout apply. If found and not `Supported`, the
error names the profile and quotes its `Note`. If not found, the existing
unknown-profile error is unchanged.

Invariants preserved exactly:

- API keys are read only from `GOPHERMIND_PROFILE_<NAME>_API_KEY` and are never
  defaulted from the registry. `compat.go` contains no key material.
- Env overrides still win over registry values, per field.
- `free-*` names satisfy the existing `profileNameRe`; no regex change.
- `BuiltinProfileNames()` is untouched. A sibling `FreeProfileNames()` feeds the
  wizard, so the existing three profiles never get buried under sixteen new ones.

### CLI: `gophermind free`

Added in `cmd/gophermind/main.go` alongside the existing subcommand switch.

1. `free list` - table of profile, provider, default model, free tier, terms
   flags, link. Zero-key providers first, then supported, then unsupported.
2. `free show <profile>` - full card: every model with context and rate limit,
   the free-tier terms in full, the website, and the exact `export` lines to run
   it.
3. `free check <profile>` - `GET {BaseURL}/models` with the env key if present.
   Prints the model IDs returned, or the HTTP status and body on failure. This is
   how a user proves an endpoint works before trusting it, and how the sync
   script proves `compat.go` is still accurate.

`free check` is the only network call in the package, is bounded by a context
deadline, and never prints the API key.

### Attribution: four surfaces

`Attribution()` returns a struct; each surface renders it differently.

1. **Startup banner** (`internal/banner`): one line under the gopher art,
   `openai/gpt-oss-120b via Groq (free tier) https://groq.com`, in the existing
   teal `#5AA6BC`, degrading to plain text off-TTY. Absent entirely when the
   active profile is not a free provider.
2. **Status line** (`internal/tui/view.go:36`): `m.model` wrapped in an OSC 8
   hyperlink to the provider website. Gated by a `hyperlinks bool` on the model,
   defaulting false in tests so the existing golden output is unchanged. Detection
   is env-based (`TERM`, `TERM_PROGRAM`), and an undetected terminal renders the
   plain string it renders today.
3. **`/provider` slash command** (`internal/tui/update.go`): prints the same card
   as `free show` for the active profile, including terms and any referral link.
4. **`docs/free-providers.md`**: generated from `data.json` plus `compat.go` by
   the sync script. Linked from `README.md`. Never hand-edited.

### Affiliate links: plumbed, empty, and disclosed by construction

Research on 2026-09-10 found no public affiliate or referral program for any of
the sixteen providers. OpenRouter's is not public. The rest are free tiers with
no program at all. So `Affiliate` ships empty everywhere.

The mechanism ships anyway, with disclosure built into the type rather than left
to the caller:

- When `Affiliate` is non-empty, `Attribution()` uses it **and** appends
  `(referral link)` to every rendering. There is no code path that emits a
  referral URL without that marker.
- `GOPHERMIND_NO_AFFILIATE=1` forces `Website` even when `Affiliate` is set.
- A test asserts the invariant directly: for every `Compat` with a non-empty
  `Affiliate`, every renderer's output contains `(referral link)`. This fails
  the build if someone later adds a link and bypasses the marker.

`docs/affiliate-plan.md` records the rest: which providers to approach and in
what order, what to ask for, the disclosure rule above stated as policy, and the
rule that a link's destination is never changed without a CHANGELOG entry.

### Sync

`scripts/sync-free-providers.sh`:

1. Fetch upstream `data.json` over HTTPS to a temp file.
2. Validate it parses and has a non-empty `providers` array. Abort on failure
   without touching the tree.
3. Write it to `internal/freellm/data.json`.
4. Regenerate `docs/free-providers.md`.
5. Run `go test ./internal/freellm/...`.

Step 5 is the point of the script. A drift test asserts every `Compat.Upstream`
resolves to a provider still present in `data.json`, and that every
`DefaultModel` is still listed for that provider. If upstream drops a provider or
renames a model, the sync fails with the specific entry named, instead of
shipping a profile that 404s at runtime.

## Error handling

| Condition | Behavior |
|---|---|
| `--profile free-nope` | Existing unknown-profile error, unchanged |
| `--profile free-cloudflare` | Error naming the profile and quoting its `Note`, pointing at the env override |
| Embedded `data.json` malformed | Panic at `Load()`. It is compiled in; a bad parse is a build defect, and a test catches it before release |
| `free check` non-2xx | Print status and body verbatim, exit non-zero. Never print the key |
| `free check` with no key on a key-required provider | Say so before making the request |
| Terminal without OSC 8 | Plain model name, exactly as today |

## Testing

TDD throughout. No test touches the network.

- **registry**: embedded JSON parses; sixteen providers; `Lookup` hit and miss.
- **drift**: every `Compat.Upstream` resolves; every `DefaultModel` is listed
  upstream for that provider. This is the test that makes sync safe.
- **compat**: every `Supported` entry has an `https://` `BaseURL`, a non-empty
  `DefaultModel`, and a `Website`; every entry has a `Note` when it overrides
  upstream or sets `Supported: false`; no entry contains anything key-shaped.
- **config**: `--profile free-groq` resolves to the expected base URL and model;
  env vars still override per field; `APIKey` stays empty with no env var set;
  unsupported and unknown profiles produce distinguishable errors.
- **attribution**: plain rendering; the `(referral link)` invariant above;
  `GOPHERMIND_NO_AFFILIATE=1` forces `Website`; OSC 8 emitted only when enabled.
- **check**: `httptest.Server` for 200, 401, 404 and malformed-body cases.
- **CLI**: golden output for `free list` and `free show`.
- **regression**: existing TUI golden files unchanged, proving surface 2 is inert
  by default.

## Files

New (9): `internal/freellm/{data.json,registry.go,compat.go,attribution.go,check.go}`,
`cmd/gophermind/free.go`, `scripts/sync-free-providers.sh`,
`docs/free-providers.md`, `docs/affiliate-plan.md` (plus `_test.go` siblings).

Modified (6): `internal/config/config.go`, `cmd/gophermind/main.go`,
`internal/banner/banner.go`, `internal/tui/view.go`, `README.md`, `CREDITS.md`.

## Risks

- **Free-tier terms are a licensing hazard, not just a rate limit.** Cohere's
  trial key is non-commercial. Gemini and Mistral may train on free-tier prompts.
  Running gophermind against these on client code could breach an agreement.
  Mitigation: `Terms` flags surfaced in `free list`, in full in `free show` and
  `/provider`, and in the generated docs table. Surfacing is all this design
  does; it does not block the user.
- **Upstream is a third-party list that could go stale or wrong.** Mitigation:
  `free check` proves an endpoint rather than trusting the entry, and the drift
  test fails the sync rather than the runtime.
- **Sixteen new profile names crowd the three existing ones.** Mitigation: the
  `free-` prefix, a separate `FreeProfileNames()`, and `BuiltinProfileNames()`
  left untouched.

## Attribution to upstream

`data.json` is CC0, so no notice is legally required. `CREDITS.md`,
`docs/free-providers.md` and the package doc comment credit
`github.com/mnfst/awesome-free-llm-apis` anyway, with the vendored `lastUpdated`
date, so the provenance and staleness of the data are both visible.
