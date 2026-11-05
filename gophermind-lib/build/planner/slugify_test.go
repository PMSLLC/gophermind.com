package planner_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/human"
	"gophermind/gophermind-lib/build/planner"
)

// The r001 regression: every node packet carries the feature text and the
// Architecture bullets, there is one entry component, and the fixed
// interfaces of the brief are the contract's signatures.
func TestSlugifyPacketsCarryTheFeatureText(t *testing.T) {
	gate := human.NewProgrammatic()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case req := <-gate.Requests():
				switch req.Kind {
				case human.KindAsk:
					as := make([]human.Answer, len(req.Questions))
					for i, q := range req.Questions {
						as[i] = human.Answer{ID: q.ID, Text: "Yes"}
					}
					req.Answer(as)
				case human.KindConfirm, human.KindApprove:
					req.Decide(human.Decision{Approved: true, By: "programmatic"})
				}
			}
		}
	}()
	g := newRigFor(t, gate, slugify, slugifyID)
	out, err := g.plan(planner.Options{}) // the outcome is not the subject; the packets are
	t.Logf("plan = %q, %v", out, err)

	byStage := map[string][]string{}
	for _, req := range g.fake.Requests() {
		stage := planner.StageOf(req)
		byStage[stage] = append(byStage[stage], req.Messages[1].Content)
	}
	mustHave := []string{"contract:slug-logic", "contract:cli-logic", "decompose:slug-logic", "decompose:cli-logic",
		"enrich:cli-logic", "testwrite:fn-make", "testwrite:fn-run"}
	for _, stage := range mustHave {
		msgs := byStage[stage]
		if len(msgs) == 0 {
			t.Fatalf("stage %s was never asked; stages: %v", stage, g.stagesCalled())
		}
		for _, want := range []string{"### Make a slug", "### Report errors",
			"`--max 6` on `Hello, World!` gives `hello`",
			"prints `usage: slugify [--max N] [FILE]`",
			"`Make(s string, max int) string`"} {
			if !strings.Contains(msgs[0], want) {
				t.Errorf("%s packet lacks %q", stage, want)
			}
		}
	}
	if contains(g.stagesCalled(), "contract:outline:entry") {
		t.Error("an entry pass ran although the batch pass's main component was mapped to cmd/slugify")
	}

	c := g.contracts()
	var pkgs, cmds []string
	for _, comp := range c.Components {
		pkgs = append(pkgs, comp.Package)
		if strings.HasPrefix(comp.Package, "cmd/") {
			cmds = append(cmds, comp.ID+" "+comp.Package)
		}
	}
	if got := strings.Join(cmds, ","); got != "entry-point cmd/slugify" {
		t.Errorf("components under cmd/ = %q, want exactly entry-point cmd/slugify", got)
	}
	if got := strings.Join(pkgs, ","); got != "internal/slug,internal/cli,cmd/slugify" {
		t.Errorf("component packages = %s", got)
	}
	fns := map[string]struct{ pkg, comp, sig string }{}
	for _, f := range c.Functions {
		fns[f.ID] = struct{ pkg, comp, sig string }{f.Package, f.Component, f.Signature}
	}
	if f := fns["fn-make"]; f.pkg != "internal/slug" || f.sig != "func Make(s string, max int) string" {
		t.Errorf("fn-make = %+v", f)
	}
	if f := fns["fn-run"]; f.comp != "cli-logic" || f.sig != "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int" {
		t.Errorf("fn-run = %+v", f)
	}
	if f := fns["fn-main"]; f.comp != "entry-point" || f.sig != "func main()" {
		t.Errorf("fn-main = %+v", f)
	}

	if !g.has("_state/sources.json") {
		t.Fatal("no _state/sources.json")
	}
	var src struct {
		Components map[string][]string `json:"components"`
	}
	if err := json.Unmarshal(g.read("_state/sources.json"), &src); err != nil {
		t.Fatal(err)
	}
	if len(src.Components) != 3 {
		t.Errorf("sources.json components = %v, want three", src.Components)
	}
	for id, ids := range src.Components {
		if !contains(ids, "feature-make-a-slug") || !contains(ids, "feature-report-errors") {
			t.Errorf("sources.json component %s = %v, lacks a feature", id, ids)
		}
	}
}
