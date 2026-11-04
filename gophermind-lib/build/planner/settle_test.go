package planner_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/planner"
)

const fixStage = "testwrite:fn-greet:fix"

// goodGreetReply is the greeter's accepted test-writer reply.
func goodGreetReply(t *testing.T) string {
	t.Helper()
	return string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))
}

// fencedFile is the fenced go block of a reply, the fences included, as a
// repair reply that holds only the file.
func fencedFile(t *testing.T, reply string) string {
	t.Helper()
	i := strings.Index(reply, "```go")
	if i < 0 {
		t.Fatal("reply has no fenced go block")
	}
	return reply[i:]
}

// edited returns reply with old replaced by new once, failing when old is absent.
func edited(t *testing.T, reply, old, new string) string {
	t.Helper()
	out := strings.Replace(reply, old, new, 1)
	if out == reply {
		t.Fatalf("fixture has no %q", old)
	}
	return out
}

// withUnused puts an unused variable as the first statement of TestGreet.
func withUnused(t *testing.T, reply string) string {
	return edited(t, reply, "func TestGreet(t *testing.T) {\n", "func TestGreet(t *testing.T) {\n\tunused := 1\n")
}

// withTypeError compares the string result with an int, which no mechanical fix touches.
func withTypeError(t *testing.T, reply string) string {
	return edited(t, reply, "if got != c.want {", "if got != 1 {")
}

