package planner_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// A state written before the batches existed (shared pass stored, outline not
// done, no batch list) continues with the batches.
func TestOldFormatStateResumesWithTheBatches(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.2.txt": "the model fell over", "contract.outline.2.2.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the batch to fail")
	}
	path := filepath.Join(g.runDir, "_state", "contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.NewReplacer(`"outline_batches"`, `"x_outline_batches"`, `"outline_batches_set"`, `"x_set"`, `"outline_batch_next"`, `"x_next"`).Replace(string(raw))
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:2" {
		t.Errorf("resume asked %q, want the batch", got)
	}
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
}

func TestSharedPassComponentsOtherThanTypesAreDroppedWithAWarning(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	w := warningsWith(g, "outline_shared_extra")
	if len(w) != 1 || !strings.Contains(w[0].Message, `"greeting"`) {
		t.Fatalf("warnings = %v", w)
	}
	// The batch declares the feature component; the shared pass did not.
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
}

// 400 features: the declared-ids list stays bounded in every batch prompt and
// the merge is correct.
func TestFourHundredFeaturesKeepTheBatchPromptBounded(t *testing.T) {
	var names []string
	for i := 1; i <= 400; i++ {
		names = append(names, fmt.Sprintf("F%03d", i))
	}
	batches := map[int]string{}
	for i := 0; i*3 < len(names); i++ {
		end := i*3 + 3
		if end > len(names) {
			end = len(names)
		}
		batches[i+2] = batchReply(names[i*3 : end]...)
	}
	g := outlineRig(t, names, batches)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := len(g.contracts().Components); n != 401 {
		t.Fatalf("components = %d, want 401", n)
	}
	last, first := 0, 0
	for _, r := range g.fake.Requests() {
		if strings.HasPrefix(planner.StageOf(r), "contract:outline:") {
			p := r.Messages[1].Content
			if first == 0 {
				first = len(p)
			}
			last = len(p)
		}
	}
	if limit := first + planner.MaxEmittedTextBytes + 2000; last > limit {
		t.Errorf("last batch prompt is %d bytes, first was %d: the emitted list is not bounded (limit %d)", last, first, limit)
	}
}

// A component that keeps adding functions forever is stopped.
func TestComponentPassCapStopsAModelThatNeverFinishes(t *testing.T) {
	files := map[string]string{}
	for i := 1; i <= 45; i++ {
		name := "contract.greeting.txt"
		if i > 1 {
			name = fmt.Sprintf("contract.greeting.%d.txt", i)
		}
		files[name] = fmt.Sprintf(`{"types": [], "functions": [{"id": "fn-g%d", "package": "greet", "file": "internal/greet/g%d.go", "signature": "func G%d() error", "doc": "G.", "uses": []}], "more": true}`, i, i, i)
	}
	g := newRig(t, approving(), variant(t, files))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "contract:greeting") || !strings.Contains(err.Error(), "40 passes") || strings.Contains(err.Error(), "fn-g") {
		t.Fatalf("err = %v, want a fixed message naming the component and the cap", err)
	}
	if n := count(g.stagesCalled(), "contract:greeting"); n != 40 {
		t.Errorf("greeting calls = %d, want exactly the cap", n)
	}
}
