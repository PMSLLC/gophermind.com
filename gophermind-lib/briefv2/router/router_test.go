package router_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/settings"
)

// clock is a fake clock: sleeping advances it and records the request.
type clock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}
func (c *clock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return nil
}
func (c *clock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// testConfig has one private provider (mini) and two public ones (kilo, ovh).
func testConfig() *settings.Config {
	c := settings.Default()
	c.Providers = []settings.ProviderConfig{
		{Name: "mini", BaseURL: "http://mini", Visibility: settings.Private, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "qwen", ContextTokens: 1000}}},
		{Name: "kilo", BaseURL: "http://kilo", Visibility: settings.Public, MaxConcurrent: 2, Models: []settings.ModelEntry{{ID: "auto", ContextTokens: 100000}}},
		{Name: "ovh", BaseURL: "http://ovh", Visibility: settings.Public, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "q27", ContextTokens: 100000}}},
	}
	c.Models = map[string][]string{
		"strong":   {"mini/qwen"},
		"standard": {"mini/qwen", "kilo/auto"},
		"any":      {"kilo/auto", "ovh/q27", "mini/qwen"},
	}
	c.Defaults.CallTimeout = 2 * time.Second
	c.Defaults.MaxWaitMinutes = 5
	return c
}

type script = func(call int, req provider.Request) (provider.Response, error)

func okText(text string) script {
	return func(_ int, req provider.Request) (provider.Response, error) {
		return provider.Response{Text: text, Model: req.Model, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
	}
}

type rig struct {
	r               *router.Router
	led             ledger.Ledger
	runDir          string
	sink            *events.Collector
	clk             *clock
	mini, kilo, ovh *provider.Fake
}

// newRig wires a router over three fakes and a real file ledger in a temp run folder.
// A nil script answers "ok".
func newRig(t *testing.T, mini, kilo, ovh script, mutate func(*settings.Config), opts ...router.Option) *rig {
	t.Helper()
	cfg := testConfig()
	if mutate != nil {
		mutate(cfg)
	}
	for _, s := range []*script{&mini, &kilo, &ovh} {
		if *s == nil {
			*s = okText("ok")
		}
	}
	g := &rig{
		mini: provider.NewFake("mini", []provider.ModelInfo{{ID: "qwen", ContextTokens: 1000}}, mini),
		kilo: provider.NewFake("kilo", []provider.ModelInfo{{ID: "auto", ContextTokens: 100000}}, kilo),
		ovh:  provider.NewFake("ovh", []provider.ModelInfo{{ID: "q27", ContextTokens: 100000}}, ovh),
	}
	g.build(t, cfg, map[string]provider.Provider{"mini": g.mini, "kilo": g.kilo, "ovh": g.ovh}, nil, opts...)
	return g
}

// build finishes a rig: real ledger unless ledOverride is given.
func (g *rig) build(t *testing.T, cfg *settings.Config, providers map[string]provider.Provider, ledOverride ledger.Ledger, opts ...router.Option) {
	t.Helper()
	g.runDir = t.TempDir()
	g.led = ledger.NewFS(runfs.Fixed(g.runDir))
	g.sink, g.clk = events.NewCollector(), newClock()
	var led ledger.Ledger = g.led
	if ledOverride != nil {
		led = ledOverride
	}
	all := append([]router.Option{router.WithNow(g.clk.now), router.WithSleep(g.clk.sleep)}, opts...)
	g.r = router.New(cfg, providers, led, g.sink, all...)
}

func info(tier router.Tier, scope router.Scope) router.CallInfo {
	return router.CallInfo{RunID: "r", Stage: "testwrite:fn-a", NodeID: "fn-a", Tier: tier, Scope: scope}
}

func req(text string) provider.Request {
	return provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: text}}}
}

