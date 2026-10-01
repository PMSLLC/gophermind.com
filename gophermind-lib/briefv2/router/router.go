// Package router sends every model call down a fallback chain: it picks the
// tier's models in order, skips the ones that are cooling down, disabled, too
// small, or not allowed to see the call (the privacy rule), retries transient
// failures, and writes exactly one ledger row per attempt.
package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

type Tier string

const (
	TierStrong   Tier = "strong"
	TierStandard Tier = "standard"
	TierAny      Tier = "any"
)

// Scope is the widest thing a call carries. The stage fixes it in code; a
// model never chooses it.
type Scope string

const (
	ScopeBrief     Scope = "brief"     // the whole brief
	ScopeComponent Scope = "component" // one component's slice of the brief
	ScopeNode      Scope = "node"      // one function's contract, signatures and tests
)

// CallInfo says who is asking and what the call carries.
type CallInfo struct {
	RunID    string
	Stage    string // clarify, contract, decompose:<component>, coverage, coverage_fill, testwrite:<node>, revise:<node>, implement:<node>
	NodeID   string // empty for run-level stages
	Tier     Tier
	Scope    Scope
	Revision int
	// TaskType is the kind of work: clarify, contract, decompose, coverage,
	// testwrite, revise, implement. Left empty it is the stage up to its first
	// colon, so every ledger row has one.
	TaskType string
	// NodeClass is, for a call about one leaf, the class of function: pure,
	// validation, handler, client, storage, concurrency, wiring, other.
	NodeClass string
	Exclude   []string // provider/model entries to skip, used after a malformed reply
	Only      string   // if set, try only this provider/model entry
}

// Result is a successful reply plus where it came from.
type Result struct {
	provider.Response
	CallID   int64  // the ledger row this attempt wrote (0 if the ledger write failed)
	Entry    string // provider/model that answered
	ChainPos int    // 1-based position in the tier's chain
}

type Option func(*Router)

// WithAllowPublic lets public providers see brief and component scope calls
// under privacy.mode need_to_know. It never overrides private_only.
func WithAllowPublic(allow bool) Option { return func(r *Router) { r.allowPublic = allow } }

// WithSleep replaces the waiting function (backoff and cooldown waits), so
// tests run instantly.
func WithSleep(fn func(context.Context, time.Duration) error) Option {
	return func(r *Router) { r.sleep = fn }
}

// WithNow replaces the clock used for cooldowns and timestamps.
func WithNow(fn func() time.Time) Option { return func(r *Router) { r.now = fn } }

const transientRetries = 3

type Router struct {
	cfg         *settings.Config
	providers   map[string]provider.Provider
	led         ledger.Ledger
	sink        events.Sink
	allowPublic bool
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time
	slots       map[string]chan struct{}

	mu         sync.Mutex
	cooldown   map[string]time.Time // provider -> earliest next use
	disabled   map[string]bool      // provider -> auth failed this run
	missing    map[string]bool      // provider/model -> not found this run
	ledgerErrs int
}

