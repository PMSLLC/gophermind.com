package planner_test

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
)

func stageReq(stage string) provider.Request {
	return provider.Request{Model: "fixture", Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "GopherMind planner. Stage: " + stage},
		{Role: provider.RoleUser, Content: "prompt"},
	}}
}

func TestFixtureProviderServesRepliesByStage(t *testing.T) {
	base := variant(t, map[string]string{
		"clarify.txt":              "base clarify",
		"decompose.greeting.txt":   "base first",
		"decompose.greeting.2.txt": "base second",
	})
	over := variant(t, map[string]string{"clarify.txt": "override clarify"})
	p, err := planner.FixtureProvider(over, base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, c := range []struct{ stage, want string }{
		{"clarify", "override clarify"},       // the first directory holding the file wins
		{"decompose:greeting", "base first"},  // a colon in the stage is a dot in the file name
		{"decompose:greeting", "base second"}, // the second request for a stage gets .2.txt
		{"decompose:greeting", "base first"},  // no .3.txt, so the base file again
		{"clarify", "override clarify"},       // no clarify.2.txt either
	} {
		resp, err := p.Complete(ctx, stageReq(c.stage))
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if resp.Text != c.want || resp.Model != "fixture" {
			t.Errorf("call %d (%s) = %q served by %q, want %q served by fixture", i+1, c.stage, resp.Text, resp.Model, c.want)
		}
	}
	if _, err := p.Complete(ctx, stageReq("coverage")); err == nil || !strings.Contains(err.Error(), "coverage.txt") {
		t.Errorf("a stage with no file must fail naming the file, got %v", err)
	}
	if _, err := p.Complete(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}}); err == nil {
		t.Error("a request with no stage line must fail")
	}
	if got := planner.StageOf(stageReq("testwrite:fn-greet")); got != "testwrite:fn-greet" {
		t.Errorf("StageOf = %q", got)
	}
}

func TestFixtureProviderNeedsRealDirectories(t *testing.T) {
	if _, err := planner.FixtureProvider(); err == nil {
		t.Error("no directory must be an error")
	}
	if _, err := planner.FixtureProvider(t.TempDir() + "/missing"); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestFixtureSettingsAreValidAndPrivate(t *testing.T) {
	c := planner.FixtureSettings()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"strong", "standard", "any"} {
		if got := c.Models[tier]; len(got) != 1 || got[0] != "fake/fixture" {
			t.Errorf("tier %s = %v, want [fake/fixture]", tier, got)
		}
	}
	if v, ok := c.Visibility("fake"); !ok || v != "private" {
		t.Errorf("fake provider visibility = %q, %v", v, ok)
	}
}
