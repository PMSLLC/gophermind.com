package planner_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
)

const miniWindow = 32768

func estTokens(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content) + 1
	}
	return n/4 + n/40
}

// bigBrief gives the Greeting and Farewell features bodies of this many bytes
// each, starting with a marker naming the feature.
func bigBrief(t *testing.T, g *rig, bytes int) {
	t.Helper()
	raw, err := os.ReadFile(g.briefPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start, end := strings.Index(s, "### Greeting"), strings.Index(s, "## Architecture")
	var b strings.Builder
	for _, n := range []string{"Greeting", "Farewell"} {
		b.WriteString("### " + n + "\n\nBODY-START-" + n + "\n\n")
		for b.Len() < bytes {
			b.WriteString("The " + n + " feature does one more thing that nobody asked for yet.\n")
		}
		b.WriteString("\n")
		bytes += b.Len()
	}
	if err := os.WriteFile(g.briefPath, []byte(s[:start]+b.String()+s[end:]), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setWindow(g *rig, tokens int) { g.cfg.Providers[0].Models[0].ContextTokens = tokens }

func contractRequests(g *rig) []provider.Request {
	var out []provider.Request
	for _, r := range g.fake.Requests() {
		if strings.HasPrefix(planner.StageOf(r), "contract:") {
			out = append(out, r)
		}
	}
	return out
}

// A brief of about 55k tokens on a model that holds 32768: every contract call
// is sized to fit, the run finishes, and each call reports its size.
func TestContractPromptsFitTheSmallestWindow(t *testing.T) {
	g := newRig(t, approving())
	bigBrief(t, g, 100_000)
	// Clarify carries the whole brief and is not sized; it runs first.
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	setWindow(g, miniWindow)
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	calls := contractRequests(g)
	if len(calls) < 5 {
		t.Fatalf("%d contract calls", len(calls))
	}
	for _, r := range calls {
		if est := estTokens(r); est+r.MaxTokens > miniWindow {
			t.Errorf("%s: about %d tokens plus %d reserved exceeds the %d window", planner.StageOf(r), est, r.MaxTokens, miniWindow)
		}
	}
	var sized []events.Event
	for _, e := range g.sink.Events() {
		if e.Kind == planner.KindPromptSize {
			sized = append(sized, e)
		}
	}
	if len(sized) != len(calls) {
		t.Fatalf("prompt_size events = %d, contract calls = %d", len(sized), len(calls))
	}
	if m := sized[0].Message; !strings.Contains(m, "prompt_tokens_estimate=") || !strings.Contains(m, "window=32768") {
		t.Errorf("event = %q", m)
	}
}

// A small brief is sent whole, as before.
func TestSmallBriefIsStillSentWhole(t *testing.T) {
	g := newRig(t, approving())
	raw, err := os.ReadFile(g.briefPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)[strings.Index(string(raw), "## Overview"):]
	setWindow(g, miniWindow)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	n := 0
	for _, r := range contractRequests(g) {
		if strings.HasPrefix(planner.StageOf(r), "contract:outline:") {
			n++
			if !strings.Contains(r.Messages[1].Content, body) {
				t.Errorf("%s does not carry the whole brief", planner.StageOf(r))
			}
		}
	}
	if n != 2 {
		t.Errorf("outline calls = %d", n)
	}
}

// The schema repair asks about the offending node with its feature's section,
// never the whole brief.
func TestSchemaRepairPromptStaysUnderTheWindowWithABigBrief(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"contract.greeting.txt": bigGreeting(136)}))
	bigBrief(t, g, 100_000)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	breakState(t, g, "doc")
	g.wire(variant(t, map[string]string{"contract._schema.txt": `{"functions": [{"id": "fn-g135", "doc": "Fixed."}]}`}))
	setWindow(g, miniWindow)
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	var fix provider.Request
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:_schema" {
			fix = r
		}
	}
	p := fix.Messages[1].Content
	if est := estTokens(fix); est+fix.MaxTokens > miniWindow {
		t.Errorf("schema repair prompt: about %d tokens plus %d reserved exceeds %d", est, fix.MaxTokens, miniWindow)
	}
	if !strings.Contains(p, "BODY-START-Greeting") || strings.Contains(p, "BODY-START-Farewell") {
		t.Error("the prompt must carry the owning feature's section and no other feature")
	}
	if len(p) > 60_000 {
		t.Errorf("schema repair prompt is %d bytes", len(p))
	}
}

// tooLongRig makes contract calls above limit tokens fail with the provider's
// typed too-long error, while the router still believes the window is large.
func tooLongRig(t *testing.T, limit int) *rig {
	t.Helper()
	g := newRig(t, approving())
	bigBrief(t, g, 100_000)
	base, err := planner.FixtureProvider(greeter)
	if err != nil {
		t.Fatal(err)
	}
	w := provider.NewFake("fake", []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}}, func(_ int, req provider.Request) (provider.Response, error) {
		if strings.HasPrefix(planner.StageOf(req), "contract:") && estTokens(req) > limit {
			return provider.Response{}, provider.ErrContextTooLong{Limit: limit}
		}
		return base.Complete(context.Background(), req)
	})
	g.fake = w
	g.router = router.New(g.cfg, map[string]provider.Provider{"fake": w}, g.led, g.sink)
	g.deps.Caller = g.router
	return g
}

// A refusal as too long is not an unusable reply: the prompt is rebuilt with
// less optional context, one step at a time, and asked again.
func TestTooLongRefusalStepsDownTheContext(t *testing.T) {
	g := tooLongRig(t, 9000)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	var sizes []int
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:outline:1" {
			sizes = append(sizes, estTokens(r))
		}
	}
	if len(sizes) < 2 || sizes[0] <= 9000 || sizes[len(sizes)-1] > 9000 {
		t.Fatalf("shared pass prompt sizes = %v, want a refused one then a smaller one that fits", sizes)
	}
	for i := 1; i < len(sizes); i++ {
		if sizes[i] >= sizes[i-1] {
			t.Errorf("sizes = %v, each retry must be smaller", sizes)
		}
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	tooLong := 0
	for _, r := range rows {
		if r.Outcome == ledger.OutcomeTooLong {
			tooLong++
		}
	}
	if tooLong == 0 {
		t.Error("no too_long row in the ledger")
	}
}

func TestTooLongEvenWithoutOptionalContextFailsWithAFixedMessage(t *testing.T) {
	g := tooLongRig(t, 300)
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "contract:outline:1") || !strings.Contains(err.Error(), "optional context") || strings.Contains(err.Error(), "BODY-START") {
		t.Fatalf("err = %v, want a fixed message naming the stage", err)
	}
	if n := count(g.stagesCalled(), "contract:outline:1"); n != 4 {
		t.Errorf("shared pass calls = %d, want one per level (4)", n)
	}
}
