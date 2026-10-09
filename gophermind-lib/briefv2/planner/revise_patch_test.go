package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// plannedGreetNode plans the greeter offline and returns the fn-greet node, the
// contracts file bytes and the settled decision ids.
func plannedGreetNode(t *testing.T) (node map[string]any, contracts []byte, g *rig) {
	t.Helper()
	g = newRig(t, approving())
	g.mustPlan(planner.Options{})
	node = rawNodes(t, g.runDir)["fn-greet"]
	if node == nil {
		t.Fatal("the greeter plan has no fn-greet node")
	}
	var err error
	if contracts, err = os.ReadFile(filepath.Join(g.runDir, "contracts.json")); err != nil {
		t.Fatal(err)
	}
	return node, contracts, g
}

func decisionIDs(node map[string]any) []string {
	var ids []string
	for _, v := range node["decision_ids"].([]any) {
		ids = append(ids, v.(string))
	}
	return ids
}

func envFor(node map[string]any) planner.ReviseCheckEnv {
	return planner.ReviseCheckEnv{Class: node["node_class"].(string), SecretNames: []string{"GREETER_TOKEN"}, DecisionIDs: decisionIDs(node), N: 1}
}

func TestParseReviseReplyForms(t *testing.T) {
	cases := []struct {
		name, reply, kind string
		wantErr           bool
	}{
		{"patch", `{"description":"Trim tabs too.","assumptions":["the test used spaces only"]}`, "patch", false},
		{"fenced patch", "```json\n{\"description\":\"x\"}\n```", "patch", false},
		{"contract change", "CONTRACT_CHANGE_REQUIRED\nThe signature must return a second value.", "contract_change", false},
		{"split", "SPLIT_REQUIRED\nThe function does three things.", "split", false},
		{"contract problem line", "CONTRACT_PROBLEM: impossible", "", true},
		{"unknown key", `{"description":"x","test_file":"evil_test.go"}`, "", true},
		{"unknown group", `{"groups":{"depends_on":["x"]}}`, "", true},
		{"no change key", `{"assumptions":["why"]}`, "", true},
		{"empty description", `{"description":""}`, "", true},
		{"too many assumptions", `{"description":"x","assumptions":["1","2","3","4","5","6"]}`, "", true},
		{"long paragraph", "SPLIT_REQUIRED\n" + strings.Repeat("a", 601), "", true},
		{"empty paragraph", "CONTRACT_CHANGE_REQUIRED\n", "", true},
		{"two objects", `{"description":"x"}{"description":"y"}`, "", true},
	}
	for _, c := range cases {
		kind, _, err := planner.ParseReviseReply(c.reply)
		if (err != nil) != c.wantErr || kind != c.kind {
			t.Errorf("%s: kind %q err %v", c.name, kind, err)
		}
	}
	_, p, _ := planner.ParseReviseReply("SPLIT_REQUIRED\nline one\x1b[31m red\x07\nline two")
	if strings.ContainsAny(p, "\x1b\x07") {
		t.Errorf("control characters survive in the paragraph: %q", p)
	}
}

func TestCheckRevisePatchAcceptsADescriptionChange(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	merged, changed, err := planner.CheckRevisePatch(node, `{"description":"Greet a name. Leading and trailing tabs and newlines are trimmed as well as spaces.","assumptions":["the test only used spaces"]}`, contracts, envFor(node))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "description" {
		t.Fatalf("changed = %v", changed)
	}
	if merged["revision"].(int) != 1 {
		t.Fatalf("revision = %v", merged["revision"])
	}
	as := merged["assumptions"].([]any)
	if last := as[len(as)-1].(string); last != "revision 1: the test only used spaces" {
		t.Fatalf("last assumption = %q", last)
	}
	if node["description"] == merged["description"] {
		t.Fatal("the original document was changed in place or the merge did nothing")
	}
}

func TestCheckRevisePatchFrozenFieldsCannotMove(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	// The grammar cannot express most frozen fields; the merge guard is exercised
	// through a hand-built patch that smuggles one in as a group item or an item key.
	for name, reply := range map[string]string{
		"input item key go_type":      `{"contract":{"inputs":[{"name":"name","type":"string","go_type":"int"}]}}`,
		"error item key test":         `{"contract":{"errors":[{"when":"w","returns":"r","kind":"sentinel","test":"TestX"}]}}`,
		"input item unknown key":      `{"contract":{"inputs":[{"name":"name","type":"string","path":"/etc"}]}}`,
		"error item kind not an enum": `{"contract":{"errors":[{"when":"w","returns":"r","kind":"bogus"}]},"rewrite_tests":true}`,
	} {
		_, _, err := planner.CheckRevisePatch(node, reply, contracts, envFor(node))
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v, want class %s", name, err, planner.ClassReviseInvalid)
		}
	}
}