func (g *rig) rows(t *testing.T) []ledger.Call {
	t.Helper()
	rows, err := g.led.List(context.Background(), "r", ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestFailsOverInChainOrderAndWritesOneRowPerAttempt(t *testing.T) {
	g := newRig(t, nil,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		},
		okText("from ovh"), nil)
	res, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "from ovh" || res.Entry != "ovh/q27" || res.ChainPos != 2 {
		t.Errorf("result = %+v", res)
	}
	if g.kilo.Calls() != 1 || g.ovh.Calls() != 1 || g.mini.Calls() != 0 {
		t.Errorf("calls: kilo %d ovh %d mini %d", g.kilo.Calls(), g.ovh.Calls(), g.mini.Calls())
	}
	rows := g.rows(t)
	if len(rows) != 2 {
		t.Fatalf("%d ledger rows, want 2", len(rows))
	}
	if rows[0].Provider != "kilo" || rows[0].Outcome != ledger.OutcomeRateLimited || rows[0].RetryAfterS != 30 || rows[0].ChainPos != 1 {
		t.Errorf("first row = %+v", rows[0])
	}
	if rows[1].Provider != "ovh" || rows[1].Outcome != ledger.OutcomeOK || rows[1].ModelServed != "q27" || rows[1].PromptTokens != 10 ||
		rows[1].Scope != "node" || rows[1].Tier != "any" || rows[1].Stage != "testwrite:fn-a" || rows[1].NodeID != "fn-a" {
		t.Errorf("second row = %+v", rows[1])
	}
	if res.CallID != rows[1].ID {
		t.Errorf("CallID %d is not the answering row %d", res.CallID, rows[1].ID)
	}
	if n := len(g.sink.OfKind(events.KindCall)); n != 2 {
		t.Errorf("%d call events, want 2", n)
	}
}

func TestCooldownHoldsAndThenExpires(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		}
		return provider.Response{Text: "kilo", Model: r.Model}, nil
	}, nil, nil)
	ctx := context.Background()
	call := func() router.Result {
		res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(); res.Entry != "ovh/q27" {
		t.Fatalf("first call answered by %s", res.Entry)
	}
	g.clk.advance(10 * time.Second)
	if res := call(); res.Entry != "ovh/q27" || g.kilo.Calls() != 1 {
		t.Errorf("during the cooldown: %s, kilo calls %d", res.Entry, g.kilo.Calls())
	}
	g.clk.advance(21 * time.Second)
	if res := call(); res.Entry != "kilo/auto" || g.kilo.Calls() != 2 {
		t.Errorf("after the cooldown: %s, kilo calls %d", res.Entry, g.kilo.Calls())
	}
}

func TestRateLimitWithoutRetryAfterUsesTheConfiguredCooldown(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{}
		}
		return provider.Response{Text: "kilo", Model: r.Model}, nil
	}, nil, nil) // cooldown_after_429_seconds is 60
	ctx := context.Background()
	g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	g.clk.advance(59 * time.Second)
	if res, _ := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi")); res.Entry != "ovh/q27" {
		t.Errorf("at 59s the provider should still be cooling, got %s", res.Entry)
	}
	g.clk.advance(2 * time.Second)
	if res, _ := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi")); res.Entry != "kilo/auto" {
		t.Errorf("at 61s the provider should be back, got %s", res.Entry)
	}
}

func TestAuthFailureDisablesTheProviderForTheRunAndWarnsOnce(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrAuth{Provider: "kilo"}
	}, nil, nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
		if err != nil || res.Entry != "ovh/q27" {
			t.Fatalf("call %d: %+v %v", i, res, err)
		}
	}
	if g.kilo.Calls() != 1 {
		t.Errorf("kilo was called %d times after a 401, want 1", g.kilo.Calls())
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings, want exactly 1", n)
	}
	var auth int
	for _, row := range g.rows(t) {
		if row.Outcome == ledger.OutcomeAuth {
			auth++
		}
	}
	if auth != 1 {
		t.Errorf("%d auth rows, want 1", auth)
	}
}

func TestModelNotFoundSkipsThatEntryForTheRestOfTheRun(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrModelNotFound{Model: "qwen"}
	}, nil, nil, nil)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		res, err := g.r.Call(ctx, info(router.TierStandard, router.ScopeNode), req("hi"))
		if err != nil || res.Entry != "kilo/auto" {
			t.Fatalf("call %d: %+v %v", i, res, err)
		}
	}
	if g.mini.Calls() != 1 {
		t.Errorf("mini was asked %d times after a 404, want 1", g.mini.Calls())
	}
	rows := g.rows(t)
	if rows[0].Outcome != ledger.OutcomeModelMissing {
		t.Errorf("first row = %+v", rows[0])
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings, want 1", n)
	}
}

