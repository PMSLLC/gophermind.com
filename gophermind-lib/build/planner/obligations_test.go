package planner

import (
	"os"
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
	byID := map[string]TestObligation{}
	for _, o := range attached {
		byID[o.ID] = o
	}
	runs, greets := 0, 0
	for _, q := range reqs {
		if q.Kind != ReqTest {
			continue
		}
		o, ok := byID[q.ID]
		switch {
		case strings.HasPrefix(q.Text, "`Run("):
			runs++
			if !ok || o.Target != "fn-run" {
				t.Errorf("%s (%s) attached to %q, want fn-run", q.ID, q.Text, o.Target)
			}
		case strings.HasPrefix(q.Text, "`Greeting("):
			greets++
			if !ok || o.Target != "fn-greeting" {
				t.Errorf("%s (%s) attached to %q, want fn-greeting", q.ID, q.Text, o.Target)
			}
		default:
			if ok {
				t.Errorf("%s (%s) attached to %s, want unattached", q.ID, q.Text, o.Target)
			}
		}
		if ok && (o.Text != q.Text || o.Line != q.Line) {
			t.Errorf("%s carries %q line %d, want %q line %d", q.ID, o.Text, o.Line, q.Text, q.Line)
		}
	}
	if runs != 5 || greets != 2 {
		t.Fatalf("the fixture has %d Run and %d Greeting bullets, want 5 and 2", runs, greets)
	}
	var texts []string
	for _, q := range unattached {
		texts = append(texts, q.Text)
	}
	if len(unattached) != 2 || unattached[1].Text != "The build is reproducible." {
		t.Errorf("unattached = %q, want the bytes.Buffer bullet and the reproducible bullet", texts)
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
