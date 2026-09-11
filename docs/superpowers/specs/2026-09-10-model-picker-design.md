# Model Picker with Per-Model Metering and Proactive Cycling - Design

**Date:** 2026-09-10
**Status:** Approved
**Builds on:** `2026-09-10-free-llm-providers-design.md` (the registry, odometer and trip meters this reuses)

> Style note: plain hyphens, no em dashes, per the global rule for new documents.

## Problem

A user cannot see what models are available to them, how much of each one's
free allowance is left, or switch between them without editing configuration.
When a model runs out of quota they discover it by getting an error.

## What already exists, and what does not

This matters because the obvious feature is half built.

| Capability | State today |
|---|---|
| Fail over to another model on 429 | **Exists.** `llm.Client.Fallbacks` is an ordered model list, and `statusFallbackEligible` (`internal/llm/fallback.go:82`) treats 429, 5xx and 404 as eligible. |
| Ordered model preference | Exists as configuration only: `GOPHERMIND_FALLBACK_MODELS`, applied at `cmd/gophermind/main.go:634` and `desktop/deps.go:68`. No UI. |
| Per-provider usage | Exists. The odometer's `Per` map and `TripMeters` cover it. |
| **Per-model usage** | **Missing.** The odometer keys on profile, never model. |
| **A model catalogue** | **Missing.** `GET /models` returns only the active endpoint's ids. |
| **Proactive switching** | **Missing.** Today's failover is reactive: the request fails first. |
| **Model homepage links** | **Missing.** The registry has a `Website` per provider, nothing per model. |

So this design adds metering granularity, a catalogue, a switching policy, and
two UI surfaces. It deliberately does NOT reimplement failover.

## Goals

1. See every model, what it costs to reach, and how much allowance is left.
2. Switch model deliberately, or let it switch before a quota runs out.
3. Order preference once, and have that order followed everywhere.
4. Always know whose service is answering, with a link.

## Non-goals

- Not a replacement for `Client.Fallbacks`. The reactive 429 path stays exactly
  as it is and remains the safety net.
- Not mid-turn switching. See "Why switching happens between turns".
- Not a billing or cost estimator. This measures free allowance, not money.

## Architecture

### 1. Per-model metering, added without disturbing the odometer's invariant

The odometer's monotonicity was hard won and must not regress. The change is
purely additive:

```go
type Event struct {
	TS       time.Time `json:"ts"`
	Profile  string    `json:"profile"`
	Model    string    `json:"model,omitempty"` // NEW
	Tokens   int64     `json:"tokens"`
	Requests int64     `json:"requests"`
}

type Odometer struct {
	Tokens   int64                    `json:"tokens"`
	Requests int64                    `json:"requests"`
	Per      map[string]ProviderTotal `json:"per"`       // unchanged, by profile
	PerModel map[string]ProviderTotal `json:"per_model"` // NEW, by "profile/model"
	Since    time.Time                `json:"since"`
	Events   []Event                  `json:"events"`
}
```

Properties this preserves, each of which has an existing test:

- A state file written before this change loads with a nil `PerModel`, which is
  allocated on first write. Lifetime totals are untouched, so no reading moves.
- `mergeUp` raises `PerModel` exactly as it raises `Per`: per key, per field,
  never downward. The monotonic guarantee is unchanged in kind.
- `Event.Model` is `omitempty`, so old ring entries still parse. They simply
  contribute to `Per` and not to `PerModel`, which is honest: we do not know
  which model they were.

The key is `profile + "/" + model` rather than a nested map, because it keeps
`mergeUp` a flat loop over one map type and avoids a second merge shape.

### 2. Remaining allowance, and where it does not exist

`TripMeters` gains a per-model variant. The rule from the free-provider spec
carries over unchanged and is the whole reason this feature can be trusted:

**A denominator is only ever shown when upstream published one.**

- Free-tier model with a parsed quota: `312/1,000 RPD`, remaining 688.
- Free-tier model whose `rateLimit` did not parse: a bare count, no remaining.
- A model on the user's own endpoint (for example the qwen on their own box):
  usage with no denominator at all. There is no quota to be near.

"Near capacity" is therefore undefined for anything without a published quota,
and such models are never auto-switched away from.

### 3. The catalogue API