func TestTransientFailuresRetryWithBackoffThenMoveOn(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
	}, nil, nil)
	ctx := context.Background()
	res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil || res.Entry != "ovh/q27" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.kilo.Calls() != 4 {
		t.Errorf("kilo calls = %d, want 1 try plus 3 retries", g.kilo.Calls())
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	if got := g.clk.sleeps(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("backoff sleeps = %v, want %v", got, want)
	}
	rows := g.rows(t)
	if len(rows) != 5 {
		t.Fatalf("%d rows, want 4 failed attempts and 1 answer", len(rows))
	}
	for _, row := range rows[:4] {
		if row.Provider != "kilo" || row.Outcome != ledger.OutcomeError || row.ErrorKind != "transient" {
			t.Errorf("row = %+v", row)
		}
	}
	// After giving up it is treated like a rate limit: the provider rests.
	g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	if g.kilo.Calls() != 4 {
		t.Errorf("kilo was tried again during its cooldown (%d calls)", g.kilo.Calls())
	}
}

func TestTransientFailureThatRecoversAnswersFromTheSameModel(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call < 3 {
			return provider.Response{}, provider.ErrTransient{Cause: errors.New("reset")}
		}
		return provider.Response{Text: "third time", Model: r.Model}, nil
	}, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil || res.Entry != "kilo/auto" || res.Text != "third time" || g.kilo.Calls() != 3 {
		t.Fatalf("%+v %v calls=%d", res, err, g.kilo.Calls())
	}
	if len(g.rows(t)) != 3 {
		t.Errorf("%d rows, want 3", len(g.rows(t)))
	}
}

func TestBackoffIsCappedAtTheConfiguredMaximum(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
	}, nil, func(c *settings.Config) { c.RateLimits.BackoffMaxSeconds = 8 })
	g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	got := g.clk.sleeps()
	if len(got) != 3 || got[0] != 5*time.Second || got[1] != 8*time.Second || got[2] != 8*time.Second {
		t.Errorf("sleeps = %v, want [5s 8s 8s]", got)
	}
}

func TestPromptTooLongIsSkippedAndRecordedWithoutCallingTheModel(t *testing.T) {
	cases := []struct {
		name      string
		promptLen int
		maxTokens int
		fits      bool
	}{
		{"small prompt", 100, 0, true},
		{"reserve pushes it over", 100, 990, false},
		{"reserve fits", 100, 900, true},
		{"huge prompt", 8000, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, nil, nil, nil, nil) // mini holds 1000 tokens
			r := req(strings.Repeat("x", c.promptLen))
			r.MaxTokens = c.maxTokens
			res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), r)
			if c.fits {
				if err != nil || res.Entry != "mini/qwen" {
					t.Fatalf("%+v %v", res, err)
				}
				return
			}
			var ce *router.ChainExhausted
			if !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonTooLong {
				t.Fatalf("err = %v, want too_long", err)
			}
			if g.mini.Calls() != 0 {
				t.Error("the model was called with a prompt that cannot fit")
			}
			rows := g.rows(t)
			if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeTooLong || rows[0].PromptBytes != c.promptLen+1 {
				t.Errorf("rows = %+v", rows)
			}
		})
	}
}

func TestBigPromptFallsThroughToALargerModel(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req(strings.Repeat("x", 8000)))
	if err != nil || res.Entry != "kilo/auto" || res.ChainPos != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 0 {
		t.Error("mini was called with a prompt larger than its window")
	}
}

func TestExhaustionExplainsEveryEntry(t *testing.T) {
	g := newRig(t,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrAuth{Provider: "mini"}
		},
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: time.Hour}
		}, nil, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.Tier != router.TierStandard || len(ce.Reasons) != 2 ||
		ce.Reasons[0].Entry != "mini/qwen" || ce.Reasons[0].Kind != router.ReasonAuth ||
		ce.Reasons[1].Entry != "kilo/auto" || ce.Reasons[1].Kind != router.ReasonCooldown {
		t.Errorf("reasons = %+v", ce.Reasons)
	}
	if msg := err.Error(); !strings.Contains(msg, "mini/qwen") || !strings.Contains(msg, "kilo/auto") || !strings.Contains(msg, "standard") {
		t.Errorf("message = %q", msg)
	}
	if len(g.clk.sleeps()) != 0 {
		t.Errorf("waited %v for a cooldown longer than max_wait_minutes", g.clk.sleeps())
	}
}

func TestWaitsWhenOnlyACooldownBlocks(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		}
		return provider.Response{Text: "after the wait", Model: r.Model}, nil
	}, nil, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	if err != nil || res.Text != "after the wait" {
		t.Fatalf("%+v %v", res, err)
	}
	if got := g.clk.sleeps(); len(got) != 1 || got[0] != 30*time.Second {
		t.Errorf("sleeps = %v, want [30s]", got)
	}
	if g.mini.Calls() != 2 || len(g.rows(t)) != 2 {
		t.Errorf("mini calls %d, rows %d; want 2 and 2", g.mini.Calls(), len(g.rows(t)))
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings about waiting, want 1", n)
	}
}

