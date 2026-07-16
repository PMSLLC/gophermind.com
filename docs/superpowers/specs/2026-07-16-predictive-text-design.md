# Predictive Text for the gophermind TUI — Design

**Date:** 2026-07-16
**Status:** Approved design — pending implementation plan
**Author:** John Brahy (with Claude)

## Summary

Add predictive text to the gophermind TUI input. As the user types in the
Bubble Tea `textarea`, the app offers completions through a **hybrid** surface:
a greyed inline **ghost text** when there is one obvious continuation, or a small
**popup menu** when there are several candidates. Completions come from pluggable
**providers**: slash-command completion, file/path completion, whole-prompt
history recall, and a **Markov (n-gram) next-word predictor**. A later phase adds
an LLM-backed provider behind the same interface.

The completion engine is built as a **separate, reusable Go module**
(`github.com/jbrahy/bubblecomplete`) that knows nothing about gophermind, so
other Bubble Tea apps can use it. gophermind supplies its own providers on top.

## Goals

- Real-time suggestions in the TUI input with zero perceptible lag for the
  deterministic providers.
- Hybrid presentation: ghost text for a single continuation, popup menu for many.
- A clean, dependency-light, reusable library core.
- Deterministic-first: ship value with no LLM, no tokens, no network.
- An interface that the Phase-2 LLM provider slots into without refactoring.

## Non-Goals (YAGNI)

- The LLM predictor itself (Phase 2).
- Fuzzy matching — Phase 1 is prefix-based only.
- A seeded base-English corpus for the Markov model — it trains on the user's
  own prompt history only.
- Config UI beyond a single on/off toggle.
- Menu scrolling beyond a fixed visible height.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Architecture | Standalone completion controller beside a vanilla `textarea` (Approach A) |
| Packaging | Separate Go module, nested in this repo, wired via replace directive (+ go.work for dev) |
| Module path | `github.com/jbrahy/bubblecomplete`, dir `./bubblecomplete/` |
| Presentation | Hybrid — ghost text (1 candidate) / popup menu (many) |
| Build order | Deterministic providers in Phase 1; LLM in Phase 2 |
| History source | Persisted to disk from day one |
| Markov predictor | Phase 1, alongside whole-prompt recall |
| Input | Multi-line, auto-grows 1→4 visible lines, then scrolls internally |
| Submit vs newline | Enter submits; Shift+Enter (fallback Alt+Enter/Ctrl+J) inserts a newline |

## Architecture

Two layers.

### Layer 1 — `github.com/jbrahy/bubblecomplete` (reusable core)

A new module in `./bubblecomplete/` with its own `go.mod`. Imports only
`github.com/charmbracelet/bubbletea` and `github.com/charmbracelet/lipgloss`.
Contains no gophermind-specific code. Wired into gophermind during development
via a repo-root `go.work` file.

**Types & interface:**

```go
// Candidate is one suggested completion.
type Candidate struct {
    Text    string // text to insert
    Display string // label shown in a menu (defaults to Text)
    Desc    string // optional right-column description in a menu
    Replace int    // runes to delete before the cursor before inserting Text
}

// Provider produces candidates for the current input state.
type Provider interface {
    Name() string
    Suggest(input string, cursor int) []Candidate
}
```

**`Model` (Bubble Tea sub-model):**

- Holds an ordered `[]Provider`. On each query it walks providers in priority
  order and takes the first provider that returns a non-empty result.
- **Hybrid decision:** if the winning result is a single candidate that extends
  the current token → render as **ghost text**; if it has multiple candidates →
  render a **popup menu** above the input line.