Three routes, registered in `internal/serve` beside the existing `GET /models`:

```
GET   /models/catalogue    the full list, one entry per model
GET   /models/preferences  the ordered preference list
PATCH /models/preferences  replace the order
```

A catalogue entry:

```go
type CatalogueEntry struct {
	ID          string `json:"id"`           // model id as the endpoint wants it
	Provider    string `json:"provider"`     // "Groq", or "" for the local endpoint
	Profile     string `json:"profile"`      // "free-groq", or "" for the configured endpoint
	Reachable   bool   `json:"reachable"`
	Reason      string `json:"reason,omitempty"` // why not, when Reachable is false
	Used        int64  `json:"used"`
	Quota       int64  `json:"quota,omitempty"`  // absent when none was published
	Unit        string `json:"unit,omitempty"`   // "requests" or "tokens"
	Window      string `json:"window,omitempty"` // "RPD", "TPM", ...
	Context     string `json:"context,omitempty"`
	Modality    string `json:"modality,omitempty"`
	Terms       []string `json:"terms,omitempty"` // freellm.TermsFlags
	ProviderURL string `json:"provider_url,omitempty"`
	ModelURL    string `json:"model_url,omitempty"`
}
```

The catalogue is assembled from three sources: the vendored registry (every
model, with context, modality and rate limit), `freellm.Compats` (reachability
and terms), and the odometer (usage). The active endpoint's own `/v1/models`
response contributes entries with an empty `Profile`.

`Reachable` is computed, never guessed: a no-key provider is reachable; a keyed
provider is reachable only when its `GOPHERMIND_PROFILE_<NAME>_API_KEY` is set;
an unsupported entry is not reachable and `Reason` carries its `Note`.

Preferences persist next to the odometer, under the OS cache directory, as a
plain ordered list of `profile/model` keys. Server-side storage is what lets
the order follow the user between the desktop app, the TUI and iOS.

### 4. Proactive cycling

A model is **near capacity at the configured threshold** (default 90 percent)
of a published quota within that quota's window. The threshold is a setting,
not a constant: a 2 RPM anonymous tier and a 10,000 RPD tier want very
different values. When the active model crosses that line, the next reachable
model in preference order becomes active.

**Why switching happens between turns, never inside one.** A turn's context is
built for one model: its token budget, its tool-calling dialect, and the
messages already sent to it. Swapping models mid-turn would split one
conversation across two backends and silently change the context window under
a request already in flight. The switch is therefore evaluated once, before a
turn starts.

Cycling is **off by default**; the button toggles it. When off, a model near
capacity is shown as such and nothing moves.

The existing reactive fallback is untouched and sits underneath. Proactive
switching handles the expected case using published numbers; the 429 path
still catches the cases published numbers get wrong, notably a key shared with
another client, where our local count understates real consumption.

### 5. UI

**Dropdown**, in the desktop app first:

```
gpt-oss-120b          Groq               312/1,000 RPD
qwen3.6-35b-a3b       your endpoint      4,506 tokens
command-a-03-2025     Cohere             needs a key   non-commercial
```

Four filters, because a 118-entry catalogue is otherwise unusable:

1. **Reachability**, defaulting to "usable now" so the list starts short.
2. **Has capacity left**, driven by the same meters as cycling.
3. **Terms**, to exclude non-commercial or trains-on-prompts providers.
4. **Provider and modality.**

**Settings panel.** Every judgment call in this design is a setting, not a
constant. A default that cannot be changed is a decision imposed on the user,
and the ones below are exactly the choices someone will reasonably disagree
with.

| Setting | Default | Why it is a setting |
|---|---|---|
| Model preference order | Registry order: no-key providers first | The core of the feature. Drag to reorder. |
| Cycle automatically on capacity | Off | Some users want to know rather than be moved. |
| Capacity threshold | 90 percent | 90 is a guess. A user on a 2 RPM tier wants to switch far earlier than one on 10,000 RPD. |
| When every model is near capacity | Stay on the current one | The alternative, stop and ask, is legitimate for someone who must not exceed a free tier. |
| Default filter: reachability | Usable now | Determines whether the dropdown opens short or complete. |
| Default filter: has capacity left | Off | Hiding exhausted models is helpful to some, confusing to others who wonder where a model went. |
| Excluded terms | None excluded | The real safety control. Excluding non-commercial and trains-on-prompts providers makes them unusable everywhere, including by automatic cycling, so client work cannot silently land on Gemini's free tier. |
| Provider and modality filters | All shown | Ordinary narrowing. |
| Endpoint: embedded or remote | Embedded | From the desktop app spec; it belongs in the same panel rather than a second one. |