func TestStopsWaitingAtMaxWaitMinutes(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrRateLimited{RetryAfter: 4 * time.Minute}
	}, nil, nil, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	// One 4 minute wait fits in max_wait_minutes (5); a second one would not.
	if got := g.clk.sleeps(); len(got) != 1 || got[0] != 4*time.Minute {
		t.Errorf("sleeps = %v, want one 4m wait", got)
	}
	if g.mini.Calls() != 2 {
		t.Errorf("mini calls = %d, want 2", g.mini.Calls())
	}
}

func TestPublicProvidersAreOnlyEverAskedWhatThePrivacyRuleAllows(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		allowPublic bool
		scope       router.Scope
		publicOK    bool
	}{
		{"need_to_know, brief", "need_to_know", false, router.ScopeBrief, false},
		{"need_to_know, component", "need_to_know", false, router.ScopeComponent, false},
		{"need_to_know, node", "need_to_know", false, router.ScopeNode, true},
		{"allow-public, brief", "need_to_know", true, router.ScopeBrief, true},
		{"allow-public, component", "need_to_know", true, router.ScopeComponent, true},
		{"private_only, node", "private_only", false, router.ScopeNode, false},
		{"private_only, node, allow-public", "private_only", true, router.ScopeNode, false},
		{"private_only, brief, allow-public", "private_only", true, router.ScopeBrief, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, nil, nil, nil, func(cfg *settings.Config) { cfg.Privacy.Mode = c.mode },
				router.WithAllowPublic(c.allowPublic))
			res, err := g.r.Call(context.Background(), info(router.TierAny, c.scope), req("the whole brief"))
			if err != nil {
				t.Fatal(err)
			}
			public := g.kilo.Calls() + g.ovh.Calls()
			if c.publicOK {
				if public == 0 || res.Entry != "kilo/auto" {
					t.Errorf("public providers were allowed but were not used: entry %s, hits %d", res.Entry, public)
				}
				return
			}
			if public != 0 {
				t.Errorf("a public provider received %d request(s) it must never see", public)
			}
			if res.Entry != "mini/qwen" {
				t.Errorf("answered by %s, want the private mini", res.Entry)
			}
		})
	}
}

func TestEveryEntryFilteredByPrivacyIsReportedAsSuch(t *testing.T) {
	g := newRig(t, nil, nil, nil, func(c *settings.Config) { c.Models["any"] = []string{"kilo/auto", "ovh/q27"} })
	_, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeBrief), req("the whole brief"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || !ce.OnlyPrivacy() {
		t.Fatalf("err = %v, want a ChainExhausted where only privacy blocked", err)
	}
	if g.kilo.Calls()+g.ovh.Calls() != 0 {
		t.Error("a public provider was called")
	}
	if len(g.rows(t)) != 0 {
		t.Error("skipping for privacy is not an attempt and must not write a ledger row")
	}
}

func TestCallsThatDoNotDeclareThemselvesAreRefused(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	bad := []router.CallInfo{
		{RunID: "r", Stage: "s", Tier: router.TierAny},                      // no scope
		{RunID: "r", Stage: "s", Tier: router.TierAny, Scope: "everything"}, // unknown scope
		{RunID: "r", Stage: "s", Tier: "huge", Scope: router.ScopeNode},     // unknown tier
		{Stage: "s", Tier: router.TierAny, Scope: router.ScopeNode},         // no run
		{RunID: "r", Tier: router.TierAny, Scope: router.ScopeNode},         // no stage
	}
	for _, i := range bad {
		if _, err := g.r.Call(ctx, i, req("hi")); err == nil {
			t.Errorf("Call accepted %+v", i)
		}
	}
	if g.mini.Calls()+g.kilo.Calls()+g.ovh.Calls() != 0 {
		t.Error("a refused call reached a provider")
	}
}