- Public surface:
  - `New(opts ...Option) Model`
  - `SetProviders(...Provider)`
  - `Query(input string, cursor int) Model` — recompute suggestions (host calls
    this after the input value changes).
  - `Update(msg tea.Msg) (Model, Result)` — consumes navigation/accept keys
    when active. `Result{Accepted *Candidate; Consumed bool}`: `Accepted` is
    non-nil when the user just accepted a suggestion; `Consumed` tells the host
    whether the completion model handled the key (if true, the host must not
    also process it).
  - `Accept() (Model, *Candidate)` — host-driven accept (e.g. Enter with a menu
    open); returns nil if nothing is active.
  - `View() string` — the popup **menu** block only (`ModeMenu`); `""` in every
    other mode.
  - `Ghost() string` — the inline ghost continuation text (`ModeGhost`); `""`
    otherwise. Rendering it inline is the host's job (drawn inside the host's
    own input widget); this library only supplies the string.
  - `GhostStyle() lipgloss.Style` — the faint style the host uses to render the
    inline ghost text.
  - `Active() bool` — `Mode() != ModeNone`.
  - `Mode() Mode` — `ModeNone` / `ModeGhost` / `ModeMenu`.
- **Keys consumed when active:** Tab (accept ghost / accept highlighted menu
  item), → (accept ghost text when cursor is at end of line), ↑/↓ (move within an
  open menu), Esc (dismiss the current suggestion). All other keys pass through,
  and Enter / Shift+Enter / Alt+Enter / Ctrl+J are never consumed.
- Styling is injected via options (ghost color, menu border/selected styles) so
  the host controls theming; sensible defaults provided.

**Optional helper subpackage — `bubblecomplete/ngram`:**

A generic Markov (n-gram) text model, dependency-free (stdlib only), usable
independently of the `Model`.

- `Model` trained by feeding lines of text (`Train(line string)` /
  `TrainAll([]string)`).
- Trigram counts with **backoff**: trigram → bigram → unigram.
- `Predict(prefixWords []string) (word string, ok bool)` returns the highest-
  probability next word, subject to a **minimum-count threshold** (stays silent
  rather than guessing from a single observation).
- No persistence of its own; the host trains it from whatever corpus it holds.

**Tests:** table-driven unit tests for the hybrid decision, key routing, and the
n-gram backoff/threshold logic. A tiny `example/` `main` demonstrates reuse in a
standalone Bubble Tea program.

### Layer 2 — gophermind wiring (`internal/tui/…`)

gophermind implements `bubblecomplete.Provider` for its four Phase-1 providers
and owns the data they need.

**Providers (priority order, highest first):**

1. **command** — active only when the line starts with `/`. Prefix-matches a new
   **command registry** and yields a menu (`Display` = command, `Desc` = help).
2. **file** — active when the token under the cursor is path-shaped (`/`, `./`,
   `~/`, or a bare filename fragment). Globs the working directory and yields a
   menu. Directories get a trailing `/`.
3. **recall** — whole-prompt history recall: the most-recent past prompt whose
   text has the current input as a prefix. Single ghost candidate. Wins over the
   Markov predictor when it matches (high confidence).
4. **markov** — the `bubblecomplete/ngram` model trained on the prompt history;
   predicts the next word from the last one or two typed words. Single ghost
   candidate. Fills mid-sentence continuations that recall can't.

**Command registry** — a new small source of truth (`internal/tui/commands`
region or file) holding `{name, desc}` for every slash command
(`/help /clear /project /phase /config /temp /topp /generate /exit /quit`). The
ad-hoc string matches in `update.go` and the hand-written `/help` line are
refactored to read from this registry, so completion and help never drift.

**History store** — a new component that:

- Appends each submitted prompt (from `handleSubmit`) to a history file.
- File location: `<os.UserConfigDir>/gophermind/history`, falling back to
  `.gophermind/history` under the project root (mirrors the existing cache-dir
  convention in `internal/config`).
- Caps the file at ~500 lines (drop oldest) and skips consecutive duplicates.
- Loads existing history at startup to seed both the recall provider and the
  Markov model's training corpus.
- **Privacy note:** prompts are written to disk in plain text. Documented in the
  README/CHANGELOG; a config toggle can disable history entirely.

## Multi-line auto-growing input

The input changes from a fixed single line to a multi-line `textarea` that grows
with its content and then scrolls.

