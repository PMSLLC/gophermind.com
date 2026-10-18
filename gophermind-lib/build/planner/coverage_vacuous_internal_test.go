package planner

import (
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/acceptcheck"
	"gophermind/gophermind-lib/build/contract"
)

func vacuousReqs() []Requirement {
	return []Requirement{
		{ID: "C1", Kind: ReqConstraint, Text: "no gorm", Line: 1},
		{ID: "A1", Kind: ReqAcceptance, Text: "csvstat summarizes", Line: 2},
	}
}

func TestRootTestDefectsFlagsGoRunAsVacuous(t *testing.T) {
	tests := []RootTest{{Requirement: "A1", Name: "n", Command: "printf 'a,b\\n1,2\\n' | go run ./cmd/csvstat - | grep -q count"}}
	got := rootTestDefects(vacuousReqs(), tests, acceptcheck.Options{Bins: []string{"csvstat"}})
	want := []QualityItem{{Requirement: "A1", Findings: []string{"acceptance_vacuous"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defects = %+v, want %+v", got, want)
	}
}

func TestRootTestDefectsAcceptsTheBuiltBinary(t *testing.T) {
	tests := []RootTest{{Requirement: "A1", Name: "n", Command: "printf 'a,b\\n1,2\\n' | csvstat - | grep -q count"}}
	if got := rootTestDefects(vacuousReqs(), tests, acceptcheck.Options{Bins: []string{"csvstat"}}); len(got) != 0 {
		t.Fatalf("defects = %+v, want none", got)
	}
	// The same command with no built binary in the contract proves nothing.
	if got := rootTestDefects(vacuousReqs(), tests, acceptcheck.Options{}); len(got) != 1 {
		t.Fatalf("defects without a binary = %+v, want one", got)
	}
}

func TestRootTestDefectsLeavesToolCommandsAndConstraintsAlone(t *testing.T) {
	tests := []RootTest{
		{Requirement: "A1", Name: "b", Command: "go build ./... && go vet ./... && go test ./..."},
		{Requirement: "C1", Name: "c", Command: "! grep -rq gorm internal/"},
	}
	if got := rootTestDefects(vacuousReqs(), tests, acceptcheck.Options{}); len(got) != 0 {
		t.Fatalf("defects = %+v, want none", got)
	}
}

func TestContractBinsAreTheMainPackagesUnderCmd(t *testing.T) {
	c := &contract.Contracts{Functions: []contract.Function{
		{ID: "fn-main", Package: "cmd/csvstat", File: "cmd/csvstat/main.go"},
		{ID: "fn-run", Package: "cmd/csvstat", File: "cmd/csvstat/run.go"},
		{ID: "fn-x", Package: "cmd/other/sub", File: "cmd/other/sub/x.go"},
		{ID: "fn-s", Package: "internal/stats", File: "internal/stats/s.go"},
	}}
	if got, want := contractBins(c), []string{"csvstat", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bins = %v, want %v", got, want)
	}
	if got := contractBins(nil); len(got) != 0 {
		t.Fatalf("nil contract bins = %v", got)
	}
}

func TestBinName(t *testing.T) {
	for _, c := range []struct {
		clause, dir, want string
	}{
		{"main", "cmd/csvstat", "csvstat"}, {"main", "cmd/a/b", "a"}, {"main", ".", ""},
		{"stats", "cmd/csvstat", ""}, {"main", "internal/x", ""}, {"main", "cmd", ""},
	} {
		got, ok := BinName(c.clause, c.dir)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("BinName(%q, %q) = %q, %v", c.clause, c.dir, got, ok)
		}
	}
}

func TestVacuousRepairMessageNamesTheRule(t *testing.T) {
	if !strings.Contains(harnessContract(), "acceptance_vacuous") {
		t.Fatal("the harness prompt must explain the acceptance_vacuous finding")
	}
}