// Every row says what kind of work the call was. A call that does not say
// gets the stage up to its first colon; a leaf call also carries its class.
func TestLedgerRowsCarryTaskTypeAndNodeClass(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	leaf := info(router.TierStrong, router.ScopeNode)
	leaf.NodeClass = "validation"
	if _, err := g.r.Call(ctx, leaf, req("a")); err != nil {
		t.Fatal(err)
	}
	fill := router.CallInfo{RunID: "r", Stage: "coverage_fill", TaskType: "coverage", Tier: router.TierStrong, Scope: router.ScopeBrief}
	if _, err := g.r.Call(ctx, fill, req("b")); err != nil {
		t.Fatal(err)
	}
	rows := g.rows(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].TaskType != "testwrite" || rows[0].NodeClass != "validation" {
		t.Errorf("leaf row = task %q class %q, want testwrite and validation", rows[0].TaskType, rows[0].NodeClass)
	}
	if rows[1].TaskType != "coverage" || rows[1].NodeClass != "" {
		t.Errorf("fill row = task %q class %q, want coverage and no class", rows[1].TaskType, rows[1].NodeClass)
	}
	sum, err := g.led.Summary(ctx, "r")
	if err != nil || len(sum) != 2 {
		t.Fatalf("summary = %d groups, %v; want 2 (one per task type)", len(sum), err)
	}
}

func TestOnlyAndExcludeSteerTheWalk(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	i := info(router.TierAny, router.ScopeNode)
	i.Only = "ovh/q27"
	if res, err := g.r.Call(ctx, i, req("hi")); err != nil || res.Entry != "ovh/q27" {
		t.Errorf("Only: %+v %v", res, err)
	}
	i = info(router.TierAny, router.ScopeNode)
	i.Exclude = []string{"kilo/auto", "ovh/q27"}
	if res, err := g.r.Call(ctx, i, req("hi")); err != nil || res.Entry != "mini/qwen" {
		t.Errorf("Exclude: %+v %v", res, err)
	}
	i.Exclude = []string{"kilo/auto", "ovh/q27", "mini/qwen"}
	var ce *router.ChainExhausted
	if _, err := g.r.Call(ctx, i, req("hi")); !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonExcluded {
		t.Errorf("excluding everything: %v", err)
	}
}

func TestNoPromptOrReplyTextReachesTheRunFolder(t *testing.T) {
	const canaryPrompt, canaryReply = "CANARY-PROMPT-8c41d2", "CANARY-REPLY-19ab73"
	g := newRig(t,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
		},
		okText(canaryReply), nil, nil)
	ctx := context.Background()
	if _, err := g.r.Call(ctx, info(router.TierStandard, router.ScopeNode), req(canaryPrompt)); err != nil {
		t.Fatal(err)
	}
	if _, err := g.r.CallParsed(ctx, info(router.TierStandard, router.ScopeNode), req(canaryPrompt), func(string) error {
		return errors.New("the reply " + canaryReply + " is not JSON")
	}); err == nil {
		t.Fatal("a parser that rejects everything should exhaust the chain")
	}
	// Every file the ledger wrote into the run folder, whatever its name.
	files := 0
	err := filepath.WalkDir(g.runDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), canaryPrompt) || strings.Contains(string(b), canaryReply) {
			t.Errorf("%s contains prompt or reply text", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Error("the ledger wrote no file, so the scan proved nothing")
	}
}

var errBad = errors.New("not JSON")

func rejectBad(text string) error {
	if text == "bad" {
		return errBad
	}
	return nil
}