- **Auto-grow:** after any change to the input value, the textarea height is set
  to `clamp(lineCount, 1, 4)` visible rows. At 1–4 wrapped lines it grows; beyond
  4 it stays at 4 and the textarea scrolls its own content (native bubbles
  behavior). Line count is measured on *wrapped* lines at the current width, not
  just literal `\n`, so long single lines that wrap also grow the box.
- **Layout:** `inputHeight` becomes dynamic — `currentInputRows + border (2)` —
  instead of the hard-coded `3`. The viewport height is recomputed from it on
  every resize and on every input-height change, so the transcript always fills
  the remaining space. When the input shrinks, the viewport reclaims the rows.
- **Submit vs newline:** Enter submits the prompt (as today). Shift+Enter inserts
  a newline; because some terminals do not report Shift+Enter distinctly,
  Alt+Enter and Ctrl+J are accepted as newline fallbacks. On submit, the input
  resets to a single row.
- **Completion overlay:** the menu/ghost overlay positions relative to the
  input's *current* top edge, so it stays correct as the box grows.

## Integration into the TUI

- `model` (in `internal/tui/model.go`) gains:
  - `complete bubblecomplete.Model`
  - `history *history.Store` (or equivalent)
- `newModel` constructs the store, loads history, trains the n-gram model, builds
  the four providers, and installs them on the completion model.
- `handleKey` (in `internal/tui/update.go`) gives the completion model **first
  refusal**: route the `tea.KeyMsg` to `m.complete.Update`; if it returns an
  accepted `*Candidate`, apply it to the textarea (delete `Replace` runes,
  insert `Text`) and re-query; if it consumed the key, stop; otherwise fall
  through to the existing textarea/submit handling. After any key that mutates
  the input value, call `m.complete.Query(value, cursor)`.
- `handleSubmit` pushes the submitted prompt to the history store (and updates
  the in-memory recall list + n-gram model incrementally).
- `view.go` overlays `m.complete.View()` above the input line when active.

**Interaction guard rails:**

- Suggestions are suppressed while `st != stateIdle` (working/approval states) so
  they never fight the approval `y/n/a` keys or a streaming turn.
- Enter with a menu open **accepts** the highlighted item; Enter with no active
  suggestion **submits**. Shift+Enter (fallback Alt+Enter/Ctrl+J) always inserts
  a newline and is never intercepted by the completion model.
- Esc dismisses an active suggestion first; a second Esc (or Esc with nothing
  active) keeps its current cancel/interrupt meaning.

## Phase 2 (designed-for, not built)

An `llm` provider implementing `bubblecomplete.Provider`: debounced and
cancellable, it calls the model for a short continuation and emits a single
ghost candidate. It plugs in below `markov` in priority (or above, by config)
with **no** changes to the controller or the other providers.

## Testing Strategy

- **bubblecomplete:** unit tests for the single-vs-menu decision, key routing
  (Tab/→/↑↓/Esc/passthrough), and ngram backoff + threshold. Example program
  compiles and runs.
- **gophermind providers:** table-driven unit tests per provider (command prefix
  matching against the registry, file globbing in a temp dir, recall prefix
  logic, markov end-to-end on a small corpus).
- **history store:** round-trip load/append/cap/dedupe against a temp file.
- **TUI:** extend the existing `update_test.go` / `e2e_test.go` patterns to cover
  key routing precedence (suggestion vs approval vs submit), that history is
  recorded on submit, input auto-grow/shrink between 1 and 4 rows with viewport
  recompute, and Shift+Enter (and fallbacks) inserting a newline while Enter
  submits.
- Full gate before done: `go test ./...` (both modules), `go vet`, `gofmt`.

## Rollout / Phasing

1. **bubblecomplete module** — types, `Model`, hybrid render, key handling,
   `ngram` helper, tests, example. Wire `go.work`.
2. **gophermind Phase 1** — multi-line auto-growing input (1→4 rows + scroll,
   Enter/Shift+Enter), command registry refactor, history store, four providers,
   TUI integration, tests, docs (README/CHANGELOG + privacy note).
3. **Phase 2 (separate spec)** — LLM provider.
