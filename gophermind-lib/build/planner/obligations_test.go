package planner

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

// helloTestsBrief is the hello fixture brief with the Tests section of the
// library brief hello v1.1 appended.
func helloTestsBrief(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/hello/brief.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.ReadFile("testdata/hello/tests-section.md")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, s...)
}

func helloContracts() *contract.Contracts {
	return &contract.Contracts{Module: "hello", Functions: []contract.Function{
		{ID: "fn-greeting", Package: "internal/greet", File: "internal/greet/greet.go", Signature: "func Greeting(name string) string"},
		{ID: "fn-run", Package: "internal/cli", File: "internal/cli/cli.go", Signature: "func Run(args []string, stdout, stderr io.Writer) int"},
		{ID: "fn-main", Package: "cmd/hello", File: "cmd/hello/main.go", Signature: "func main()"},
	}}
}

func TestObligationsAttachByCallee(t *testing.T) {
	src := append(helloTestsBrief(t), []byte("- The build is reproducible.\n")...)
	reqs := ParseRequirements(src)
	attached, unattached := testObligations(reqs, helloContracts())
	got := map[string]string{}
	for _, o := range attached {
		got[o.ID] = o.Target
	}
	want := map[string]string{
		"T1": "fn-greeting", "T2": "fn-greeting", "T3": "fn-greeting", "T4": "fn-greeting",
		"T6": "fn-run", "T7": "fn-run", "T8": "fn-run", "T9": "fn-run",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attached = %v, want %v", got, want)
	}
	byID := map[string]Requirement{}
	for _, q := range reqs {
		byID[q.ID] = q
	}
	for _, o := range attached {
		if q := byID[o.ID]; o.Text != q.Text || o.Line != q.Line {
			t.Errorf("%s carries %q line %d, want %q line %d", o.ID, o.Text, o.Line, q.Text, q.Line)
		}
	}
	var ids []string
	for _, q := range unattached {
		ids = append(ids, q.ID)
	}
	if !reflect.DeepEqual(ids, []string{"T5", "T10"}) || unattached[1].Text != "The build is reproducible." {
		t.Errorf("unattached = %v, want T5 (no call) and T10 (the reproducible bullet)", ids)
	}
}

func TestObligationsAmbiguousCalleeIsUnattached(t *testing.T) {
	reqs := []Requirement{{ID: "T1", Kind: ReqTest, Text: "`Run(nil, stdout, stderr)` returns 0.", Line: 3}}
	c := &contract.Contracts{Module: "m", Functions: []contract.Function{
		{ID: "fn-run", Package: "internal/cli", File: "internal/cli/cli.go", Signature: "func Run(args []string, stdout, stderr io.Writer) int"},
		{ID: "fn-server-run", Package: "internal/server", File: "internal/server/server.go", Signature: "func (s *Server) Run(ctx context.Context) error"},
	}}
	attached, unattached := testObligations(reqs, c)
	if len(attached) != 0 || len(unattached) != 1 || unattached[0].ID != "T1" {
		t.Fatalf("attached %+v, unattached %+v; want T1 unattached", attached, unattached)
	}
}

func TestObligationCalleeOfAMethodCall(t *testing.T) {
	reqs := []Requirement{{ID: "T1", Kind: ReqTest, Text: "Calling `cli.Run([]string{\"a\"}, out, errw)` returns 0.", Line: 3}}
	attached, _ := testObligations(reqs, helloContracts())
	if len(attached) != 1 || attached[0].Target != "fn-run" {
		t.Fatalf("attached %+v, want T1 on fn-run", attached)
	}
}

func TestRequirementsTextLeavesOutTestRequirements(t *testing.T) {
	got := requirementsText([]Requirement{
		{ID: "C1", Kind: ReqConstraint, Text: "Standard library only.", Line: 3},
		{ID: "T1", Kind: ReqTest, Text: "`Run(nil, stdout, stderr)` returns 0.", Line: 9},
	})
	if strings.Contains(got, "T1") || !strings.Contains(got, "C1") {
		t.Fatalf("coverage requirement list = %q, want C1 without T1", got)
	}
}
