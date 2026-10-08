package planner

import (
	"reflect"
	"strings"
	"testing"
)

func covFixture() CoverageFile {
	return CoverageFile{Covered: []Covered{
		{Requirement: "F1", Nodes: []string{"fn-a", "greeting"}},
		{Requirement: "C1", Nodes: []string{"greeting"}},
		{Requirement: "A1", Nodes: []string{"fn-b"}},
	}}
}

func TestRequirementIDsAreTheFunctionsOwnElseItsComponents(t *testing.T) {
	cov := covFixture()
	if got := requirementIDsFor(cov, "fn-a", "greeting"); !reflect.DeepEqual(got, []string{"F1"}) {
		t.Errorf("fn-a = %v, want its own [F1]", got)
	}
	if got := requirementIDsFor(cov, "fn-c", "greeting"); !reflect.DeepEqual(got, []string{"C1", "F1"}) {
		t.Errorf("fn-c has none of its own, so it takes its component's: got %v", got)
	}
	if got := requirementIDsFor(cov, "fn-x", "other"); len(got) != 0 {
		t.Errorf("fn-x = %v, want none", got)
	}
	if got := allRequirementIDs(cov); !reflect.DeepEqual(got, []string{"A1", "C1", "F1"}) {
		t.Errorf("all = %v", got)
	}
}

func TestTestwriterViewCarriesNoBriefDerivedReasoning(t *testing.T) {
	const canary = "CANARY-view-9021"
	d := draftFixture(t)
	for _, k := range []string{"rationale", "alternatives", "refactor_notes", "assumptions", "open_questions", "decision_ids", "decisions", "construction", "security", "performance", "observability", "portability"} {
		d[k] = canary
	}
	d["profile_hooks"] = []any{"bench", "pprof"}
	v := testwriterView(d)
	if strings.Contains(mustJSON(v), canary) {
		t.Fatalf("the Test-writer view leaks node reasoning:\n%s", mustJSON(v))
	}
	if v["contract"] == nil || v["context"] == nil || v["id"] != "fn-register" {
		t.Errorf("view = %v", v)
	}
	if !reflect.DeepEqual(v["profile_hooks"], []string{"bench", "pprof"}) {
		t.Errorf("profile_hooks = %v", v["profile_hooks"])
	}
	d["profile_hooks"] = map[string]any{"not_applicable": "this function is too small to profile usefully"}
	if _, has := testwriterView(d)["profile_hooks"]; has {
		t.Error("a not-applicable profile_hooks must not appear")
	}
}

func testsReply(tests string, file string) string {
	return `{"tests":` + tests + `,"test_file":` + mustJSON(file) + `}`
}

const goodTestFile = `package httpapi

import "testing"

func TestRegister(t *testing.T) {}
`

func TestParseTestwriteRequiresPolarityAndCoverageOfEveryError(t *testing.T) {
	ct := draftFixture(t)["contract"].(map[string]any) // two errors
	good := `[
 {"name":"valid returns 201","given":"g","expect":"e","polarity":"success","covers":"happy"},
 {"name":"bad json","given":"g","expect":"e","polarity":"negative","covers":"error:1"},
 {"name":"email taken","given":"g","expect":"e","polarity":"negative","covers":"error:2"},
 {"name":"empty body","given":"g","expect":"e","polarity":"boundary","covers":"input:r"}]`
	tests, _, err := parseTestwrite(testsReply(good, goodTestFile), ct, "httpapi", "TestRegister", "example.com/acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tests[1]["polarity"] != "negative" || tests[1]["covers"] != "error:1" || tests[1]["level"] != "unit" || !strings.Contains(tests[1]["command"].(string), "TestRegister") {
		t.Errorf("tests[1] = %v", tests[1])
	}
	for name, c := range map[string]struct {
		mut  func(string) string
		word string
	}{
		"no happy test":         {func(s string) string { return strings.Replace(s, `"covers":"happy"`, `"covers":"input:r"`, 1) }, grpPolarity},
		"an error is uncovered": {func(s string) string { return strings.Replace(s, `"covers":"error:2"`, `"covers":"error:1"`, 1) }, grpErrTest},
		"bad polarity":          {func(s string) string { return strings.Replace(s, `"polarity":"success"`, `"polarity":"sunny"`, 1) }, grpEnum},
		"a negative test that is not negative": {func(s string) string {
			return strings.Replace(s, `"polarity":"negative","covers":"error:2"`, `"polarity":"success","covers":"error:2"`, 1)
		}, grpPolarity},
		"bad covers": {func(s string) string { return strings.Replace(s, `"covers":"input:r"`, `"covers":"everything"`, 1) }, grpEnum},
	} {
		_, _, err := parseTestwrite(testsReply(c.mut(good), goodTestFile), ct, "httpapi", "TestRegister", "example.com/acme", nil)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.HasPrefix(err.Error(), c.word+":") {
			t.Errorf("%s: err = %q, want the prefix %q", name, err, c.word+":")
		}
	}
}

