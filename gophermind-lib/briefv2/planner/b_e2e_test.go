package planner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
)

func rawNodes(t *testing.T, runDir string) map[string]map[string]any {
	t.Helper()
	loaded, err := tree.NewStore(runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for id, n := range loaded.Nodes {
		raw, err := n.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		out[id] = m
	}
	return out
}

func TestPlanBuildsATreeWhoseNodesCarryEveryGroupAndTheConversation(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	if out, err := g.plan(planner.Options{}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	nodes := rawNodes(t, g.runDir)
	functions := 0
	for id, n := range nodes {
		if n["kind"] != "function" {
			continue
		}
		functions++
		for _, k := range append(append([]string(nil), allGroups...), "requirement_ids", "decisions") {
			if _, ok := n[k]; !ok {
				t.Errorf("%s lacks %s", id, k)
			}
		}
		ct := n["contract"].(map[string]any)
		errs, _ := ct["errors"].([]any)
		tests := n["tests"].([]any)
		names := map[string]bool{}
		for _, x := range tests {
			tm := x.(map[string]any)
			names[tm["name"].(string)] = true
			if tm["polarity"] == nil || tm["covers"] == nil {
				t.Errorf("%s: a test has no polarity or covers", id)
			}
		}
		for i, e := range errs {
			if name, _ := e.(map[string]any)["test"].(string); !names[name] {
				t.Errorf("%s: error %d links to %q, which is not one of its tests", id, i+1, name)
			}
		}
		cited := map[string]bool{}
		for _, q := range n["decision_ids"].([]any) {
			cited[q.(string)] = true
		}
		recs := n["decisions"].([]any)
		if len(recs) != len(cited) {
			t.Errorf("%s: %d embedded decisions for %d cited", id, len(recs), len(cited))
		}
		for _, r := range recs {
			if !cited[r.(map[string]any)["id"].(string)] {
				t.Errorf("%s carries a decision it does not cite", id)
			}
		}
	}
	if functions == 0 {
		t.Fatal("no function nodes")
	}
	root := nodes[greeterID]
	rootDecisions := root["decisions"].([]any)
	if len(rootDecisions) != 1 || rootDecisions[0].(map[string]any)["id"] != "q1" || rootDecisions[0].(map[string]any)["answered_by"] != "human" {
		t.Errorf("the root does not carry the conversation: %v", rootDecisions)
	}
	if ids, _ := root["requirement_ids"].([]any); len(ids) == 0 {
		t.Error("the root names no requirement")
	}

	// The approval summary shows the groups, and the approval binds the understanding.
	if len(gate.plans) == 0 || !strings.Contains(gate.plans[0].Markdown, "## Node groups") {
		t.Error("the approval summary has no Node groups section")
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "approval.json"))
	if !strings.Contains(string(raw), `"understanding_hash": "`) {
		t.Errorf("approval.json = %s", raw)
	}

	// A node-scope call never carries the node's reasoning.
	for _, req := range g.fake.Requests() {
		if !strings.HasPrefix(planner.StageOf(req), "testwrite:") {
			continue
		}
		for _, m := range req.Messages {
			for _, forbidden := range []string{`"rationale"`, `"alternatives"`, `"refactor_notes"`, `"assumptions"`, `"open_questions"`,
				`"decision_ids"`, `"decisions"`, `"construction"`, `"security"`, `"performance"`, `"observability"`, `"portability"`} {
				if m.Role == provider.RoleUser && strings.Contains(m.Content, forbidden) {
					t.Errorf("a Test-writer prompt contains %s", forbidden)
				}
			}
		}
	}
}

type refusesQuestions struct {
	*scriptGate
	t *testing.T
}

func (g refusesQuestions) Ask(_ context.Context, qs []human.Question) ([]human.Answer, error) {
	g.t.Errorf("an unattended run asked %d question(s)", len(qs))
	return nil, errors.New("asked")
}

func TestUnattendedPlanNeverAsksAndRecordsEveryAssumption(t *testing.T) {
	gate := refusesQuestions{approving(), t}
	g := newRig(t, gate)
	assumeBrief(g)
	if out, err := g.plan(planner.Options{}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	root := rawNodes(t, g.runDir)[greeterID]
	rec := root["decisions"].([]any)[0].(map[string]any)
	if rec["answered_by"] != "unattended-default" || rec["answer"] != "Yes, trim it." {
		t.Errorf("root decision = %v", rec)
	}
	md, _ := os.ReadFile(filepath.Join(g.runDir, "UNDERSTANDING.md"))
	if !strings.Contains(string(md), "[assumed, nobody was asked]") {
		t.Errorf("UNDERSTANDING.md does not list the assumption:\n%s", md)
	}
}

func TestChangingAnAnswerRestartsFromContractAndNeedsANewConfirmationAndApproval(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	if _, err := g.plan(planner.Options{StopAfter: "approve"}); err != nil {
		t.Fatal(err)
	}
	rep, err := planner.ChangeAnswer(greeterID, "q1", "No, use it exactly as given.", time.Now())
	if err != nil || rep.Reset != "contract" {
		t.Fatalf("ChangeAnswer = %+v, %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "contracts.json")); err == nil {
		t.Fatal("contracts.json survived the reset")
	}
	if out, err := g.plan(planner.Options{RunID: greeterID}); err != nil || out != planner.Done {
		t.Fatalf("resume = %v, %v", out, err)
	}
	if len(gate.understandings) != 2 || gate.understandings[0].Hash == gate.understandings[1].Hash {
		t.Errorf("the changed answer must put a new understanding to the gate: %d", len(gate.understandings))
	}
	if len(gate.plans) != 2 {
		t.Errorf("the plan was put to the gate %d times, want 2", len(gate.plans))
	}
	rec := rawNodes(t, g.runDir)[greeterID]["decisions"].([]any)[0].(map[string]any)
	hist, _ := rec["history"].([]any)
	if rec["answer"] != "No, use it exactly as given." || len(hist) != 1 || hist[0].(map[string]any)["answer"] != "yes" {
		t.Errorf("the tree does not show the change: %v", rec)
	}
}

func TestPlanRefusesToApproveWhileAnOpenQuestionRemains(t *testing.T) {
	respond := func(stage string, call int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			out[i]["open_questions"] = []any{"Which store?"}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	g := newRig(t, approving())
	withEnrichment(g, respond)
	_, err := g.plan(planner.Options{})
	if err == nil || !strings.Contains(err.Error(), "open questions") {
		t.Fatalf("err = %v, want a refusal naming the open questions", err)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "approval.json")); err == nil {
		t.Fatal("a plan with open questions was approved")
	}
}

// Every settled question has a decision record that lists the nodes it shaped:
// the functions and components whose decision_ids name it, and the root.
func TestDecisionRecordsListTheNodesTheyShaped(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	md := string(g.read("decisions/q1.md"))
	if strings.Contains(md, "none recorded") {
		t.Fatalf("the record names no node:\n%s", md)
	}
	nodes := rawNodes(t, g.runDir)
	var cited []string
	for id, n := range nodes {
		for _, q := range n["decision_ids"].([]any) {
			if q == "q1" {
				cited = append(cited, id)
			}
		}
	}
	if len(cited) < 2 {
		t.Fatalf("only %v cite q1; the fixtures should make fn-greet, fn-farewell and the root cite it", cited)
	}
	sort.Strings(cited)
	want := "Shaped these nodes: "
	line := ""
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, want) {
			line = strings.TrimPrefix(l, want)
		}
	}
	if line == "" {
		t.Fatalf("the record has no 'Shaped these nodes' line:\n%s", md)
	}
	listed := strings.Split(line, ", ")
	sort.Strings(listed)
	// The root node is listed as "root"; the tree calls it by the run id.
	for i, id := range cited {
		if id == greeterID {
			cited[i] = "root"
		}
	}
	sort.Strings(cited)
	if strings.Join(listed, ",") != strings.Join(cited, ",") {
		t.Errorf("the record lists %v, the nodes citing q1 are %v", listed, cited)
	}
}

// A secret's value is in the vault and nowhere else. This covers the files the
// question model and the node groups add: the question store, the answers, the
// decision records, the understanding, enriched.json and every node.
func TestNoSecretValueReachesAnyFileTheNewStagesWrite(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, withSecret)
	store := &memSecrets{vals: map[string]string{vault.HarnessScope + "/GREETER_API_KEY": canary}}
	g.deps.OpenSecrets = func() (planner.Secrets, error) { return store, nil }
	// The model may name a declared secret in secret_use; it never sees a value.
	withEnrichment(g, func(_ string, _ int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			if n.ID == "fn-greet" {
				out[i]["security"] = map[string]any{"trust_boundary": "external_input", "untrusted_inputs": []any{"name"}, "authz": "none",
					"secret_use": []any{"GREETER_API_KEY"},
					"threats":    []any{map[string]any{"threat": "a very long name", "mitigation": "the function allocates one string and does no I/O"}}}
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	})
	g.mustPlan(planner.Options{})

	checked := map[string]bool{}
	_ = filepath.WalkDir(g.runDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(g.runDir, p)
		checked[rel] = true
		if strings.Contains(string(raw), canary) {
			t.Errorf("%s contains the secret value", rel)
		}
		return nil
	})
	for _, want := range []string{"_state/enriched.json", "_state/clarify/questions.json", "answers.json", "decisions/q1.md", "UNDERSTANDING.md",
		"_state/understanding.json", "approval.json", "root.json"} {
		if !checked[want] {
			t.Errorf("%s was not among the files checked", want)
		}
	}
	if !strings.Contains(string(g.read("_state/enriched.json")), "GREETER_API_KEY") && !strings.Contains(string(g.read("_state/decomposed.json")), "GREETER_API_KEY") {
		t.Error("the declared secret's name never reached the enriched nodes, so the check proves nothing")
	}
	for _, req := range g.fake.Requests() {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, canary) {
				t.Errorf("a %s prompt contains the secret value", planner.StageOf(req))
			}
		}
	}
}

// The groups make nodes several times larger. This records bytes per function
// node with and without them, so the cost is measured, and keeps a node under
// a size an implement call can still use.
func TestNodeSizeBeforeAndAfterTheGroups(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	widened := append(append([]string(nil), allGroups...), "requirement_ids", "decisions", "decision_ids")
	const maxNodeBytes = 24 * 1024
	for id, n := range rawNodes(t, g.runDir) {
		if n["kind"] != "function" {
			continue
		}
		after, _ := json.Marshal(n)
		old := map[string]any{}
		for k, v := range n {
			old[k] = v
		}
		for _, k := range widened {
			if k != "node_class" {
				delete(old, k)
			}
		}
		before, _ := json.Marshal(old)
		t.Logf("%s: %d bytes before the groups, %d after (%.1fx)", id, len(before), len(after), float64(len(after))/float64(len(before)))
		if len(after) <= len(before) {
			t.Errorf("%s did not grow", id)
		}
		if len(after) > maxNodeBytes {
			t.Errorf("%s is %d bytes, over the %d byte ceiling", id, len(after), maxNodeBytes)
		}
	}
}
