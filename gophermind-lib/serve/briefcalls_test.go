package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview/runviewtest"
)

const callsSample = `{"kind":"call","id":1,"stage":"plan","node_id":"","prompt_tokens":100,"completion_tokens":20,"duration_ms":4000,"outcome":"ok","prompt_sha256":"abc"}
{"kind":"call","id":2,"task_type":"leaf","stage":"leaf","node_id":"fn-a","prompt_tokens":50,"completion_tokens":10,"duration_ms":9000,"outcome":"ok"}
{"kind":"call","id":3,"stage":"leaf:fn-b","node_id":"fn-b","prompt_tokens":50,"completion_tokens":10,"duration_ms":12000,"outcome":"malformed"}
{"kind":"call","id":4,"stage":"leaf:fn-c","node_id":"fn-c","prompt_tokens":5,"completion_tokens":5,"duration_ms":1000,"outcome":"ok"}
not json
{"kind":"amend","id":4,"outcome":"error"}
{"kind":"call","id":5,"task_type":"repair","node_id":"fn-d","duration_ms":500,"outcome":"ok"}
{"kind":"call","id":6,"stage":"plan","duration_ms":100,"outcome":"ok"}
{"kind":"call","id":7,"stage":"plan","duration_ms":200,"outcome":"ok"}
`

func TestReadCallsStagesAndSlow(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "_state"), 0o755)
	os.WriteFile(filepath.Join(dir, "_state", "calls.jsonl"), []byte(callsSample), 0o644)
	stages, slow := readCalls(dir)
	if len(stages) != 3 {
		t.Fatalf("stages %+v", stages)
	}
	leaf := stages[0]
	if leaf.Stage != "leaf" || leaf.Calls != 3 || leaf.OK != 1 || leaf.Problems != 2 || leaf.DurationMS != 22000 || leaf.Tokens != 130 {
		t.Errorf("leaf %+v (the amend turns call 4 into an error)", leaf)
	}
	if stages[1].Stage != "plan" || stages[1].Calls != 3 || stages[2].Stage != "repair" {
		t.Errorf("order or plan %+v", stages)
	}
	if len(slow) != 5 || slow[0].Node != "fn-b" || slow[0].Outcome != "malformed" || slow[1].Node != "fn-a" || slow[0].Tokens != 60 {
		t.Errorf("slow %+v", slow)
	}
	b, _ := json.Marshal(append([]any{}, stages, slow))
	if strings.Contains(string(b), "abc") {
		t.Error("a hash leaked into the output")
	}
}

func TestReadCallsMissingFile(t *testing.T) {
	stages, slow := readCalls(t.TempDir())
	if stages == nil || slow == nil || len(stages) != 0 || len(slow) != 0 {
		t.Errorf("want empty non-nil, got %v %v", stages, slow)
	}
}

func TestBriefStateProblemsAndStages(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Failed)
	dir := filepath.Join(root, ".gophermind", f.RunID)
	os.MkdirAll(filepath.Join(dir, "_state"), 0o755)
	os.WriteFile(filepath.Join(dir, "_state", "calls.jsonl"), []byte(callsSample), 0o644)
	st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Stages) != 3 || len(st.SlowCalls) != 5 {
		t.Errorf("stages %d slow %d", len(st.Stages), len(st.SlowCalls))
	}
	if len(st.Problems) != 1 || st.Problems[0].Node != "fn-bye" || st.Problems[0].Class == "" || st.Problems[0].Reason == "" {
		t.Fatalf("problems %+v", st.Problems)
	}
	if want := nodeByID(t, st, "fn-bye").Attempts; st.Problems[0].Attempts != len(want) {
		t.Errorf("attempts %d vs %d", st.Problems[0].Attempts, len(want))
	}
}

func TestBriefStateNoCallsNoProblems(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	dir := filepath.Join(root, ".gophermind", f.RunID)
	if err := os.Remove(filepath.Join(dir, "_state", "calls.jsonl")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	st, _ := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	b, _ := json.Marshal(st)
	for _, k := range []string{`"stages":[]`, `"slow_calls":[]`, `"problems":[]`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing %s", k)
		}
	}
}
