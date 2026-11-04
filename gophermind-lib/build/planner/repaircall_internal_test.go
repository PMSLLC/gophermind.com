package planner

import (
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/router"
)

func TestRepairSpecAddsTheFixStage(t *testing.T) {
	cs := callSpec{stage: "testwrite:fn-x", taskType: "testwrite", nodeID: "fn-x", nodeClass: "pure", tier: router.TierStandard, scope: router.ScopeNode, maxTokens: 7}
	got := repairSpec(cs)
	cs.stage = "testwrite:fn-x:fix"
	if !reflect.DeepEqual(got, cs) {
		t.Fatalf("got %+v", got)
	}
}

func TestRepairPromptHoldsTheFileAndTheErrorsAndNothingElse(t *testing.T) {
	got, err := renderRepairPrompt("package p\n\nfunc TestX(t *testing.T) {}\n", "unused import at line 4: \"fmt\" imported and not used")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "```go\npackage p") {
		t.Errorf("file missing: %q", got)
	}
	if !strings.Contains(got, "unused import at line 4") {
		t.Errorf("error line missing: %q", got)
	}
	end := "Change only what the errors require; keep every test case, helper and assertion."
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), end) {
		t.Errorf("closing sentence missing: %q", got)
	}
	for _, bad := range []string{"brief", "contract"} {
		if strings.Contains(got, bad) {
			t.Errorf("prompt mentions %q", bad)
		}
	}
}