func TestCheckRevisePatchInputNamesMustMatchTheSignature(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	_, _, err := planner.CheckRevisePatch(node, `{"contract":{"inputs":[{"name":"who","type":"string"}]}}`, contracts, envFor(node))
	if planner.ReviseErrClass(err) != planner.ClassContractFlagged {
		t.Fatalf("err = %v, want class %s", err, planner.ClassContractFlagged)
	}
}

func TestCheckRevisePatchRunsTheGroupChecks(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	for name, reply := range map[string]string{
		"construction with one step": `{"groups":{"construction":{"approach_chosen":"trim","steps":["only one"]}}}`,
		"na reason too short":        `{"groups":{"profile_hooks":{"not_applicable":"no"}}}`,
		"log message with a verb":    `{"groups":{"observability":{"log_events":[{"level":"info","msg":"hello %s","fields":["name"]}],"metrics":[]}}}`,
	} {
		_, _, err := planner.CheckRevisePatch(node, reply, contracts, envFor(node))
		c := planner.ReviseErrClass(err)
		if c != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v (class %q), want %s", name, err, c, planner.ClassReviseInvalid)
			continue
		}
		if strings.Contains(err.Error(), "hello") || strings.Contains(err.Error(), "only one") {
			t.Errorf("%s: the message quotes the reply: %v", name, err)
		}
	}
}

func TestCheckRevisePatchNoopAndSecret(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	same, _ := json.Marshal(map[string]any{"description": node["description"]})
	if _, _, err := planner.CheckRevisePatch(node, string(same), contracts, envFor(node)); planner.ReviseErrClass(err) != planner.ClassReviseNoop {
		t.Fatalf("a patch equal to the node: err = %v", err)
	}
	env := envFor(node)
	env.Secrets = []string{"CANARY-SECRET-VALUE"}
	_, _, err := planner.CheckRevisePatch(node, `{"description":"Use CANARY-SECRET-VALUE as the greeting."}`, contracts, env)
	if planner.ReviseErrClass(err) != planner.ClassSecretInReply {
		t.Fatalf("err = %v, want secret_in_reply", err)
	}
	if strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("the message quotes the secret: %v", err)
	}
	// With tests to be rewritten a no-op body is not decided here.
	env = envFor(node)
	env.TestsRewrite = true
	if _, _, err := planner.CheckRevisePatch(node, string(same), contracts, env); err != nil {
		t.Fatalf("a body-equal patch with rewrite_tests: %v", err)
	}
}

// cloneDoc deep-copies a node document.
func cloneDoc(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCheckRevisePatchInputCheckRunsOnlyWhenInputsAreSet(t *testing.T) {
	greet, contracts, _ := plannedGreetNode(t)
	// A node whose inputs omit the signature's parameter: planning only
	// requires an input per named parameter, so this is a valid node.
	node := cloneDoc(t, greet)
	node["contract"].(map[string]any)["inputs"] = []any{}
	env := envFor(node)
	if _, _, err := planner.CheckRevisePatch(node, `{"description":"Greet a name; surrounding white space of any kind is trimmed."}`, contracts, env); err != nil {
		t.Fatalf("a description-only patch on a node with fewer inputs: %v", err)
	}
	_, _, err := planner.CheckRevisePatch(node, `{"contract":{"inputs":[{"name":"who","type":"string"}]}}`, contracts, env)
	if planner.ReviseErrClass(err) != planner.ClassContractFlagged {
		t.Fatalf("mismatching names: err = %v", err)
	}
	if _, _, err := planner.CheckRevisePatch(node, `{"contract":{"inputs":[{"name":"name","type":"string"}]}}`, contracts, env); err != nil {
		t.Fatalf("matching names: %v", err)
	}
	_, _, err = planner.CheckRevisePatch(node, `{"contract":{"inputs":[{"name":"name","type":"string"},{"name":"name","type":"string"}]}}`, contracts, env)
	if planner.ReviseErrClass(err) != planner.ClassContractFlagged {
		t.Fatalf("duplicate names: err = %v", err)
	}
}

func TestCheckMergedFrozenFields(t *testing.T) {
	node, _, _ := plannedGreetNode(t)
	env := envFor(node)
	mut := map[string]func(m map[string]any){
		"depends_on": func(m map[string]any) { m["depends_on"] = []any{"fn-other"} },
		"signature": func(m map[string]any) {
			m["contract"].(map[string]any)["signature"] = "func Greet(who string) (string, error)"
		},
		"package":      func(m map[string]any) { m["contract"].(map[string]any)["package"] = "other" },
		"file":         func(m map[string]any) { m["contract"].(map[string]any)["file"] = "other.go" },
		"id":           func(m map[string]any) { m["id"] = "fn-evil" },
		"wave":         func(m map[string]any) { m["wave"] = 99 },
		"model_tier":   func(m map[string]any) { m["model_tier"] = "frontier" },
		"rationale":    func(m map[string]any) { m["rationale"] = "changed" },
		"node_class":   func(m map[string]any) { m["node_class"] = "changed" },
		"secrets":      func(m map[string]any) { m["secrets"] = []any{"X"} },
		"decision_ids": func(m map[string]any) { m["decision_ids"] = []any{"changed"} },
	}
	for name, f := range mut {
		merged := cloneDoc(t, node)
		f(merged)
		_, err := planner.CheckMergedDoc(node, merged, env)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v, want %s", name, err, planner.ClassReviseInvalid)
		}
	}
	// An allowed change passes the same entry point.
	merged := cloneDoc(t, node)
	merged["description"] = "A new description."
	if _, err := planner.CheckMergedDoc(node, merged, env); err != nil {
		t.Fatalf("a description change: %v", err)
	}
}

