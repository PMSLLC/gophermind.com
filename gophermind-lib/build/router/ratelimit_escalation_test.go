package router_test

import (
	"context"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/router"
)

// A provider that keeps answering 429 is cooled down for longer each time, so a
// provider that never answers is not asked again every minute for hours.
func TestRepeatedRateLimitsDoubleTheCooldown(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrRateLimited{}
	}, nil, nil) // cooldown_after_429_seconds is 60
	call := func() {
		if _, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi")); err != nil {
			t.Fatal(err)
		}
	}
	call() // 429 number 1: cooled for 60s
	if g.kilo.Calls() != 1 {
		t.Fatalf("kilo calls %d, want 1", g.kilo.Calls())
	}
	g.clk.advance(61 * time.Second)
	call() // 429 number 2: cooled for 120s
	if g.kilo.Calls() != 2 {
		t.Fatalf("kilo calls %d, want 2", g.kilo.Calls())
	}
	g.clk.advance(61 * time.Second)
	call() // still cooling
	if g.kilo.Calls() != 2 {
		t.Errorf("kilo asked again after 61s of a 120s cooldown (calls %d)", g.kilo.Calls())
	}
	g.clk.advance(60 * time.Second)
	call() // 429 number 3: cooled for 240s
	if g.kilo.Calls() != 3 {
		t.Fatalf("kilo calls %d, want 3", g.kilo.Calls())
	}
	g.clk.advance(200 * time.Second)
	call()
	if g.kilo.Calls() != 3 {
		t.Errorf("kilo asked again after 200s of a 240s cooldown (calls %d)", g.kilo.Calls())
	}
}

func TestTheEscalatedCooldownIsCapped(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrRateLimited{}
	}, nil, nil)
	for i := 0; i < 12; i++ {
		g.clk.advance(2 * time.Hour)
		if _, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi")); err != nil {
			t.Fatal(err)
		}
	}
	before := g.kilo.Calls()
	g.clk.advance(31 * time.Minute) // past the 30 minute cap
	if _, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi")); err != nil {
		t.Fatal(err)
	}
	if g.kilo.Calls() != before+1 {
		t.Errorf("kilo not asked again 31 minutes after its last 429 (calls %d, before %d)", g.kilo.Calls(), before)
	}
}

func TestASuccessResetsTheRateLimitCount(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call == 3 {
			return provider.Response{Text: "kilo", Model: r.Model}, nil
		}
		return provider.Response{}, provider.ErrRateLimited{}
	}, nil, nil)
	call := func() {
		if _, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi")); err != nil {
			t.Fatal(err)
		}
	}
	call() // 429 number 1
	g.clk.advance(61 * time.Second)
	call() // 429 number 2: cooled for 120s
	g.clk.advance(121 * time.Second)
	call() // kilo answers: the count resets
	call() // kilo call 4 is a 429 number 1 again: cooled for 60s only
	g.clk.advance(61 * time.Second)
	before := g.kilo.Calls()
	call()
	if g.kilo.Calls() != before+1 {
		t.Errorf("kilo still cooling 61s after a first 429 that followed a success (calls %d, before %d)", g.kilo.Calls(), before)
	}
}
