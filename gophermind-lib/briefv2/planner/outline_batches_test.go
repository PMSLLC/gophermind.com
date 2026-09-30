package planner_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

var featureNames = []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta"}

// setFeatures rewrites the rig's brief so it has exactly these features.
func setFeatures(t *testing.T, g *rig, names []string) {
	t.Helper()
	raw, err := os.ReadFile(g.briefPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "### Greeting")
	end := strings.Index(s, "## Architecture")
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "### %s\n\nThe %s feature.\n\n", n, n)
	}
	if err := os.WriteFile(g.briefPath, []byte(s[:start]+b.String()+s[end:]), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lower(n string) string { return strings.ToLower(n) }

func compReply(id string) string {
	return fmt.Sprintf(`{"types": [], "functions": [{"id": "fn-%s-run", "package": "greet", "file": "internal/greet/%s.go", "signature": "func Run%s() error", "doc": "Run does the work.", "uses": ["name-error"]}], "more": false}`,
		id, id, strings.ToUpper(id[:1])+id[1:])
}

func sharedReply() string {
	return `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "types": [` + nameErrorType + `]}`
}

func batchReply(names ...string) string {
	var cs []string
	for _, n := range names {
		cs = append(cs, comp(lower(n)))
	}
	return `{"components": [` + strings.Join(cs, ", ") + `], "types": []}`
}

// outlineRig builds a rig for a brief with these features, with a scripted
// shared pass, the given batch replies (outline.2, outline.3, ...) and one
// reply per component.
func outlineRig(t *testing.T, names []string, batches map[int]string) *rig {
	t.Helper()
	files := map[string]string{"contract.outline.1.txt": sharedReply()}
	for n, r := range batches {
		files[fmt.Sprintf("contract.outline.%d.txt", n)] = r
	}
	for _, n := range names {
		files["contract."+lower(n)+".txt"] = compReply(lower(n))
	}
	g := newRig(t, approving(), variant(t, files))
	setFeatures(t, g, names)
	return g
}

func passStages(g *rig) string {
	var out []string
	for _, s := range g.stagesCalled() {
		if strings.HasPrefix(s, "contract:outline") {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}

func warningsWith(g *rig, word string) []events.Event {
	var out []events.Event
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if strings.Contains(e.Message, word) {
			out = append(out, e)
		}
	}
	return out
}

func TestSevenFeaturesGiveASharedPassAndThreeBatches(t *testing.T) {
	g := outlineRig(t, featureNames, map[int]string{
		2: batchReply("Alpha", "Beta", "Gamma"), 3: batchReply("Delta", "Epsilon", "Zeta"), 4: batchReply("Eta"),
	})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:1 contract:outline:2 contract:outline:3 contract:outline:4" {
		t.Fatalf("outline stages = %q, want 1 shared pass and 3 batches", got)
	}
	if got := componentIDs(g); got != "types alpha beta gamma delta epsilon zeta eta" {
		t.Errorf("components = %q", got)
	}
	var prompts []string
	for _, r := range g.fake.Requests() {
		if strings.HasPrefix(planner.StageOf(r), "contract:outline") {
			prompts = append(prompts, r.Messages[1].Content)
		}
	}
	for i, want := range []string{"Alpha, Beta, Gamma", "Delta, Epsilon, Zeta", "Eta"} {
		p := prompts[i+1]
		if !strings.Contains(p, "THESE features only: "+want+"\n") {
			t.Errorf("batch %d prompt does not name %q", i+1, want)
		}
		if !strings.Contains(p, "### Alpha") {
			t.Errorf("batch %d prompt lacks the whole brief", i+1)
		}
	}
	if !strings.Contains(prompts[2], "components: types, alpha, beta, gamma") {
		t.Errorf("batch 2 prompt lacks the ids declared so far:\n%s", prompts[2])
	}
	if strings.Contains(prompts[0], "THESE features") || strings.Contains(strings.Join(prompts, "\n"), `"more"`) {
		t.Error("the shared pass names features, or a prompt still mentions more")
	}
	if n := len(warningsWith(g, "outline_pass_empty")); n != 0 {
		t.Errorf("%d empty-pass warnings on a clean run", n)
	}
}

func TestOneFeatureIsASharedPassAndOneBatch(t *testing.T) {
	g := outlineRig(t, featureNames[:1], map[int]string{2: batchReply("Alpha")})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:1 contract:outline:2" {
		t.Errorf("outline stages = %q", got)
	}
}