func TestCheckMergedAssumptionsAreAPrefix(t *testing.T) {
	greet, _, _ := plannedGreetNode(t)
	node := cloneDoc(t, greet)
	node["assumptions"] = []any{"first", "second", "third"}
	env := envFor(node)
	old := node["assumptions"].([]any)
	cases := map[string][]any{
		"rewritten": append([]any{"rewritten"}, old[1:]...),
		"shortened": old[:len(old)-1],
		"reordered": append([]any{old[1], old[0]}, old[2:]...),
		"emptied":   {},
	}
	for name, as := range cases {
		merged := cloneDoc(t, node)
		merged["description"] = "changed so the no-op check is not the reason"
		merged["assumptions"] = as
		_, err := planner.CheckMergedDoc(node, merged, env)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v, want %s", name, err, planner.ClassReviseInvalid)
		}
	}
	merged := cloneDoc(t, node)
	merged["description"] = "changed"
	merged["assumptions"] = append(append([]any{}, old...), "appended by the model")
	if _, err := planner.CheckMergedDoc(node, merged, env); err != nil {
		t.Fatalf("a legit append: %v", err)
	}
}

func TestParseReviseReplyBounds(t *testing.T) {
	huge := strings.Repeat("a", 5000000)
	bad := map[string]string{
		"5 MB notes entry":  `{"context":{"notes":["` + huge + `"]}}`,
		"5 MB description":  `{"description":"` + huge + `"}`,
		"5 MB paragraph":    "SPLIT_REQUIRED\n" + huge,
		"long note":         `{"context":{"notes":["` + strings.Repeat("a", 501) + `"]}}`,
		"long constraint":   `{"context":{"constraints":["` + strings.Repeat("a", 501) + `"]}}`,
		"long side effect":  `{"contract":{"side_effects":["` + strings.Repeat("a", 501) + `"]}}`,
		"long item field":   `{"contract":{"inputs":[{"name":"` + strings.Repeat("a", 501) + `","type":"string"}]}}`,
		"long error field":  `{"contract":{"errors":[{"when":"` + strings.Repeat("a", 501) + `","returns":"r","kind":"sentinel"}]}}`,
		"many notes":        `{"context":{"notes":[` + strings.Repeat(`"n",`, 20) + `"n"]}}`,
		"many constraints":  `{"context":{"constraints":[` + strings.Repeat(`"n",`, 20) + `"n"]}}`,
		"many side effects": `{"contract":{"side_effects":[` + strings.Repeat(`"n",`, 20) + `"n"]}}`,
		"many inputs":       `{"contract":{"inputs":[` + strings.Repeat(`{"name":"a","type":"t"},`, 20) + `{"name":"a","type":"t"}]}}`,
		"many errors":       `{"contract":{"errors":[` + strings.Repeat(`{"when":"w","returns":"r","kind":"sentinel"},`, 20) + `{"when":"w","returns":"r","kind":"sentinel"}]}}`,
		"too deep":          `{"groups":{"security":` + strings.Repeat(`[`, 100) + strings.Repeat(`]`, 100) + `}}`,
	}
	for name, reply := range bad {
		_, _, err := planner.ParseReviseReply(reply)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v, want %s", name, err, planner.ClassReviseInvalid)
			continue
		}
		if len(err.Error()) > 200 || strings.Contains(err.Error(), "aaaa") {
			t.Errorf("%s: the message quotes or is long: %.100q", name, err.Error())
		}
	}
	if _, _, err := planner.ParseReviseReply(`{"context":{"notes":["` + strings.Repeat("a", 500) + `"]}}`); err != nil {
		t.Errorf("a note at the cap: %v", err)
	}
}

