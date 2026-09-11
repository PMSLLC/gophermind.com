# Model Picker Phase 3: Selection Policy and UI - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Choose a model before each turn according to the user's stated preferences, and give them a dropdown, filters and a settings panel to state those preferences.

**Architecture:** The selection policy is a pure function in `internal/modelcat`, so it is testable without a server or a UI. The desktop frontend gains two components rather than growing `App.tsx`, which is already 381 lines carrying chat and approvals.

**Tech Stack:** Go 1.26.5, module `gophermind`. React + TypeScript in `desktop/frontend`. No new dependency on either side.

**Spec:** `docs/superpowers/specs/2026-09-10-model-picker-design.md`, sections 4 and 5.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only on the Go side. No new npm dependency.
- NO em dashes and NO emoji in code, comments, or UI strings. Plain hyphens.
- Every exported Go symbol gets a doc comment.
- No test may make a network call.
- `go build ./... && go test ./... -race` green, and `desktop/frontend` type-checks and builds.
- The existing `desktop/server_test.go`, `desktop/cors_test.go` and `desktop/approval_test.go` must keep passing unchanged: they pin loopback-only binding, the token requirement, the exact `wails://wails` origin, and the approval gate.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## What phase 2 provides

`internal/modelcat`: `Entry`, `Build(o, s, endpointModels, now) []Entry`, `Settings` with `Order`, `CycleOnCapacity`, `CapacityPercent`, `WhenAllFull`, `FilterReachable`, `FilterHasCapacity`, `ExcludedTerms`, `CustomLinks`, plus `LoadSettings`, `SaveSettings`, `SettingsPath`, `ValidateLink`, `CapacityThreshold`. Routes `GET /models/catalogue`, `GET|PATCH /models/settings`.

---

### Task 1: The selection policy, as a pure function

**Files:**
- Create: `internal/modelcat/select.go`
- Test: `internal/modelcat/select_test.go`

**Interfaces produced:**
```go
// Choice is the outcome of applying the user's preferences to the catalogue.
type Choice struct {
	Profile, Model string
	Switched       bool   // the active model was changed
	Reason         string // why, for the UI to show; empty when nothing changed
}

// Next decides which model should serve the next turn.
func Next(entries []Entry, s Settings, currentProfile, currentModel string) Choice
```

**The rules, in order. These are the whole task; the UI only renders them.**

1. **An excluded term is an exclusion, not a filter.** Any entry whose `Terms` intersect `s.ExcludedTerms` is removed from consideration entirely, before anything else. It can never be selected, however preferred. This is the one rule that exists for legal rather than convenience reasons: a user who excludes "non-commercial" must not have automatic cycling quietly pick Cohere.
2. An unreachable entry is never selected.
3. If `CycleOnCapacity` is false, keep the current model. Never switch.
4. If the current model is not `NearCapacity`, keep it.
5. Otherwise walk `s.Order` and pick the first entry that is reachable, not excluded, and not `NearCapacity`.
6. If `s.Order` yields nothing, walk the remaining catalogue in its natural order under the same conditions.
7. If nothing qualifies, honor `s.WhenAllFull`: `"stay"` keeps the current model; `"ask"` returns the current model with `Switched` false and a `Reason` saying every option is full. **Never return an empty choice**; refusing to run is worse than one 429.

**A model with no published quota is never `NearCapacity`** (phase 2 guarantees this), so rule 4 keeps it forever. That is correct: there is no line to cross.

- [ ] **Step 1: Write the failing tests**

Create `internal/modelcat/select_test.go`. Cover each rule explicitly, with a small hand-built `[]Entry` rather than the real registry so the cases are readable:

- cycling off: near-capacity current model is kept
- cycling on, current not near capacity: kept
- cycling on, current near capacity: moves to the first preferred reachable entry
- preference order is honored over catalogue order
- an unreachable entry is skipped even when first in `Order`
- **an excluded-term entry is never selected even when it is first in `Order` and everything else is full** (the rule 1 test, and the most important one here)
- every option full, `WhenAllFull: "stay"`: current retained, `Switched` false
- every option full, `WhenAllFull: "ask"`: current retained, `Reason` non-empty
- a model with no quota is never treated as near capacity
- `Next` on an empty catalogue returns the current model rather than an empty `Choice`

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/modelcat/ -run TestNext -v`

- [ ] **Step 3: Implement `internal/modelcat/select.go`**

Keep it a pure function: no I/O, no clock, no globals. Everything it needs is in its arguments.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/modelcat/ -race -count=1`

- [ ] **Step 5: Commit**

---

### Task 2: Apply the choice before each turn

**Files:**
- Modify: `desktop/deps.go` (consult `Next` at turn start)
- Modify: `internal/serve/catalogue.go` if a helper is needed
- Test: `desktop/select_integration_test.go`

