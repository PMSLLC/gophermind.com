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

func TestCheckRevisePatchInputCheckRunsOnlyWhenInputsAreSet(t *testing.T) {
	node, contracts, _ := plannedGreetNode(t)
	// A description-only patch is fine even if the inputs do not list every
	// parameter (planning only requires an input per named parameter).
	if _, _, err := planner.CheckRevisePatch(node, `{"description":"Greet a name; surrounding white space of any kind is trimmed."}`, contracts, envFor(node)); err != nil {
		t.Fatalf("a description-only patch: %v", err)
	}
	ct := node["contract"].(map[string]any)
	var ins []any
	for _, in := range ct["inputs"].([]any) {
		m := in.(map[string]any)
		ins = append(ins, map[string]any{"name": m["name"], "type": m["type"], "description": "the name to greet, revised"})
	}
	b, _ := json.Marshal(map[string]any{"contract": map[string]any{"inputs": ins}})
	if _, _, err := planner.CheckRevisePatch(node, string(b), contracts, envFor(node)); err != nil {
		t.Fatalf("inputs with the signature's names: %v", err)
	}
	dup := append(append([]any{}, ins...), ins...)
	b, _ = json.Marshal(map[string]any{"contract": map[string]any{"inputs": dup}})
	if _, _, err := planner.CheckRevisePatch(node, string(b), contracts, envFor(node)); planner.ReviseErrClass(err) != planner.ClassContractFlagged {
		t.Fatalf("duplicate inputs: err = %v", err)
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