func TestParseReviseReplyDuplicateKeys(t *testing.T) {
	for name, reply := range map[string]string{
		"top level":      `{"description":"a","description":"b"}`,
		"nested item":    `{"contract":{"inputs":[{"name":"a","name":"b","type":"t"}]}}`,
		"contract":       `{"contract":{"side_effects":[],"side_effects":["x"]}}`,
		"inside a group": `{"groups":{"security":{"x":1,"x":2}}}`,
	} {
		_, _, err := planner.ParseReviseReply(reply)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A duplicate key that is not in the allow-list is never quoted.
	_, _, err := planner.ParseReviseReply(`{"groups":{"security":{"SECRETWORD":1,"SECRETWORD":2}}}`)
	if err == nil || strings.Contains(err.Error(), "SECRETWORD") {
		t.Errorf("a reply key is quoted: %v", err)
	}
}

func TestParseReviseReplyNullIsInvalid(t *testing.T) {
	for name, reply := range map[string]string{
		"description": `{"description":null}`,
		"contract":    `{"contract":null,"description":"x"}`,
		"inputs":      `{"contract":{"inputs":null}}`,
		"context":     `{"context":null,"description":"x"}`,
		"notes":       `{"context":{"notes":null}}`,
		"groups":      `{"groups":null,"description":"x"}`,
		"assumptions": `{"description":"x","assumptions":null}`,
		"rewrite":     `{"description":"x","rewrite_tests":null}`,
		"a group":     `{"groups":{"security":null}}`,
	} {
		_, _, err := planner.ParseReviseReply(reply)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseReviseReplyDuplicateAssumptions(t *testing.T) {
	_, _, err := planner.ParseReviseReply(`{"description":"x","assumptions":["same"," same "]}`)
	if planner.ReviseErrClass(err) != planner.ClassReviseInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestReviseReplySecretScanCoversParagraphs(t *testing.T) {
	secrets := []string{"CANARY-SECRET-VALUE"}
	for name, reply := range map[string]string{
		"split":           "SPLIT_REQUIRED\nIt must split because CANARY-SECRET-VALUE is used.",
		"contract change": "CONTRACT_CHANGE_REQUIRED\nThe signature leaks CANARY-SECRET-VALUE.",
		"patch note":      `{"context":{"notes":["CANARY-SECRET-VALUE"]}}`,
	} {
		_, _, err := planner.ParseReviseReplyScanned(reply, secrets)
		if planner.ReviseErrClass(err) != planner.ClassSecretInReply {
			t.Errorf("%s: err = %v, want %s", name, err, planner.ClassSecretInReply)
			continue
		}
		if strings.Contains(err.Error(), "CANARY") {
			t.Errorf("%s: the message quotes the secret", name)
		}
	}
	if _, _, err := planner.ParseReviseReplyScanned("SPLIT_REQUIRED\nnothing secret", secrets); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRevisePatchAssumptionsAreAppendOnly(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	before := len(node["assumptions"].([]any))
	merged, _, err := planner.CheckRevisePatch(node, `{"description":"Greet a name, trimming all white space.","assumptions":["a","b"]}`, contracts, envFor(node))
	if err != nil {
		t.Fatal(err)
	}
	as := merged["assumptions"].([]any)
	if len(as) != before+2 {
		t.Fatalf("assumptions grew by %d, want 2", len(as)-before)
	}
	for i, a := range node["assumptions"].([]any) {
		if as[i] != a {
			t.Fatalf("assumption %d was changed", i)
		}
	}
}

func TestCheckMergedGoTypesAreDerivedFromTheSignature(t *testing.T) {
	node, _, _ := plannedGreetNode(t)
	env := envFor(node)
	first := func(m map[string]any, field string) map[string]any {
		items, _ := m["contract"].(map[string]any)[field].([]any)
		if len(items) == 0 {
			t.Fatalf("the greeter node has no %s", field)
		}
		return items[0].(map[string]any)
	}
	mut := map[string]func(m map[string]any){
		"input edited":   func(m map[string]any) { first(m, "inputs")["go_type"] = "int" },
		"input missing":  func(m map[string]any) { delete(first(m, "inputs"), "go_type") },
		"output edited":  func(m map[string]any) { first(m, "outputs")["go_type"] = "int" },
		"output missing": func(m map[string]any) { delete(first(m, "outputs"), "go_type") },
	}
	for name, f := range mut {
		merged := cloneDoc(t, node)
		merged["description"] = "changed so the no-op check is not the reason"
		f(merged)
		_, err := planner.CheckMergedDoc(node, merged, env)
		if planner.ReviseErrClass(err) != planner.ClassReviseInvalid || !strings.Contains(err.Error(), "go_type") {
			t.Errorf("%s: err = %v, want %s naming go_type", name, err, planner.ClassReviseInvalid)
		}
	}
	merged := cloneDoc(t, node)
	merged["description"] = "changed"
	if _, err := planner.CheckMergedDoc(node, merged, env); err != nil {
		t.Fatalf("a legit merged document: %v", err)
	}
}