// lineOf is the 1-based line of the first line of the reply's Go file holding s.
func lineOf(t *testing.T, reply, s string) int {
	t.Helper()
	body := strings.TrimPrefix(fencedFile(t, reply), "```go\n")
	for i, l := range strings.Split(body, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	t.Fatalf("no line holds %q", s)
	return 0
}

// greetRecord is the accepted node record of fn-greet in testwriter.json.
func greetRecord(t *testing.T, g *rig) (fixes []string, repaired bool) {
	t.Helper()
	var st struct {
		Nodes map[string]struct {
			Fixes    []string `json:"fixes"`
			Repaired bool     `json:"repaired"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(g.read("_state/testwriter.json"), &st); err != nil {
		t.Fatal(err)
	}
	n, ok := st.Nodes["fn-greet"]
	if !ok {
		t.Fatal("testwriter.json has no fn-greet record")
	}
	return n.Fixes, n.Repaired
}

func hasWarning(g *rig, msg string) bool {
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if e.Message == msg {
			return true
		}
	}
	return false
}

func greetRefusals(g *rig) []planner.Refusal {
	var out []planner.Refusal
	for _, r := range planner.ReadRefusals(g.runDir) {
		if r.Stage == "testwrite:fn-greet" {
			out = append(out, r)
		}
	}
	return out
}

func writtenGreetTest(t *testing.T, g *rig) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(g.repo, "internal/greet/fn_greet_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// The model's file has an unused variable: it compiles after the mechanical fix, no repair call.
func TestSettleFixesAnUnusedVariableWithoutACall(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": withUnused(t, goodGreetReply(t))}))
	g.mustPlan(planner.Options{})
	fixes, repaired := greetRecord(t, g)
	if len(fixes) != 1 || fixes[0] != "unused variable unused" || repaired {
		t.Errorf("record fixes = %q, repaired = %v; want [unused variable unused], false", fixes, repaired)
	}
	if n := count(g.stagesCalled(), fixStage); n != 0 {
		t.Errorf("%s called %d times, want 0", fixStage, n)
	}
	if n := count(g.stagesCalled(), "testwrite:fn-greet"); n != 1 {
		t.Errorf("testwrite:fn-greet called %d times, want 1", n)
	}
	if src := writtenGreetTest(t, g); !strings.Contains(src, "_ = unused") {
		t.Errorf("written file has no _ = unused:\n%s", src)
	}
	if !hasWarning(g, "test_file_mechanical_fix: unused variable unused in the test file of node fn-greet") {
		t.Error("no test_file_mechanical_fix warning")
	}
	if r := greetRefusals(g); len(r) != 0 {
		t.Errorf("refusals = %+v, want none", r)
	}
}

// A type error the pass cannot fix: one repair call, whose reply is the good file; accepted, Repaired true.
func TestSettleRepairsACompileErrorWithOneCall(t *testing.T) {
	good := goodGreetReply(t)
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     withTypeError(t, good),
		"testwrite.fn-greet.fix.txt": fencedFile(t, good),
	}))
	g.mustPlan(planner.Options{})
	if n := count(g.stagesCalled(), fixStage); n != 1 {
		t.Errorf("%s called %d times, want 1", fixStage, n)
	}
	if n := count(g.stagesCalled(), "testwrite:fn-greet"); n != 1 {
		t.Errorf("testwrite:fn-greet called %d times, want 1", n)
	}
	if fixes, repaired := greetRecord(t, g); !repaired || len(fixes) != 0 {
		t.Errorf("record fixes = %q, repaired = %v; want none, true", fixes, repaired)
	}
	if r := greetRefusals(g); len(r) != 0 {
		t.Errorf("refusals = %+v, want none", r)
	}
	if !hasWarning(g, "test_file_repaired: the test file of node fn-greet compiled after a repair call") {
		t.Error("no test_file_repaired warning")
	}
	if src := writtenGreetTest(t, g); strings.Contains(src, "got != 1") {
		t.Errorf("the unrepaired file was written:\n%s", src)
	}
}

// The repair reply still fails: one refusal, for the original errors, prefixed.
func TestSettleFailedRepairRefusesTheOriginalErrors(t *testing.T) {
	good := goodGreetReply(t)
	bad := withTypeError(t, good)
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     bad,
		"testwrite.fn-greet.fix.txt": fencedFile(t, bad),
		"testwrite.fn-greet.2.txt":   good,
	}))
	g.mustPlan(planner.Options{})
	refs := greetRefusals(g)
	if len(refs) != 1 {
		t.Fatalf("refusals = %+v, want exactly one", refs)
	}
	if !strings.HasPrefix(refs[0].Text, "after a repair call: test file does not compile") {
		t.Errorf("refusal = %q, want the prefixed compile refusal", refs[0].Text)
	}
	if want := fmt.Sprintf("line %d:", lineOf(t, bad, "got != 1")); !strings.Contains(refs[0].Text, want) {
		t.Errorf("refusal = %q, want %s", refs[0].Text, want)
	}
	if n := count(g.stagesCalled(), fixStage); n != 1 {
		t.Errorf("%s called %d times, want 1", fixStage, n)
	}
	if _, repaired := greetRecord(t, g); repaired {
		t.Error("the accepted .2 reply is recorded as repaired")
	}
}

// The repair reply has no Go file: the refusal is the original error and the event says unusable.
func TestSettleUnusableRepairReply(t *testing.T) {
	good := goodGreetReply(t)
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     withTypeError(t, good),
		"testwrite.fn-greet.fix.txt": "nope",
		"testwrite.fn-greet.2.txt":   good,
	}))
	g.mustPlan(planner.Options{})
	if !hasWarning(g, "test_file_repair_unusable: the repair reply for node fn-greet held no Go file") {
		t.Error("no test_file_repair_unusable warning")
	}
	refs := greetRefusals(g)
	if len(refs) != 1 {
		t.Fatalf("refusals = %+v, want exactly one", refs)
	}
	if strings.Contains(refs[0].Text, "after a repair call") || !strings.HasPrefix(refs[0].Text, "test file does not compile") {
		t.Errorf("refusal = %q, want the unprefixed compile refusal", refs[0].Text)
	}
}

// A syntax error gets the repair call too.
func TestSettleRepairsASyntaxError(t *testing.T) {
	good := goodGreetReply(t)
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     edited(t, good, `import "testing"`, "import \"testing\"\n\nvar = ="),
		"testwrite.fn-greet.fix.txt": fencedFile(t, good),
	}))
	g.mustPlan(planner.Options{})
	if n := count(g.stagesCalled(), fixStage); n != 1 {
		t.Errorf("%s called %d times, want 1", fixStage, n)
	}
	if n := count(g.stagesCalled(), "testwrite:fn-greet"); n != 1 {
		t.Errorf("testwrite:fn-greet called %d times, want 1", n)
	}
	if _, repaired := greetRecord(t, g); !repaired {
		t.Error("record is not repaired")
	}
	if r := greetRefusals(g); len(r) != 0 {
		t.Errorf("refusals = %+v, want none", r)
	}
}

// A repair reply in another package is unusable: the refusal is the original error.
func TestSettleRepairInAnotherPackageIsUnusable(t *testing.T) {
	good := goodGreetReply(t)
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     withTypeError(t, good),
		"testwrite.fn-greet.fix.txt": edited(t, fencedFile(t, good), "package greet\n", "package greet_test\n"),
		"testwrite.fn-greet.2.txt":   good,
	}))
	g.mustPlan(planner.Options{})
	if !hasWarning(g, "test_file_repair_unusable: the repair reply for node fn-greet held no Go file") {
		t.Error("no test_file_repair_unusable warning")
	}
	if refs := greetRefusals(g); len(refs) != 1 || strings.Contains(refs[0].Text, "after a repair call") {
		t.Errorf("refusals = %+v, want one unprefixed", refs)
	}
	if _, repaired := greetRecord(t, g); repaired {
		t.Error("the accepted .2 reply is recorded as repaired")
	}
}

// Refusal line numbers are the model's: an unfixed diagnostic below an inserted "_ = x" keeps its original line.
func TestSettleRefusalKeepsOriginalLineNumbers(t *testing.T) {
	good := goodGreetReply(t)
	bad := withTypeError(t, withUnused(t, good))
	g := newRig(t, approving(), variant(t, map[string]string{
		"testwrite.fn-greet.txt":     bad,
		"testwrite.fn-greet.fix.txt": "nope",
		"testwrite.fn-greet.2.txt":   good,
	}))
	g.mustPlan(planner.Options{})
	refs := greetRefusals(g)
	if len(refs) != 1 {
		t.Fatalf("refusals = %+v, want exactly one", refs)
	}
	m := lineOf(t, bad, "got != 1")
	if !strings.Contains(refs[0].Text, fmt.Sprintf("line %d:", m)) || strings.Contains(refs[0].Text, fmt.Sprintf("line %d:", m+1)) {
		t.Errorf("refusal = %q, want line %d", refs[0].Text, m)
	}
	if strings.Contains(refs[0].Text, "unused") {
		t.Errorf("refusal = %q names the fixed unused variable", refs[0].Text)
	}
	// The repair call is shown the fixed file, so its errors name that file's
	// lines: the inserted "_ = unused" moves the type error down by one.
	var prompts []string
	for _, req := range g.fake.Requests() {
		if planner.StageOf(req) == fixStage {
			prompts = append(prompts, req.Messages[len(req.Messages)-1].Content)
		}
	}
	if len(prompts) != 1 {
		t.Fatalf("%s requests = %d, want 1", fixStage, len(prompts))
	}
	errs := prompts[0][strings.LastIndex(prompts[0], "Errors:"):]
	if !strings.Contains(errs, fmt.Sprintf("line %d:", m+1)) || strings.Contains(errs, fmt.Sprintf("line %d:", m)) {
		t.Errorf("repair errors = %q, want line %d", errs, m+1)
	}
	if !strings.Contains(prompts[0], "_ = unused") {
		t.Error("the repair call was not shown the fixed file")
	}
}
