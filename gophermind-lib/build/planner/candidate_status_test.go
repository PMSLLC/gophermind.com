package planner_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/planner"
)

var candidateStatusRE = regexp.MustCompile(`^(accepted|accepted_with_repairs:[a-z_]+(,[a-z_]+)*|rejected:[a-z_]+)$`)

func TestCandidateStatusRecordsRepairsAndRejections(t *testing.T) {
	// The three value shapes, from note and settle alone.
	if got := planner.CandidateStatusAfter(nil, nil); got != "accepted" {
		t.Errorf("no repair = %q, want accepted", got)
	}
	kinds := []string{"fixed_interface_missing", "entry_extra_function_dropped", "fixed_interface_missing"}
	if got, want := planner.CandidateStatusAfter(kinds, nil), "accepted_with_repairs:entry_extra_function_dropped,fixed_interface_missing"; got != want {
		t.Errorf("repairs = %q, want %q", got, want)
	}
	conflict := errors.New("contract: conflicting declaration of fn-a by component c")
	if got := planner.CandidateStatusAfter(kinds, conflict); got != "rejected:conflict" {
		t.Errorf("conflict = %q, want rejected:conflict", got)
	}
	if got := planner.CandidateStatusAfter(nil, fmt.Errorf("contract: component c does not declare x")); got != "rejected:contract" {
		t.Errorf("contract error = %q, want rejected:contract", got)
	}

	// The hello rig: every pass has a status, and a pass that carried a
	// contract warning names its kind.
	g := newRigFor(t, approving(), hello, helloID)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	raw, err := os.ReadFile(filepath.Join(g.runDir, "_state", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Done             []string          `json:"done"`
		OutlineBatchNext int               `json:"outline_batch_next"`
		CandidateStatus  map[string]string `json:"candidate_status"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"outline"}, st.Done...)
	for i := 0; i < st.OutlineBatchNext; i++ {
		want = append(want, fmt.Sprintf("outline:%d", i))
	}
	for _, pass := range want {
		if _, ok := st.CandidateStatus[pass]; !ok {
			t.Errorf("candidate_status has no entry for pass %q: %v", pass, st.CandidateStatus)
		}
	}
	for pass, v := range st.CandidateStatus {
		if !candidateStatusRE.MatchString(v) {
			t.Errorf("candidate_status[%q] = %q, not one of the three shapes", pass, v)
		}
	}
	// A warning names its pass by stage (contract:<id>) or by the component id
	// after its kind ("entry_extra_function_dropped: entry-point: fn-run").
	for _, e := range g.sink.OfKind(events.KindWarning) {
		kind, rest, ok := strings.Cut(e.Message, ": ")
		if !ok {
			continue
		}
		pass := strings.TrimPrefix(e.Stage, "contract:")
		if e.Stage == "contract" {
			pass, _, _ = strings.Cut(rest, ": ")
		}
		v, known := st.CandidateStatus[pass]
		if !known || !strings.HasPrefix(e.Stage, "contract") {
			continue
		}
		if !strings.Contains(v, kind) {
			t.Errorf("pass %q carried the warning %s but its status is %q", pass, kind, v)
		}
	}
	t.Logf("hello candidate_status: %v", st.CandidateStatus)
}

func TestPlanStatusTotalExcludesTestObligations(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	reqs := []planner.Requirement{
		{ID: "C1", Kind: planner.ReqConstraint, Text: "a constraint", Line: 1},
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "an acceptance", Line: 2},
		{ID: "T1", Kind: planner.ReqTest, Text: "a test obligation", Line: 3},
	}
	raw, err := json.Marshal(reqs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.runDir, "requirements.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil || st.Requirements != 2 {
		t.Errorf("status requirements = %d, %v; want 2 (the test obligation is not counted)", st.Requirements, err)
	}
}

// A fixed-interface stop stores the rejected status on the last saved state:
// the refused pass's functions are not in the stored doc, so a resume sees the
// doc it saw before that pass.
func TestFixedInterfaceStopStoresTheStateBeforeTheRefusedPass(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID,
		variant(t, map[string]string{"contract.cli-logic.2.txt": `{"types": [], "functions": [{"id": "fn-stray-helper", "package": "internal/cli",` +
			` "file": "internal/cli/stray.go", "signature": "func strayHelper() int", "doc": "Returns zero.", "uses": []}], "more": false}`}))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "does not declare the fixed interface") {
		t.Fatalf("plan error = %v, want the fixed interface error", err)
	}
	raw, err := os.ReadFile(filepath.Join(g.runDir, "_state", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Doc             map[string]any    `json:"doc"`
		CandidateStatus map[string]string `json:"candidate_status"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	fns, _ := st.Doc["functions"].([]any)
	for _, f := range fns {
		if m, _ := f.(map[string]any); m["id"] == "fn-stray-helper" {
			t.Errorf("the stored doc holds fn-stray-helper, a function of the refused pass")
		}
	}
	if got := st.CandidateStatus["cli-logic"]; got != "rejected:fixed_interface_missing" {
		t.Errorf("candidate_status[cli-logic] = %q, want rejected:fixed_interface_missing (all: %v)", got, st.CandidateStatus)
	}
}