func TestCallParsedRetriesTheSameModelOnceWithTheParseError(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{Text: "bad", Model: r.Model}, nil
		}
		return provider.Response{Text: "good", Model: r.Model}, nil
	}, nil, nil, nil)
	res, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	if err != nil || res.Text != "good" || res.Entry != "mini/qwen" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 0 {
		t.Errorf("mini %d kilo %d, want 2 and 0", g.mini.Calls(), g.kilo.Calls())
	}
	second := g.mini.Requests()[1].Messages
	if len(second) != 3 || second[1].Role != provider.RoleAssistant || second[1].Content != "bad" ||
		second[2].Role != provider.RoleUser || !strings.Contains(second[2].Content, "not JSON") {
		t.Errorf("retry request = %+v", second)
	}
	rows := g.rows(t)
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[0].ErrorKind != "malformed" || rows[1].Outcome != ledger.OutcomeOK {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCallParsedExcludesAModelThatKeepsReturningGarbage(t *testing.T) {
	g := newRig(t, okText("bad"), okText("good"), nil, nil)
	res, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	if err != nil || res.Entry != "kilo/auto" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 1 {
		t.Errorf("mini %d kilo %d, want 2 and 1", g.mini.Calls(), g.kilo.Calls())
	}
	var outcomes []ledger.Outcome
	for _, row := range g.rows(t) {
		outcomes = append(outcomes, row.Outcome)
	}
	want := []ledger.Outcome{ledger.OutcomeMalformed, ledger.OutcomeMalformed, ledger.OutcomeOK}
	if len(outcomes) != 3 || outcomes[0] != want[0] || outcomes[1] != want[1] || outcomes[2] != want[2] {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
}

func TestCallParsedReportsTheLastParseErrorWhenEveryModelFails(t *testing.T) {
	g := newRig(t, okText("bad"), okText("bad"), nil, nil)
	_, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || !errors.Is(err, errBad) {
		t.Fatalf("err = %v, want a ChainExhausted wrapping the parse error", err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 2 {
		t.Errorf("mini %d kilo %d, want 2 each", g.mini.Calls(), g.kilo.Calls())
	}
	for _, row := range g.rows(t) {
		if row.Outcome != ledger.OutcomeMalformed {
			t.Errorf("row %+v should be malformed", row)
		}
	}
}

func TestCallParsedMakesOneCallWhenTheFirstReplyParses(t *testing.T) {
	g := newRig(t, okText("good"), nil, nil, nil)
	if _, err := g.r.CallParsed(context.Background(), info(router.TierStrong, router.ScopeNode), req("x"), rejectBad); err != nil {
		t.Fatal(err)
	}
	if g.mini.Calls() != 1 || len(g.rows(t)) != 1 {
		t.Errorf("mini %d, rows %d", g.mini.Calls(), len(g.rows(t)))
	}
}

// blocker waits for its context to end, like a slow model.
type blocker struct {
	started chan struct{}
	once    sync.Once
}

func (b *blocker) Name() string { return "mini" }
func (b *blocker) Models() []provider.ModelInfo {
	return []provider.ModelInfo{{ID: "qwen", ContextTokens: 1000}}
}
func (b *blocker) Complete(ctx context.Context, _ provider.Request) (provider.Response, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

func TestCancellationLeavesExactlyOneCancelledRow(t *testing.T) {
	b := &blocker{started: make(chan struct{})}
	g := &rig{}
	g.build(t, testConfig(), map[string]provider.Provider{"mini": b}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.r.Call(ctx, info(router.TierStrong, router.ScopeNode), req("hi"))
		done <- err
	}()
	<-b.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	rows := g.rows(t)
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeError || rows[0].ErrorKind != "cancelled" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCallTimeoutIsRecordedAsATimeout(t *testing.T) {
	cfg := testConfig()
	cfg.Defaults.CallTimeout = 20 * time.Millisecond
	b := &blocker{started: make(chan struct{})}
	g := &rig{}
	g.build(t, cfg, map[string]provider.Provider{"mini": b}, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonFailed {
		t.Fatalf("err = %v", err)
	}
	rows := g.rows(t)
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeTimeout {
		t.Errorf("rows = %+v", rows)
	}
}

// brokenLedger fails every write, like a full disk.
type brokenLedger struct{ ledger.Ledger }

func (brokenLedger) Record(context.Context, *ledger.Call) error { return errors.New("disk full") }

func TestALedgerFailureDoesNotThrowAwayTheModelsAnswer(t *testing.T) {
	g := &rig{}
	cfg := testConfig()
	g.mini = provider.NewFake("mini", nil, okText("precious answer"))
	g.build(t, cfg, map[string]provider.Provider{"mini": g.mini}, brokenLedger{})
	res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	if err != nil || res.Text != "precious answer" || res.CallID != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if g.r.LedgerErrors() != 1 {
		t.Errorf("LedgerErrors = %d, want 1", g.r.LedgerErrors())
	}
	if n := len(g.sink.OfKind(events.KindLedgerError)); n != 1 {
		t.Errorf("%d ledger_error events, want 1", n)
	}
}

func TestAProviderNeverRunsMoreCallsAtOnceThanItsMaxConcurrent(t *testing.T) {
	var inflight, peak atomic.Int32
	g := newRig(t, func(_ int, r provider.Request) (provider.Response, error) {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inflight.Add(-1)
		return provider.Response{Text: "ok", Model: r.Model}, nil
	}, nil, nil, nil) // mini has max_concurrent 1
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak.Load())
	}
	if g.mini.Calls() != 8 || len(g.rows(t)) != 8 {
		t.Errorf("calls %d rows %d", g.mini.Calls(), len(g.rows(t)))
	}
}

func TestTruncationRetriesOnceOnTheSameEntryWithADoubledBudget(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrTruncated{Provider: "mini"}
		}
		return provider.Response{Text: "ok", Model: r.Model}, nil
	}, nil, nil, nil)
	rq := req("hi")
	rq.MaxTokens = 100
	res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), rq)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "mini/qwen" || g.mini.Calls() != 2 {
		t.Errorf("entry %s calls %d", res.Entry, g.mini.Calls())
	}
	reqs := g.mini.Requests()
	if reqs[0].MaxTokens != 100 || reqs[1].MaxTokens != 200 {
		t.Errorf("max_tokens %d then %d, want 100 then 200", reqs[0].MaxTokens, reqs[1].MaxTokens)
	}
	rows := g.rows(t)
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeError || rows[0].ErrorKind != "truncated" || rows[1].Outcome != ledger.OutcomeOK {
		t.Errorf("rows = %+v", rows)
	}
}

