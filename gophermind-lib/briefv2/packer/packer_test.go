package packer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

const contractsJSON = `{
 "spec_version": "2.0", "brief_id": "b", "revision": 0, "module": "example.com/m",
 "conventions": {"layout": ["x"], "naming": ["y"], "errors": "z"},
 "types": [
  {"id": "widget", "package": "greet", "file": "internal/greet/w.go", "decl": "// Widget is a thing.\ntype Widget struct {\n\tName string\n}", "uses": []},
  {"id": "opts", "package": "greet", "file": "internal/greet/o.go", "decl": "// Options configures Other.\ntype Options struct {\n\tLevel int\n}", "uses": []}
 ],
 "functions": [
  {"id": "fn-a", "package": "greet", "file": "internal/greet/a.go", "signature": "func MakeWidget(name string) Widget", "doc": "MakeWidget builds a Widget.", "uses": ["widget"], "component": "c"},
  {"id": "fn-b", "package": "greet", "file": "internal/greet/b.go", "signature": "func Other(o Options) error", "doc": "Other applies options. CANARY-sibling-doc", "uses": ["opts"], "component": "c"},
  {"id": "fn-c", "package": "greet", "file": "internal/greet/c.go", "signature": "func Third() int", "doc": "Third returns three.", "uses": [], "component": "c"}
 ],
 "components": []
}`

func contractsFixture(t *testing.T) *contract.Contracts {
	t.Helper()
	c, err := contract.Load([]byte(contractsJSON))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const baseTest = `package greet

import "testing"

func TestMakeWidget(t *testing.T) {
	w := MakeWidget("x")
	if w.Name != "x" {
		t.Errorf("Name = %q", w.Name)
	}
}
`

func baseNode() NodeView {
	return NodeView{
		ID: "n-a", FuncID: "fn-a", Package: "greet", File: "internal/greet/a.go",
		Signature:   "func MakeWidget(name string) Widget",
		Constraints: []string{"no global state"},
		TestFile:    "internal/greet/a_test.go", TestSource: baseTest,
	}
}

func lines(n int, prefix string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s line %03d some compiler output text\n", prefix, i)
	}
	return b.String()
}