// New builds a router over already-constructed providers.
func New(cfg *settings.Config, providers map[string]provider.Provider, led ledger.Ledger, sink events.Sink, opts ...Option) *Router {
	if sink == nil {
		sink = events.Nop
	}
	r := &Router{
		cfg: cfg, providers: providers, led: led, sink: sink,
		sleep:    sleepCtx,
		now:      time.Now,
		slots:    map[string]chan struct{}{},
		cooldown: map[string]time.Time{},
		disabled: map[string]bool{},
		missing:  map[string]bool{},
	}
	for _, p := range cfg.Providers {
		r.slots[p.Name] = make(chan struct{}, p.MaxConcurrent)
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LedgerErrors counts ledger writes that failed. A run with a non-zero count
// is marked incomplete in its status; the model results were still used.
func (r *Router) LedgerErrors() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ledgerErrs
}

// promptDigest is the size and hash of the prompt text, the only things the
// ledger ever stores about it.
type promptDigest struct {
	bytes  int
	sha256 string
	tokens int // estimated: bytes/4 plus 10 percent
}

func digestPrompt(req provider.Request) promptDigest {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	n, sum := ledger.Digest([]byte(b.String()))
	return promptDigest{bytes: n, sha256: sum, tokens: n/4 + n/40}
}

// Call walks the tier's chain until one entry answers.
func (r *Router) Call(ctx context.Context, info CallInfo, req provider.Request) (Result, error) {
	chain, err := r.chainFor(info)
	if err != nil {
		return Result{}, err
	}
	pd := digestPrompt(req)
	maxWait := time.Duration(r.cfg.Defaults.MaxWaitMinutes) * time.Minute
	var waited time.Duration
	recordedTooLong := map[string]bool{}

	for {
		var reasons []EntryReason
		for i, entry := range chain {
			if info.Only != "" && entry != info.Only {
				continue
			}
			provName, model, _ := settings.SplitEntry(entry)
			if reason, skip := r.gate(info, entry, provName); skip {
				reasons = append(reasons, reason)
				continue
			}
			mi, _ := r.cfg.ModelInfo(entry)
			if pd.tokens+req.MaxTokens > mi.ContextTokens {
				if !recordedTooLong[entry] {
					recordedTooLong[entry] = true
					r.record(ctx, r.newRow(info, i+1, provName, model, pd, ledger.OutcomeTooLong, "too_long"))
				}
				reasons = append(reasons, EntryReason{Entry: entry, Kind: ReasonTooLong,
					Detail: fmt.Sprintf("about %d tokens plus %d reserved, model holds %d", pd.tokens, req.MaxTokens, mi.ContextTokens)})
				continue
			}
			res, reason, answered, err := r.attempt(ctx, info, req, entry, i+1, provName, model, pd)
			if err != nil {
				return Result{}, err
			}
			if answered {
				return res, nil
			}
			reasons = append(reasons, reason)
		}

		wait, cooling := r.shortestCooldown(reasons)
		if !cooling || waited+wait > maxWait {
			return Result{}, &ChainExhausted{Tier: info.Tier, Reasons: reasons}
		}
		if wait > 0 {
			r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
				Message: fmt.Sprintf("every model is cooling down; waiting %s", wait.Round(time.Second))})
			if err := r.sleep(ctx, wait); err != nil {
				return Result{}, err
			}
			waited += wait
		}
	}
}

// chainFor validates the call's tier, scope and identity and returns the chain.
func (r *Router) chainFor(info CallInfo) ([]string, error) {
	if info.RunID == "" || info.Stage == "" {
		return nil, errors.New("router: RunID and Stage are required")
	}
	switch info.Scope {
	case ScopeBrief, ScopeComponent, ScopeNode:
	default:
		return nil, fmt.Errorf("router: unknown scope %q (a call must declare what it carries)", info.Scope)
	}
	chain, ok := r.cfg.Models[string(info.Tier)]
	if !ok || len(chain) == 0 {
		return nil, fmt.Errorf("router: unknown tier %q", info.Tier)
	}
	return chain, nil
}

// gate reports why an entry must not be tried right now, if it must not.
func (r *Router) gate(info CallInfo, entry, provName string) (EntryReason, bool) {
	for _, x := range info.Exclude {
		if x == entry {
			return EntryReason{Entry: entry, Kind: ReasonExcluded, Detail: "its last reply was unusable"}, true
		}
	}
	if r.providers[provName] == nil {
		return EntryReason{Entry: entry, Kind: ReasonNoProvider}, true
	}
	r.mu.Lock()
	missing, disabled, until := r.missing[entry], r.disabled[provName], r.cooldownUntil(entry, provName)
	r.mu.Unlock()
	switch {
	case missing:
		return EntryReason{Entry: entry, Kind: ReasonModelMissing, Detail: "not found earlier in this run"}, true
	case disabled:
		return EntryReason{Entry: entry, Kind: ReasonAuth, Detail: "authentication failed earlier in this run"}, true
	case !r.eligible(provName, info.Scope):
		return EntryReason{Entry: entry, Kind: ReasonPrivacy,
			Detail: fmt.Sprintf("a public provider may not see %s scope under privacy.mode %s", info.Scope, r.cfg.Privacy.Mode)}, true
	case r.now().Before(until):
		return EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: "until " + until.UTC().Format(time.RFC3339)}, true
	}
	return EntryReason{}, false
}