func TestTruncationTwiceFailsOverToTheNextEntry(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrTruncated{Provider: "mini"}
	}, okText("from kilo"), nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "kilo/auto" || g.mini.Calls() != 2 {
		t.Errorf("entry %s mini calls %d", res.Entry, g.mini.Calls())
	}
	if n := len(g.rows(t)); n != 3 {
		t.Errorf("%d rows, want 3 (two truncated, one ok)", n)
	}
}

func TestTruncationBudgetIsCapped(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrTruncated{Provider: "mini"}
		}
		return provider.Response{Text: "ok", Model: r.Model}, nil
	}, nil, nil, func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 100000 })
	rq := req("hi")
	rq.MaxTokens = 12000
	if _, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), rq); err != nil {
		t.Fatal(err)
	}
	if got := g.mini.Requests()[1].MaxTokens; got != 16384 {
		t.Errorf("retry max_tokens = %d, want the 16384 cap", got)
	}
}

func TestEmptyReplyIsOneFailedAttemptAndFailsOver(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrEmptyReply{Provider: "mini"}
	}, okText("from kilo"), nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "kilo/auto" || g.mini.Calls() != 1 {
		t.Errorf("entry %s mini calls %d (an empty reply is not retried on the same entry)", res.Entry, g.mini.Calls())
	}
	rows := g.rows(t)
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeError || rows[0].ErrorKind != "empty_reply" || rows[1].Outcome != ledger.OutcomeOK {
		t.Errorf("rows = %+v", rows)
	}
}

// A truncated reply that a bigger budget then answers: one row per attempt,
// the first marked truncated (never malformed), and the entry stays usable
// for the next call in the same walk.
func TestTruncationThenSuccessIsNotExcludedAsUnusable(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		switch call {
		case 1:
			return provider.Response{}, provider.ErrTruncated{Provider: "mini"}
		case 2:
			return provider.Response{Text: "partial-then-fixed", Model: r.Model}, nil
		}
		return provider.Response{Text: "fine", Model: r.Model}, nil
	}, okText("from kilo"), nil, func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 100000 })
	rq := req("hi")
	rq.MaxTokens = 8000
	var parsed int
	res, err := g.r.CallParsed(context.Background(), info(router.TierStrong, router.ScopeNode), rq, func(string) error { parsed++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "mini/qwen" || parsed != 1 {
		t.Errorf("entry %s parsed %d, want mini/qwen and the parser run once on the good reply", res.Entry, parsed)
	}
	reqs := g.mini.Requests()
	if len(reqs) != 2 || reqs[0].MaxTokens != 8000 || reqs[1].MaxTokens != 16000 {
		t.Fatalf("requests = %d, max_tokens %v", len(reqs), reqs)
	}
	rows := g.rows(t)
	if len(rows) != 2 || rows[0].ErrorKind != "truncated" || rows[0].Outcome == ledger.OutcomeMalformed || rows[1].Outcome != ledger.OutcomeOK {
		t.Errorf("rows = %+v", rows)
	}
	// The next call still goes to the same entry first.
	res2, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("again"))
	if err != nil || res2.Entry != "mini/qwen" {
		t.Errorf("next call entry %q err %v, want mini/qwen (not excluded)", res2.Entry, err)
	}
}

// The grown-budget cap is a property of the call, so the outline can ask for
// more than the global cap while other stages keep it.
func TestGrowthCapComesFromTheRequest(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrTruncated{Provider: "mini"}
		}
		return provider.Response{Text: "ok", Model: r.Model}, nil
	}, nil, nil, func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 100000 })
	rq := req("hi")
	rq.MaxTokens = 16000
	rq.MaxGrownTokens = 32768
	if _, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), rq); err != nil {
		t.Fatal(err)
	}
	if got := g.mini.Requests()[1].MaxTokens; got != 32000 {
		t.Errorf("retry max_tokens = %d, want 32000 (double, under the request's 32768 cap)", got)
	}
}