func TestParseTestwriteRequiresABenchmarkWhenTheHookAsksForOne(t *testing.T) {
	ct := map[string]any{"file": "internal/httpapi/register.go", "errors": []any{}}
	good := `[{"name":"valid","given":"g","expect":"e","polarity":"success","covers":"happy"}]`
	if _, _, err := parseTestwrite(testsReply(good, goodTestFile), ct, "httpapi", "TestRegister", "example.com/acme", []string{"bench"}); err == nil || !strings.HasPrefix(err.Error(), grpHookBench+":") || !strings.Contains(err.Error(), "benchmark") {
		t.Fatalf("err = %v, want a hook_bench refusal about the missing benchmark", err)
	}
	withBench := goodTestFile + "\nfunc BenchmarkRegister(b *testing.B) {}\n"
	if _, _, err := parseTestwrite(testsReply(good, withBench), ct, "httpapi", "TestRegister", "example.com/acme", []string{"bench"}); err != nil {
		t.Fatalf("a file with the benchmark was refused: %v", err)
	}
	if !hasBenchmark(withBench, "TestRegister") || hasBenchmark(goodTestFile, "TestRegister") {
		t.Error("hasBenchmark")
	}
}

func TestLinkErrorTestsWritesTheTestNameIntoEachError(t *testing.T) {
	ct := draftFixture(t)["contract"].(map[string]any)
	tests := []map[string]any{
		{"name": "ok", "polarity": "success", "covers": "happy"},
		{"name": "bad json", "polarity": "negative", "covers": "error:1"},
		{"name": "email taken", "polarity": "negative", "covers": "error:2"},
	}
	if err := linkErrorTests(ct, tests); err != nil {
		t.Fatal(err)
	}
	errs := objects(ct["errors"])
	if errs[0]["test"] != "bad json" || errs[1]["test"] != "email taken" {
		t.Errorf("errors = %v", errs)
	}
	if err := linkErrorTests(ct, tests[:2]); err == nil {
		t.Error("an error with no covering test must be refused")
	}
}

func TestFinishGroupDefectsFindsAHandEditedTree(t *testing.T) {
	s := settledStore()
	root := map[string]any{"id": "run", "kind": "root"}
	fn := map[string]any{"id": "fn-a", "kind": "function", "decision_ids": []any{"q1"}}
	embedDecisions(s, root, []map[string]any{fn})
	if bad := finishGroupDefects(append([]map[string]any{root}, fn), s); len(bad) != 0 {
		t.Fatalf("a clean tree reported %v", bad)
	}
	fn["decisions"].([]any)[0].(map[string]any)["answer"] = "tampered"
	if bad := finishGroupDefects(append([]map[string]any{root}, fn), s); len(bad) != 1 || !strings.Contains(bad[0], grpDecisionsEmbed) {
		t.Fatalf("a hand edit was not found: %v", bad)
	}
}