// shortestCooldown returns how long until the first cooling provider in
// reasons is usable again. cooling is false when nothing is merely waiting.
func (r *Router) shortestCooldown(reasons []EntryReason) (wait time.Duration, cooling bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range reasons {
		if x.Kind != ReasonCooldown {
			continue
		}
		provName, _, _ := settings.SplitEntry(x.Entry)
		d := r.cooldownUntil(x.Entry, provName).Sub(r.now())
		if d < 0 {
			d = 0
		}
		if !cooling || d < wait {
			wait, cooling = d, true
		}
	}
	return wait, cooling
}

// setCooldown cools key down for d. A key is a provider name (rate limits and
// repeated failures) or a "provider/model" entry (a timeout); provider names
// hold no slash, so the two never collide.
func (r *Router) setCooldown(key string, d time.Duration) {
	r.mu.Lock()
	r.cooldown[key] = r.now().Add(d)
	r.mu.Unlock()
}

// cooldownUntil is the later of an entry's own cooldown and its provider's.
// The caller holds r.mu.
func (r *Router) cooldownUntil(entry, provName string) time.Time {
	a, b := r.cooldown[entry], r.cooldown[provName]
	if a.After(b) {
		return a
	}
	return b
}

// timeoutCooldown is how long an entry is skipped after its call timed out.
func (r *Router) timeoutCooldown() time.Duration {
	s := r.cfg.RateLimits.CooldownAfterTimeoutSeconds
	if s < 1 {
		s = 60
	}
	return time.Duration(s) * time.Second
}

func (r *Router) defaultCooldown() time.Duration {
	return time.Duration(r.cfg.RateLimits.CooldownAfter429Seconds) * time.Second
}

func (r *Router) emit(e events.Event) {
	if e.At.IsZero() {
		e.At = r.now().UTC()
	}
	r.sink.Emit(e)
}

// newRow starts a ledger row for an attempt on one entry.
func (r *Router) newRow(info CallInfo, pos int, provName, model string, pd promptDigest, o ledger.Outcome, errorKind string) *ledger.Call {
	taskType := info.TaskType
	if taskType == "" {
		taskType, _, _ = strings.Cut(info.Stage, ":")
	}
	return &ledger.Call{
		RunID: info.RunID, At: r.now().UTC(), Stage: info.Stage, NodeID: info.NodeID, Revision: info.Revision,
		TaskType: taskType, NodeClass: info.NodeClass,
		Scope: string(info.Scope), Tier: string(info.Tier), ChainPos: pos,
		Provider: provName, ModelRequested: model,
		PromptBytes: pd.bytes, PromptSHA256: pd.sha256,
		Outcome: o, ErrorKind: errorKind,
	}
}

// record writes a row. A failed write never fails the call: it is reported as
// a ledger_error event and counted, and the run is marked incomplete.
func (r *Router) record(ctx context.Context, row *ledger.Call) {
	if r.led == nil {
		return
	}
	// A cancelled call still gets its row, so write with a context that survives the cancel.
	if err := r.led.Record(context.WithoutCancel(ctx), row); err != nil {
		r.noteLedgerError(row.Stage, row.NodeID, err)
		return
	}
	cp := *row
	r.emit(events.Event{Kind: events.KindCall, Stage: row.Stage, NodeID: row.NodeID, Call: &cp})
}

func (r *Router) noteLedgerError(stage, nodeID string, err error) {
	r.mu.Lock()
	r.ledgerErrs++
	r.mu.Unlock()
	r.emit(events.Event{Kind: events.KindLedgerError, Stage: stage, NodeID: nodeID, Message: err.Error()})
}
