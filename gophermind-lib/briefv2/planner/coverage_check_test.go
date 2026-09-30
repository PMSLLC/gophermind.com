package planner_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

var testReqs = []planner.Requirement{
	{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Says hello.", Line: 10},
	{ID: "C1", Kind: planner.ReqConstraint, Text: "Standard library only.", Line: 20},
	{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds.", Line: 30},
}

var testNodes = []planner.PlanNode{
	{ID: "gm-1", Kind: tree.KindRoot, Title: "Root"},
	{ID: "greeting", Kind: tree.KindComponent, Parent: "gm-1", Title: "Greeting"},
	{ID: "types", Kind: tree.KindComponent, Parent: "gm-1", Title: "Types"},
	{ID: "fn-greet", Kind: tree.KindFunction, Parent: "greeting", Title: "Greet", File: "internal/greet/greet.go"},
}

func build(name string) planner.RootTest {
	return planner.RootTest{Requirement: "A1", Name: name, Given: "the tree is verified", Expect: "exit 0", Command: "go build ./..."}
}

func TestCheckCoverage(t *testing.T) {
	full := planner.CoverageReply{
		Map: []planner.MapEntry{
			{Requirement: "F1", Nodes: []string{"greeting"}},
			{Requirement: "C1", Nodes: []string{"fn-greet"}},
			{Requirement: "A1", Nodes: nil},
		},
		RootTests: []planner.RootTest{build("builds")},
	}
	cases := []struct {
		name    string
		reply   planner.CoverageReply
		wantGap map[string]string // requirement id -> reason
	}{
		{"everything covered", full, map[string]string{}},
		{"a requirement missing from the reply",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "not mapped"}},
		{"only node does not exist",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-nope"}}}, RootTests: full.RootTests},
			map[string]string{"C1": "names unknown node fn-nope; no node and no root test"}},
		{"constraint with neither node nor root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{}}}, RootTests: full.RootTests},
			map[string]string{"C1": "no node and no root test"}},
		{"constraint covered by a root test alone",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}},
				RootTests: []planner.RootTest{build("builds"), {Requirement: "C1", Name: "no third-party modules", Command: "test -z \"$(go list -m all | tail -n +2)\""}}},
			map[string]string{}},
		{"acceptance bullet with nodes but no root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}, {Requirement: "A1", Nodes: []string{"fn-greet"}}}},
			map[string]string{"A1": "acceptance bullet has no root test"}},
		{"feature mapped only to the root node",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"gm-1"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "the root node alone covers nothing; no covering node"}},
		{"feature mapped to a component with no functions",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"types"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "component types has no functions; no covering node"}},
		{"a feature is not covered by a root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-greet"}}},
				RootTests: []planner.RootTest{build("builds"), {Requirement: "F1", Name: "greets", Command: "go run ./cmd/greet"}}},
			map[string]string{"F1": "no covering node"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			covered, gaps := planner.CheckCoverage(testReqs, testNodes, c.reply)
			got := map[string]string{}
			for _, g := range gaps {
				got[g.Requirement] = g.Reason
				if g.Text == "" || g.Line == 0 {
					t.Errorf("gap %s lost its text or line: %+v", g.Requirement, g)
				}
			}
			if !reflect.DeepEqual(got, c.wantGap) {
				t.Fatalf("gaps = %v, want %v", got, c.wantGap)
			}
			if len(covered)+len(gaps) != len(testReqs) {
				t.Fatalf("%d covered + %d gaps, want %d requirements in total", len(covered), len(gaps), len(testReqs))
			}
		})
	}

	covered, _ := planner.CheckCoverage(testReqs, testNodes, full)
	want := []planner.Covered{
		{Requirement: "F1", Nodes: []string{"greeting"}, RootTests: []string{}},
		{Requirement: "C1", Nodes: []string{"fn-greet"}, RootTests: []string{}},
		{Requirement: "A1", Nodes: []string{}, RootTests: []string{"builds"}},
	}
	if !reflect.DeepEqual(covered, want) {
		t.Fatalf("covered = %+v, want %+v", covered, want)
	}
}