func TestChainExhaustedOnlyTooLong(t *testing.T) {
	tl := router.EntryReason{Entry: "a/x", Kind: router.ReasonTooLong}
	cases := map[string]struct {
		ce   *router.ChainExhausted
		want bool
	}{
		"every entry too long":   {&router.ChainExhausted{Reasons: []router.EntryReason{tl, tl}}, true},
		"one too long, one auth": {&router.ChainExhausted{Reasons: []router.EntryReason{tl, {Kind: router.ReasonAuth}}}, false},
		"a bad reply too":        {&router.ChainExhausted{Reasons: []router.EntryReason{tl}, ParseErr: errors.New("x")}, false},
		"no reasons":             {&router.ChainExhausted{}, false},
	}
	for name, c := range cases {
		if got := c.ce.OnlyTooLong(); got != c.want {
			t.Errorf("%s: OnlyTooLong = %v, want %v", name, got, c.want)
		}
	}
}

// countingBlocker blocks until its context ends and counts the calls.
type countingBlocker struct {
	name string
	n    atomic.Int32
}

func (b *countingBlocker) Name() string { return b.name }
func (b *countingBlocker) Models() []provider.ModelInfo {
	return []provider.ModelInfo{{ID: "qwen", ContextTokens: 1000}}
}
func (b *countingBlocker) Complete(ctx context.Context, _ provider.Request) (provider.Response, error) {
	b.n.Add(1)
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

func TestTimeoutCoolsTheEntryAndTheNextEntryAnswers(t *testing.T) {
	cfg := testConfig()
	cfg.Defaults.CallTimeout = 20 * time.Millisecond
	cfg.RateLimits.CooldownAfterTimeoutSeconds = 7
	dead := &countingBlocker{name: "mini"}
	kilo := provider.NewFake("kilo", []provider.ModelInfo{{ID: "auto", ContextTokens: 100000}}, okText("from kilo"))
	g := &rig{}
	g.build(t, cfg, map[string]provider.Provider{"mini": dead, "kilo": kilo}, nil)
	call := func() router.Result {
		t.Helper()
		res, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(); res.Entry != "kilo/auto" || dead.n.Load() != 1 {
		t.Fatalf("first call: entry %s, dead calls %d", res.Entry, dead.n.Load())
	}
	// A dead provider no longer costs a call_timeout on every call.
	if res := call(); res.Entry != "kilo/auto" || dead.n.Load() != 1 {
		t.Fatalf("second call: entry %s, dead calls %d", res.Entry, dead.n.Load())
	}
	g.clk.advance(6 * time.Second)
	call()
	if dead.n.Load() != 1 {
		t.Fatal("the cooldown ended early")
	}
	g.clk.advance(2 * time.Second)
	call()
	if dead.n.Load() != 2 {
		t.Fatalf("the entry was not retried after the cooldown: %d", dead.n.Load())
	}
}

func TestTimeoutCooldownDefaultsToSixtySecondsWhenUnset(t *testing.T) {
	cfg := testConfig()
	cfg.Defaults.CallTimeout = 20 * time.Millisecond
	cfg.RateLimits.CooldownAfterTimeoutSeconds = 0
	dead := &countingBlocker{name: "mini"}
	kilo := provider.NewFake("kilo", []provider.ModelInfo{{ID: "auto", ContextTokens: 100000}}, okText("k"))
	g := &rig{}
	g.build(t, cfg, map[string]provider.Provider{"mini": dead, "kilo": kilo}, nil)
	for i := 0; i < 2; i++ {
		if _, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi")); err != nil {
			t.Fatal(err)
		}
	}
	g.clk.advance(59 * time.Second)
	_, _ = g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	if dead.n.Load() != 1 {
		t.Fatalf("calls = %d", dead.n.Load())
	}
	g.clk.advance(2 * time.Second)
	_, _ = g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	if dead.n.Load() != 2 {
		t.Fatalf("calls = %d", dead.n.Load())
	}
}

func TestPerProviderCallTimeoutOverride(t *testing.T) {
	cfg := testConfig()
	cfg.Defaults.CallTimeout = time.Hour
	one := 1
	cfg.Providers[0].CallTimeoutSeconds = &one
	b := &countingBlocker{name: "mini"}
	g := &rig{}
	g.build(t, cfg, map[string]provider.Provider{"mini": b}, nil)
	start := time.Now()
	_, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonFailed {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 20*time.Second {
		t.Fatalf("the call took %v, want about the 1 s override", d)
	}
	rows := g.rows(t)
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeTimeout {
		t.Errorf("rows = %+v", rows)
	}
}