Two properties this panel must have:

- **An exclusion is an exclusion.** A provider excluded by terms is removed
  from cycling and from `Fallbacks`, not merely hidden from the dropdown. A
  filter that only affects display would let automatic cycling select the very
  provider the user excluded, which is the worst possible failure for the one
  setting that exists for legal reasons.
- **Unreachable models state their remedy.** Not "unavailable" but the exact
  variable to set, for example `GOPHERMIND_PROFILE_FREE_GROQ_API_KEY`, so the
  panel doubles as the instructions for widening what is reachable.

Settings persist server-side beside the preference order, so they follow the
user between the desktop app, the TUI and iOS, and so an exclusion made in one
client cannot be bypassed by another.

### 6. Links

- **Provider homepage: always.** `Compat.Website` already exists for all 16.
- **Model page: only when derivable.** A Hugging Face-style `owner/name` id
  maps to `huggingface.co/owner/name`. Everything else omits `ModelURL` rather
  than emitting a guess that 404s. This follows the same rule as quotas: state
  what is known, omit what is not.

## Error handling

| Condition | Behavior |
|---|---|
| Catalogue assembly cannot reach the active endpoint | Return registry entries and mark the endpoint's own models unavailable with the error text. Never fail the whole catalogue. |
| Preferences file missing or corrupt | Fall back to registry order, log once, rewrite on next change. Never block a turn. |
| Preference names a model no longer in the catalogue | Skip it when cycling, keep it in the stored list, show it greyed in settings. Upstream removing a model must not silently reorder the user's list. |
| Every preferred model is near capacity | Follow the configured choice: stay on the current model (default, since refusing to run is worse than one 429) or stop and ask, for a user who must not exceed a free tier. |
| A model's quota did not parse | Never auto-switch away from it; there is no line to cross. |

## Testing

- **Odometer compatibility**: a state file written before `PerModel` existed loads, accumulates, and its lifetime reading never moves. This is the regression test that matters most.
- **Monotonicity, again**: `PerModel` entries never decrease, including a stale in-memory instance merging against a newer file.
- **Denominator honesty**: an entry whose `rateLimit` did not parse never reports a `Quota`; a local-endpoint model never reports one either.
- **Reachability**: a keyed provider flips to reachable exactly when its env var is set, via `t.Setenv`.
- **Cycling policy**: at 89 percent no switch, at 90 percent switch, and with cycling off no switch at either. No switch occurs for a model with no published quota. With every preferred model near capacity, the active model is retained.
- **Preference durability**: reorder, restart, order survives; a preference naming a vanished model is skipped but retained.
- **Model URL derivation**: an `owner/name` id yields a Hugging Face URL; `gpt-oss-120b` yields none.
- **API**: golden JSON for one entry of each shape (free with quota, free without, local endpoint, unreachable).

## Delivery, in three landable pieces

Each is useful on its own, which is the point.

1. **Per-model metering.** The odometer change plus per-model trip meters. Ships invisibly; `gophermind free usage` can show a per-model breakdown immediately.
2. **Catalogue API.** The three routes, assembled and tested, with no UI. Verifiable with curl.
3. **UI.** Dropdown, filters, settings panel, and the cycle toggle in the desktop app. The TUI and iOS can adopt the same endpoints later without reimplementing anything.

## Risks

- **The odometer is the one piece of durable state this feature touches**, and it has already been the subject of a false monotonicity claim once. Mitigation: the change is additive, the old field is untouched, and the compatibility test is written before the change.
- **Local counts drift from the provider's** when a key is shared with another client. Mitigation: this is exactly why the reactive 429 fallback stays. Proactive switching is an optimization, not the guarantee.
- **A 118-entry catalogue is a bad dropdown.** Mitigation: reachability filtering defaults to on, which typically leaves about a dozen entries.