The desktop app builds its `SessionTurn` in `desktop/deps.go`. Before a turn begins, load settings and the odometer, build the catalogue, call `modelcat.Next`, and when it returns a different model, set `client.Model` accordingly.

**Switching happens between turns, never inside one.** A turn's context is built for one model's token budget and tool dialect; swapping mid-turn would split a conversation across two backends. Evaluate once, before the turn starts.

When a switch happens, emit an SSE frame so the UI can say so:
```
event: model-switched
data: {"profile":"...","model":"...","reason":"..."}
```

- [ ] **Step 1: Write the failing test**

An integration test that starts the embedded server with a stubbed LLM (follow `desktop/approval_test.go`'s pattern), seeds an odometer whose current model is over the threshold, sets `CycleOnCapacity`, runs a turn, and asserts a `model-switched` frame arrives naming the expected model. Also assert that with `CycleOnCapacity` false, no frame arrives.

- [ ] **Step 2-4:** fail, implement, pass.

- [ ] **Step 5: Commit**

---

### Task 3: The dropdown and filters

**Files:**
- Create: `desktop/frontend/src/components/ModelPicker.tsx`
- Modify: `desktop/frontend/src/api/client.ts` (catalogue + settings calls)
- Modify: `desktop/frontend/src/App.tsx` (mount it; do not grow it)

`App.tsx` is already 381 lines carrying chat and approvals. Put the picker in its own component and keep `App.tsx`'s growth to mounting it and holding the shared state it needs.

**The dropdown row** shows name, provider and remaining allowance:
```
gpt-oss-120b          Groq            312/1,000 RPD
qwen3.6-35b-a3b       your endpoint   4,506 tokens
command-a-03-2025     Cohere          needs a key     non-commercial
```
A model with no published quota shows its bare count, never a fraction.

**Four filters**, defaulting from `Settings`:
1. Reachability, defaulting to "usable now" so the list opens short
2. Has capacity left
3. Terms
4. Provider and modality

**Links:** each row links its provider homepage, and its model page when one exists. Render nothing rather than a dead link when `model_url` is empty.

- [ ] Steps: write component tests first if a test runner is configured; otherwise type-check and verify by running the app and reading the rendered list. State in your report which you did and why.

---

### Task 4: The settings panel

**Files:**
- Create: `desktop/frontend/src/components/SettingsPanel.tsx`
- Modify: `desktop/frontend/src/App.tsx` (mount)

Every row in the spec's settings inventory, all persisted through `PATCH /models/settings`:

| Control | Notes |
|---|---|
| Preference order | Drag to reorder, or up/down buttons if drag proves fiddly. Say which you shipped. |
| Cycle automatically | Toggle |
| Capacity threshold | Number or slider, 50-99, default 90 |
| When everything is full | "stay" or "ask" |
| Default filters | Reachability, has-capacity |
| Excluded terms | Checkboxes for non-commercial, trains on prompts, identity check |
| Custom links | Per model and per provider, with inline validation |
| Endpoint | Embedded or remote, from the desktop spec |

**Two properties the panel must have:**

- **An exclusion is enforced where selection happens**, not merely hidden in the list. Task 1 rule 1 already does this; the panel must make clear that excluding a term removes those providers from automatic cycling too, not just from view.
- **An unreachable model states its remedy**, naming the exact variable (for example `GOPHERMIND_PROFILE_FREE_GROQ_API_KEY`), so the panel doubles as instructions for widening what is reachable.

**Custom link validation:** the server returns 400 with the offending scheme for a `javascript:` or `file:` URL. Show that message inline; do not swallow it, and do not re-implement the check only on the client, where it could be bypassed.

- [ ] Steps as Task 3.

---

### Task 5: Build, install, and look at it

- [ ] `go build ./... && go test ./... -race` green.
- [ ] `cd desktop && wails build -platform darwin/arm64`
- [ ] Install to `/Applications/GopherMind Desktop.app`. **Do NOT touch `/Applications/Gophermind.app`**, which is the user's separate private AppleScript launcher.
- [ ] Launch it, open the dropdown, and confirm: the list is short by default, a reachable model shows a real allowance, an unreachable one names its variable, and the settings panel persists a change across a restart.
- [ ] Report what you actually saw, not what should happen. Screenshot-level detail in words.

## Final verification

- [ ] The three pinned desktop tests pass unchanged.
- [ ] An excluded term keeps a provider out of `Next`'s result even when it is first in the preference order.
- [ ] A model with no published quota never shows a fraction and is never switched away from.
- [ ] A `javascript:` custom link is refused by the server with 400 and the UI shows why.
- [ ] `go.mod`/`go.sum` unchanged; no new npm dependency.
- [ ] No em dashes on added lines.