func TestAnExactMultipleOfTheBatchSizeHasNoEmptyBatch(t *testing.T) {
	g := outlineRig(t, featureNames[:6], map[int]string{2: batchReply("Alpha", "Beta", "Gamma"), 3: batchReply("Delta", "Epsilon", "Zeta")})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:1 contract:outline:2 contract:outline:3" {
		t.Errorf("outline stages = %q", got)
	}
}

// The model cannot tell what is left: it repeats earlier ids with different
// content in every batch and says more. The first emissions stay.
func TestReEmittedIDsAndMoreAreNoise(t *testing.T) {
	again := func(extra string, names ...string) string {
		r := batchReply(names...)
		return strings.Replace(r, `"components": [`, `"more": true, "components": [{"id": "types", "package": "other", "exports": []}, `+extra, 1)
	}
	g := outlineRig(t, featureNames[:6], map[int]string{
		2: again("", "Alpha", "Beta", "Gamma"),
		3: again(`{"id": "alpha", "package": "other", "exports": []}, `, "Delta", "Epsilon", "Zeta")})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types alpha beta gamma delta epsilon zeta" {
		t.Errorf("components = %q", got)
	}
	for _, c := range g.contracts().Components {
		if c.Package != "greet" {
			t.Errorf("component %s package %q, want the first emission", c.ID, c.Package)
		}
	}
	if n := len(warningsWith(g, "outline_duplicate_ignored")); n != 2 {
		t.Errorf("duplicate warnings = %d, want 2 (one per batch)", n)
	}
	if got := passStages(g); strings.Count(got, "contract:outline") != 3 {
		t.Errorf("outline stages = %q, want no retries", got)
	}
}

func TestABatchThatAddsNothingIsAWarningNotAnError(t *testing.T) {
	g := outlineRig(t, featureNames[:6], map[int]string{2: batchReply("Alpha", "Beta", "Gamma"), 3: `{"components": [], "types": [], "more": true}`})
	// The fixture has no component replies for Delta..Zeta: they are never declared.
	g.mustPlan(planner.Options{StopAfter: "contract"})
	w := warningsWith(g, "outline_pass_empty")
	if len(w) != 1 || !strings.Contains(w[0].Message, "batch 2") || strings.Contains(w[0].Message, "Delta") {
		t.Fatalf("warnings = %v, want one naming the batch index only", w)
	}
	if got := componentIDs(g); got != "types alpha beta gamma" {
		t.Errorf("components = %q", got)
	}
}

func TestOutlineResumeSkipsFinishedBatches(t *testing.T) {
	files := map[string]string{"contract.outline.1.txt": sharedReply(), "contract.outline.2.txt": batchReply("Alpha", "Beta", "Gamma"),
		"contract.outline.3.txt": "the model fell over", "contract.outline.3.2.txt": "and again"}
	for _, n := range featureNames[:6] {
		files["contract."+lower(n)+".txt"] = compReply(lower(n))
	}
	r := newRig(t, approving(), variant(t, files))
	setFeatures(t, r, featureNames[:6])
	if _, err := r.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want batch 2 to fail")
	}
	if r.has("contracts.json") || !strings.Contains(string(r.read("_state/contract.json")), `"outline_batches"`) {
		t.Fatal("the stored state must hold the batch list and contracts.json must not exist yet")
	}
	more := map[string]string{"contract.outline.3.txt": batchReply("Delta", "Epsilon", "Zeta")}
	for _, n := range featureNames[:6] {
		more["contract."+lower(n)+".txt"] = compReply(lower(n))
	}
	r.wire(variant(t, more))
	r.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := passStages(r); got != "contract:outline:3" {
		t.Errorf("resume asked %q, want only the unfinished batch", got)
	}
	if got := componentIDs(r); got != "types alpha beta gamma delta epsilon zeta" {
		t.Errorf("components = %q", got)
	}
}

func TestOutlineBatchErrorsNeverQuoteReplyText(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"contract.outline.1.txt": sharedReply(),
		"contract.outline.2.txt": "CANARY reply text", "contract.outline.2.2.txt": "CANARY again"}))
	setFeatures(t, g, featureNames[:2])
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err = %v, want a failure that quotes no reply text", err)
	}
}

// The same model behaviour in a component pass: it says more, then repeats
// what it already wrote. Nothing new means the component is finished.
func TestComponentPassThatRepeatsItselfEndsTheComponent(t *testing.T) {
	again := strings.Replace(greetReply("name-error"), `"more": false`, `"more": true`, 1)
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": again, "contract.greeting.2.txt": again,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:greeting"); n != 2 {
		t.Errorf("greeting calls = %d, want the pass and one that added nothing", n)
	}
	if n := len(g.contracts().Functions); n != 3 {
		t.Errorf("functions = %d, want 3", n)
	}
}