func TestParseCoverageReply(t *testing.T) {
	ok := `{"map":[{"requirement":"C1","nodes":["fn-greet"]}],"root_tests":[{"requirement":"A1","name":"builds","given":"g","expect":"e","command":"go build ./..."}],"functions":[]}`
	r, err := planner.ParseCoverageReply(ok, testReqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Map) != 1 || len(r.RootTests) != 1 || r.RootTests[0].Command != "go build ./..." {
		t.Fatalf("reply = %+v", r)
	}
	bad := []struct{ name, text, wantErr string }{
		{"not json", `here is the mapping`, "not valid JSON"},
		{"unknown requirement in map", `{"map":[{"requirement":"C9","nodes":[]}]}`, `unknown requirement (2 bytes)`},
		{"unknown requirement in root test", `{"root_tests":[{"requirement":"A9","name":"x","command":"true"}]}`, `unknown requirement (2 bytes)`},
		{"root test without a name", `{"root_tests":[{"requirement":"A1","name":" ","command":"true"}]}`, "has no name"},
		{"root test without a command", `{"root_tests":[{"requirement":"A1","name":"builds","command":""}]}`, "has no command"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := planner.ParseCoverageReply(c.text, testReqs)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	a := planner.CoverageReply{
		Map:       []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-a"}}, {Requirement: "F1", Nodes: nil}},
		RootTests: []planner.RootTest{build("builds")},
	}
	b := planner.CoverageReply{
		Map:       []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-a", "fn-b"}}, {Requirement: "A1", Nodes: []string{}}},
		RootTests: []planner.RootTest{build("builds"), build("builds twice")},
	}
	got := a.Merge(b)
	want := planner.CoverageReply{
		Map: []planner.MapEntry{
			{Requirement: "C1", Nodes: []string{"fn-a", "fn-b"}},
			{Requirement: "F1", Nodes: []string{"greeting"}},
			{Requirement: "A1", Nodes: []string{}},
		},
		RootTests: []planner.RootTest{build("builds"), build("builds twice")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %+v\nwant   %+v", got, want)
	}
	if len(a.Map[0].Nodes) != 1 {
		t.Fatal("Merge changed its receiver")
	}
}

func TestPathWarnings(t *testing.T) {
	src := []byte("Packages: `cmd/greet`, `internal/greet`, `internal/store/`, `internal/...`, `cmd/greet` again, `go build ./cmd/greet`.\n")
	nodes := []planner.PlanNode{
		{ID: "fn-greet", Kind: tree.KindFunction, Parent: "greeting", File: "internal/greet/greet.go"},
		{ID: "fn-main", Kind: tree.KindFunction, Parent: "greeting", File: "cmd/greeter/main.go"},
		{ID: "store", Kind: tree.KindComponent, File: "internal/store/store.go"},
	}
	got := planner.PathWarnings(src, nodes)
	want := []string{
		"`cmd/greet` is named in the brief but no function file is under it",
		"`internal/store/` is named in the brief but no function file is under it",
		"`internal/...` is named in the brief but no function file is under it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q\nwant       %q", got, want)
	}
}

func TestCommandWarnings(t *testing.T) {
	reqs := []planner.Requirement{
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds."},
		{ID: "A2", Kind: planner.ReqAcceptance, Text: "`go vet ./...` reports nothing."},
		{ID: "A3", Kind: planner.ReqAcceptance, Text: "Two users cannot read each other's data: `GET` returns 404."},
		{ID: "A4", Kind: planner.ReqAcceptance, Text: "`go test ./...` passes."},
		{ID: "C1", Kind: planner.ReqConstraint, Text: "`gofmt` clean."},
	}
	reply := planner.CoverageReply{RootTests: []planner.RootTest{
		{Requirement: "A1", Name: "builds", Command: "sh -c 'go build ./... && echo ok'"},
		{Requirement: "A2", Name: "vet", Command: "go vet ./cmd/..."},
		{Requirement: "A3", Name: "isolation", Command: "go test ./internal/tenancy"},
		{Requirement: "C1", Name: "fmt", Command: "true"},
	}}
	got := planner.CommandWarnings(reqs, reply)
	want := []string{"A2: no root test runs the command the bullet begins with (`go vet ./...`)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// The plan the v1 planner produced for AI Venture Studio on 2026-09-29 was
// approved with most of the brief's constraints and all of its acceptance
// bullets dropped. The checker must say so. See testdata/aivs/README.md.
func TestGoldenFailingPlan(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	reqs := planner.ParseRequirements(src)
	var nodes []planner.PlanNode
	readJSON(t, "testdata/aivs/nodes.json", &nodes)
	raw, err := os.ReadFile("testdata/aivs/mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := planner.ParseCoverageReply(string(raw), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 121 {
		t.Fatalf("nodes.json has %d nodes, want 121 (1 root, 21 phases, 99 steps)", len(nodes))
	}

	covered, gaps := planner.CheckCoverage(reqs, nodes, reply)
	var ids []string
	text := map[string]string{}
	for _, g := range gaps {
		ids = append(ids, g.Requirement)
		text[g.Requirement] = g.Text
	}
	sort.Strings(ids)
	want := []string{"A1", "A10", "A11", "A12", "A2", "A3", "A4", "A5", "A6", "A7", "A8", "A9", "C1", "C10", "C2", "C4", "C5", "C7", "C9"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("gaps = %v\nwant   %v", ids, want)
	}
	if len(covered) != 22 {
		t.Fatalf("covered = %d, want 22 (19 features and C3, C6, C8)", len(covered))
	}
	for id, part := range map[string]string{
		"C1":  "`gofmt` and `go vet` clean",
		"C4":  "A test scans all SQL under `internal/`",
		"C9":  "Handlers never exceed 60 lines",
		"A8":  "`stage.advanced` in that order",
		"A9":  "Financial round trip",
		"A10": "`?as_of=` before the transfer",
		"A12": "returns 404",
	} {
		if !strings.Contains(text[id], part) {
			t.Errorf("gap %s should be the requirement containing %q, got %q", id, part, text[id])
		}
	}

	warnings := planner.PathWarnings(src, nodes)
	joined := strings.Join(warnings, "\n")
	for _, tok := range []string{"internal/tenancy", "internal/httpapi", "internal/openapi", "internal/pnl"} {
		if !strings.Contains(joined, "`"+tok+"`") {
			t.Errorf("no path warning for %s; warnings:\n%s", tok, joined)
		}
	}
	// The v1 plan did put its main package under cmd/venture-server (beside a
	// stray cmd/fake-llm), so that path is not among the warnings.
	if strings.Contains(joined, "`cmd/venture-server`") {
		t.Errorf("cmd/venture-server has a function file under it and must not be warned about")
	}
	if len(warnings) != 18 {
		t.Errorf("%d path warnings, want 18:\n%s", len(warnings), joined)
	}
}

func TestStrayCommandWarnings(t *testing.T) {
	src := []byte("The binary is `cmd/greeter`. Run `cmd/tool/main.go` by hand. Not a path: `cmd`.")
	nodes := []planner.PlanNode{
		{ID: "fn-main", Kind: tree.KindFunction, File: "cmd/greeter/main.go"},
		{ID: "fn-tool", Kind: tree.KindFunction, File: "cmd/tool/main.go"},
		{ID: "fn-a", Kind: tree.KindFunction, File: "cmd/server/main.go"},
		{ID: "fn-b", Kind: tree.KindFunction, File: "cmd/server/router.go"},
		{ID: "fn-c", Kind: tree.KindFunction, File: "internal/cmd/x.go"},
		{ID: "comp", Kind: tree.KindComponent, File: "cmd/ghost/x.go"},
	}
	got := planner.StrayCommandWarnings(src, nodes)
	want := []string{"`cmd/server` holds plan files but the brief never names it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

// The plan that motivated the coverage stage put a tool in cmd/fake-llm
// although the brief names one binary, cmd/venture-server. (That plan also
// touched cmd/server, but never as a step's first file, which is the only one
// nodes.json keeps.)
func TestGoldenPlanHasAStrayCommand(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []planner.PlanNode
	readJSON(t, "testdata/aivs/nodes.json", &nodes)
	got := planner.StrayCommandWarnings(src, nodes)
	want := []string{"`cmd/fake-llm` holds plan files but the brief never names it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}