func TestPackNeedToKnow(t *testing.T) {
	n := baseNode()
	n.Constraints = []string{"no global state"}
	n.DependencySignatures = []string{"func Helper() int"}
	p, err := Pack(n, contractsFixture(t), Inputs{Budget: 8000, Secrets: []string{"CANARY-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func MakeWidget(name string) Widget", "type Widget struct", "MakeWidget builds a Widget.", n.TestFile, "t.Errorf", "no global state", "func Helper() int"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, bad := range []string{"CANARY", "type Options", "func Other", "func Third"} {
		if strings.Contains(p.Text, bad) {
			t.Errorf("prompt leaks %q", bad)
		}
	}
	paths := map[string]bool{}
	for _, m := range regexp.MustCompile(`[A-Za-z0-9_./-]+\.go\b`).FindAllString(p.Text, -1) {
		paths[m] = true
	}
	if len(paths) != 2 || !paths[n.File] || !paths[n.TestFile] {
		t.Errorf("paths in prompt = %v, want only %s and %s", paths, n.File, n.TestFile)
	}
	if !strings.Contains(p.Text, "CONTRACT_PROBLEM: <one sentence>") || !strings.Contains(p.Text, "The reply is the whole Go file, nothing else. Import only the standard library, this module's own packages, and the modules in `<constraints>`") {
		t.Error("rule lines missing")
	}
	if strings.ContainsRune(p.Text, '—') {
		t.Error("em dash in prompt")
	}
}

func TestPackNeedToKnowCanariesInOtherNodes(t *testing.T) {
	// Data a wrong implementation could reach: another node's constraints and
	// test source, a repo file. None is given to Pack, so none may appear.
	p, err := Pack(baseNode(), contractsFixture(t), Inputs{Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"CANARY-sibling-constraint", "CANARY-sibling-test", "CANARY-repo", "CANARY-sibling-doc"} {
		if strings.Contains(p.Text, c) {
			t.Errorf("leak %s", c)
		}
	}
}

func tagOrder(t *testing.T, text string, tags []string) {
	t.Helper()
	last := -1
	for _, tag := range tags {
		re := regexp.MustCompile(`(?m)^<` + tag + `>$`)
		loc := re.FindStringIndex(text)
		if loc == nil {
			t.Fatalf("section %s missing", tag)
		}
		if loc[0] <= last {
			t.Errorf("section %s out of order", tag)
		}
		last = loc[0]
	}
}

func TestPackOrderAndFailureCap(t *testing.T) {
	n := baseNode()
	n.DependencySignatures = []string{"func Helper() int"}
	f := NewFailure([]string{"TestMakeWidget"}, lines(100, "x"), nil)
	if len(f.Lines) != 30 {
		t.Fatalf("lines = %d, want 30 (limited by 2048 bytes?)", len(f.Lines))
	}
	p, err := Pack(n, contractsFixture(t), Inputs{Failure: f, Notes: []string{"hint one"}, Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	tagOrder(t, p.Text, []string{"file", "signature", "contract", "dependency_signatures", "constraints", "revision_notes", "tests", "previous_failure"})

	q, err := Pack(n, contractsFixture(t), Inputs{Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"revision_notes", "previous_failure"} {
		if strings.Contains(q.Text, "<"+tag+">") {
			t.Errorf("empty %s must be omitted", tag)
		}
	}

	big := NewFailure(nil, lines(100, "y"), nil)
	total := 0
	for _, l := range big.Lines {
		total += len(l) + 1
	}
	if total > 2048 || len(big.Lines) == 0 {
		t.Errorf("failure bytes = %d in %d lines", total, len(big.Lines))
	}
	// Cut on a line boundary: every kept line is a whole original line.
	for _, l := range big.Lines {
		if !regexp.MustCompile(`^y line \d{3} some compiler output text$`).MatchString(l) {
			t.Errorf("partial line %q", l)
		}
	}
	// A multi-byte rune at the cut is never split.
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString(strings.Repeat("é", 20) + "\n")
	}
	mb := NewFailure(nil, b.String(), nil)
	for _, l := range mb.Lines {
		if !utf8.ValidString(l) || len([]rune(l)) != 20 {
			t.Errorf("split rune line %q", l)
		}
	}
	// A single line longer than the cap is dropped whole, never cut.
	long := NewFailure(nil, strings.Repeat("é", 2000)+"\nshort\n", nil)
	for _, l := range long.Lines {
		if !utf8.ValidString(l) {
			t.Error("invalid utf8")
		}
	}
}

func TestPackStripsSecretLines(t *testing.T) {
	out := "line one\nline two\nfound CANARY-secret here\nline four\nother-secret leaked\nprogress\rCANARY-secret\nline seven\n"
	f := NewFailure([]string{"TestOk", "Test-CANARY-secret"}, out, []string{"CANARY-secret", "", "other-secret"})
	joined := strings.Join(f.Lines, "\n") + strings.Join(f.Names, "\n")
	if strings.Contains(joined, "CANARY-secret") || strings.Contains(joined, "other-secret") {
		t.Fatalf("secret survived: %q", joined)
	}
	want := []string{"line one", "line two", "line four", "line seven"}
	if strings.Join(f.Lines, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q", f.Lines)
	}
	if len(f.Names) != 1 || f.Names[0] != "TestOk" {
		t.Errorf("names = %q", f.Names)
	}
	// An empty secret must not delete everything.
	g := NewFailure(nil, "a\nb\n", []string{""})
	if len(g.Lines) != 2 {
		t.Errorf("empty secret dropped lines: %q", g.Lines)
	}
	if g.Empty() || !NewFailure(nil, "", nil).Empty() {
		t.Error("Empty wrong")
	}
	if names := NewFailure([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, "", nil).Names; len(names) != 8 {
		t.Errorf("names = %d, want 8", len(names))
	}

	// Pack refuses a secret that sits in an input it does not strip.
	n := baseNode()
	n.TestSource = baseTest + "// CANARY-secret\n"
	_, err := Pack(n, contractsFixture(t), Inputs{Budget: 8000, Secrets: []string{"CANARY-secret"}})
	if err == nil || strings.Contains(err.Error(), "CANARY-secret") {
		t.Errorf("err = %v, want a refusal without the secret", err)
	}
	// A failure built without the secrets list is still stripped by Pack.
	raw := Failure{Names: []string{"TestX"}, Lines: []string{"ok", "has CANARY-secret"}}
	p, err := Pack(baseNode(), contractsFixture(t), Inputs{Failure: raw, Notes: []string{"note CANARY-secret"}, Budget: 8000, Secrets: []string{"CANARY-secret"}})
	if err != nil || strings.Contains(p.Text, "CANARY-secret") {
		t.Errorf("Pack kept a secret: %v", err)
	}
}

func TestPackBudget(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 0}, {1, 2}, {4, 2}, {4000, 1100}, {4001, 1102}} { // ceil(1001*1.1) is 1102; the plan text says 1101 but its formula gives 1102
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	n := baseNode()
	f := NewFailure([]string{"TestMakeWidget"}, lines(20, "b"), nil)
	full, err := Pack(n, contractsFixture(t), Inputs{Failure: f, Budget: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Dropped) != 0 || full.Tokens != EstimateTokens(full.Bytes) || full.Bytes != len(full.Text) {
		t.Fatalf("full = %d bytes %d tokens dropped %v", full.Bytes, full.Tokens, full.Dropped)
	}
	at, err := Pack(n, contractsFixture(t), Inputs{Failure: f, Budget: full.Tokens})
	if err != nil || len(at.Dropped) != 0 {
		t.Errorf("at budget dropped %v (%v)", at.Dropped, err)
	}
	below, err := Pack(n, contractsFixture(t), Inputs{Failure: f, Budget: full.Tokens - 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(below.Dropped, ",") != "failure:lines10" {
		t.Errorf("dropped = %v, want [failure:lines10]", below.Dropped)
	}
	if _, err := Pack(n, contractsFixture(t), Inputs{Budget: 0}); err == nil || err.Error() != "packer: budget must be positive" {
		t.Errorf("err = %v", err)
	}
}

const commentedTest = `package greet

// TestMakeWidget checks the name.
// A long comment that should be cut when the budget is tight, padding padding
// padding padding padding padding padding padding padding padding padding.

import "testing"

// helper comment padding padding padding padding padding padding padding.


func TestMakeWidget(t *testing.T) {
	// explain padding padding padding padding padding padding padding padding
	w := MakeWidget("x")

	if w.Name != "x" {
		t.Errorf("Name = %q", w.Name)
	}
	// more padding padding padding padding padding padding padding padding
	if w.Name == "" {
		t.Errorf("empty")
	}
}
`

func TestPackDropOrder(t *testing.T) {
	c := contractsFixture(t)
	named := "func (w Widget) Label() string"
	pad := func(tag string) string {
		return "func " + tag + "Pad() int // " + strings.Repeat("padding ", 60)
	}
	n := baseNode()
	n.TestSource = commentedTest
	n.DependencySignatures = []string{pad("A"), named, pad("B"), pad("C")}
	fail := func(nl int) Failure { return NewFailure([]string{"TestMakeWidget"}, lines(nl, "z"), nil) }
	notes := []string{"remember the padding note padding padding padding padding padding"}
	stripped, ok := stripTestComments(commentedTest)
	if !ok || strings.Contains(stripped, "padding") {
		t.Fatalf("strip = %q", stripped)
	}

	f30 := fail(30)
	f10 := Failure{Names: f30.Names, Lines: f30.Lines[:10]}
	f0 := Failure{Names: f30.Names}
	kept := n
	kept.DependencySignatures = []string{named}
	strippedNode := n
	strippedNode.TestSource = stripped
	both := kept
	both.TestSource = stripped

	stages := []struct {
		n     NodeView
		in    Inputs
		wants []string
	}{
		{n, Inputs{Failure: f30, Notes: notes}, nil},
		{n, Inputs{Failure: f10, Notes: notes}, []string{"failure:lines10"}},
		{n, Inputs{Failure: f0, Notes: notes}, []string{"failure:lines10", "failure:names"}},
		{n, Inputs{Failure: f0}, []string{"failure:lines10", "failure:names", "notes"}},
		{strippedNode, Inputs{Failure: f0}, []string{"failure:lines10", "failure:names", "notes", "test_comments"}},
		{both, Inputs{Failure: f0}, []string{"failure:lines10", "failure:names", "notes", "test_comments", "dep_sig:3"}},
	}
	// Failure lines are cut to 10 but "failure:names" means the lines are gone
	// and only names stay; the stage inputs above hold exactly that.
	for i, s := range stages {
		s.in.Budget = 100000
		base, err := Pack(s.n, c, s.in)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && base.Tokens >= stagesTokens(t, c, stages[i-1].n, stages[i-1].in) {
			t.Fatalf("stage %d does not shrink the prompt", i)
		}
		in := Inputs{Failure: f30, Notes: notes, Budget: base.Tokens}
		got, err := Pack(n, c, in)
		if err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
		if strings.Join(got.Dropped, ",") != strings.Join(s.wants, ",") {
			t.Errorf("stage %d dropped %v, want %v", i, got.Dropped, s.wants)
		}
		if got.Tokens > base.Tokens {
			t.Errorf("stage %d over budget", i)
		}
		for _, must := range []string{"func MakeWidget(name string) Widget", "type Widget struct", "MakeWidget builds a Widget.", "no global state", "TestMakeWidget", `t.Errorf("Name = %q"`, `t.Errorf("empty")`} {
			if !strings.Contains(got.Text, must) {
				t.Errorf("stage %d lost %q", i, must)
			}
		}
		if strings.Contains(strings.Join(got.Dropped, ","), "dep_sig") && !strings.Contains(got.Text, named) {
			t.Errorf("stage %d dropped a named dependency", i)
		}
	}
}

func stagesTokens(t *testing.T, c *contract.Contracts, n NodeView, in Inputs) int {
	t.Helper()
	in.Budget = 100000
	p, err := Pack(n, c, in)
	if err != nil {
		t.Fatal(err)
	}
	return p.Tokens
}

func TestPackPaddedSignatures(t *testing.T) {
	n := baseNode()
	n.Signature = "func MakeWidget(name string) Widget"
	// Two signatures the contract slice names (Widget, MakeWidget), and padding.
	named1 := "// Widget helpers.\nfunc (w Widget) Label() string"
	var deps []string
	for i := 0; i < 400; i++ {
		deps = append(deps, fmt.Sprintf("// Padding%d does padding.\nfunc Padding%d(a, b, c, d, e int) (int, error) // %s", i, i, strings.Repeat("x", 60)))
	}
	deps[10] = named1
	deps[300] = "func MakeWidget(name string, extra int) Widget"
	total := 0
	for _, d := range deps {
		total += len(d)
	}
	if total < 40*1024 {
		t.Fatalf("padding %d bytes, want at least 40 KiB", total)
	}
	n.DependencySignatures = deps
	p, err := Pack(n, contractsFixture(t), Inputs{Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if p.Tokens > 8000 {
		t.Errorf("tokens = %d", p.Tokens)
	}
	for _, want := range []string{"func (w Widget) Label() string", "func MakeWidget(name string, extra int) Widget"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("named signature %q dropped", want)
		}
	}
	if strings.Count(p.Text, "does padding.") >= 390 {
		t.Error("padding not cut")
	}
	if len(p.Dropped) == 0 || !strings.HasPrefix(p.Dropped[len(p.Dropped)-1], "dep_sig:") {
		t.Errorf("dropped = %v", p.Dropped)
	}
	// The cut runs from the last block to the first: early padding survives.
	if !strings.Contains(p.Text, "Padding0 ") || strings.Contains(p.Text, "Padding399 ") {
		t.Error("drop order is not last to first")
	}
}

func TestPackFloorOverBudget(t *testing.T) {
	n := baseNode()
	n.TestSource = baseTest + "\n" + strings.Repeat("func TestPadding(t *testing.T) { t.Errorf(\"padding padding\") }\n", 40)
	n.Constraints = []string{"CANARY-prompt-secret-constraint"}
	p, err := Pack(n, contractsFixture(t), Inputs{Budget: 300})
	var floor *ErrFloorOverBudget
	if !errors.As(err, &floor) {
		t.Fatalf("err = %v", err)
	}
	if floor.Tokens <= floor.Budget || floor.Budget != 300 {
		t.Errorf("floor = %+v", floor)
	}
	if p.Text != "" || len(p.Dropped) != 0 || p.Bytes != 0 {
		t.Error("partial result returned")
	}
	for _, bad := range []string{"CANARY", "MakeWidget", "padding"} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("error text contains %q", bad)
		}
	}
}

func TestPackMaxTokens(t *testing.T) {
	for _, c := range []struct {
		ctx, prompt, want int
		ok                bool
	}{{32768, 1000, 4096, true}, {2000, 1000, 1000, true}, {1200, 1000, 200, false}, {0, 5000, 4096, true}, {1512, 1000, 512, true}} {
		got, ok := MaxTokens(c.ctx, c.prompt)
		if got != c.want || ok != c.ok {
			t.Errorf("MaxTokens(%d,%d) = %d,%v want %d,%v", c.ctx, c.prompt, got, ok, c.want, c.ok)
		}
	}
}

func TestNoPromptTextPersisted(t *testing.T) {
	const canary = "CANARY-prompt"
	n := baseNode()
	n.TestSource = baseTest + "// " + canary + "\n"
	p, err := Pack(n, contractsFixture(t), Inputs{Notes: []string{"note " + canary}, Budget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, canary) {
		t.Fatal("canary not in prompt text")
	}
	forms := fmt.Sprintf("%v %+v %#v %s %q %d %x", p, p, p, p, p, p, p) + fmt.Sprintf("%v %+v %s", &p, &p, &p)
	js, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{forms, string(js)} {
		if strings.Contains(s, canary) {
			t.Errorf("canary in %q", s)
		}
		if !strings.Contains(s, p.SHA256) || !strings.Contains(s, fmt.Sprint(p.Bytes)) {
			t.Errorf("sha or size missing in %q", s)
		}
	}

	// Through a real router, ledger and database file.
	cfg := settings.Default()
	fake := provider.NewFake("mini", []provider.ModelInfo{{ID: "qwen3.6:35b-a3b", ContextTokens: 32768}},
		func(_ int, req provider.Request) (provider.Response, error) {
			return provider.Response{Text: "package greet\n", Model: req.Model}, nil
		})
	path := filepath.Join(t.TempDir(), "bb.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	led := ledger.NewSQLite(d)
	sink := events.NewCollector()
	r := router.New(cfg, map[string]provider.Provider{"mini": fake}, led, sink)
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: SystemPrefix + "implement:fn-a"},
		{Role: provider.RoleUser, Content: p.Text},
	}, MaxTokens: 512}
	if _, err := r.Call(context.Background(), router.CallInfo{RunID: "r", Stage: "implement:fn-a", NodeID: "fn-a", Tier: router.TierStrong, Scope: router.ScopeNode}, req); err != nil {
		t.Fatal(err)
	}
	if got := fake.Requests(); len(got) != 1 || !strings.Contains(got[0].Messages[1].Content, canary) {
		t.Fatal("the provider did not receive the prompt")
	}
	rows, err := led.List(context.Background(), "r", ledger.Filter{})
	if err != nil || len(rows) != 1 || rows[0].PromptSHA256 == "" || rows[0].PromptBytes < p.Bytes {
		t.Fatalf("ledger rows = %+v (%v)", rows, err)
	}
	rowJSON, _ := json.Marshal(rows)
	evJSON, _ := json.Marshal(sink.Events())
	if strings.Contains(string(rowJSON), canary) || strings.Contains(string(evJSON), canary) {
		t.Error("canary in ledger rows or events")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), canary) {
			t.Errorf("canary in %s", filepath.Base(f))
		}
	}
}
