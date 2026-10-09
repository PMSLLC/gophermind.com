package planner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
)

const runSecret = "CANARY-SECRET-VALUE"

const revisedGreetTest = `package greet

import "testing"

func TestGreet(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"greets a name", "Ada", "Hello, Ada!", false},
		{"trims the name", "  Ada  ", "Hello, Ada!", false},
		{"trims tabs and newlines", "\tAda\n", "Hello, Ada!", false},
		{"refuses an empty name", "   ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Greet(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("Greet(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("Greet(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
`

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// testwriteReply is a valid Test-writer reply for fn-greet carrying src.
func testwriteReply(t *testing.T, src string) string {
	return marshal(t, map[string]any{"test_file": src, "tests": []map[string]string{
		{"name": "greets a name", "polarity": "success", "covers": "happy", "given": `"Ada"`, "expect": "Hello, Ada! and a nil error"},
		{"name": "trims the name", "polarity": "boundary", "covers": "input:name", "given": `"  Ada  "`, "expect": "Hello, Ada! and a nil error"},
		{"name": "trims tabs and newlines", "polarity": "boundary", "covers": "input:name", "given": `"\tAda\n"`, "expect": "Hello, Ada! and a nil error"},
		{"name": "refuses an empty name", "polarity": "negative", "covers": "error:1", "given": `"   "`, "expect": "an empty string and a *NameError"},
	}})
}

const descriptionPatch = `{"description":"Greet a name. Leading and trailing spaces, tabs and newlines are trimmed first; an empty result is a *NameError.","rewrite_tests":true,"assumptions":["the planned test only used spaces"]}`

// planned plans the greeter with the given extra fixtures and returns the rig.
func planned(t *testing.T, files map[string]string) *rig {
	t.Helper()
	var dirs []string
	if files != nil {
		dirs = append(dirs, variant(t, files))
	}
	g := newRig(t, approving(), dirs...)
	g.mustPlan(planner.Options{})
	return g
}

func reviseReq(t *testing.T, g *rig) planner.ReviseRequest {
	t.Helper()
	lt, err := planner.ReadLeafTests(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(g.repo, filepath.FromSlash(lt["fn-greet"].TestFile)))
	if err != nil {
		t.Fatal(err)
	}
	return planner.ReviseRequest{
		RunID: greeterID, RunDir: g.runDir, Repo: g.repo, NodeID: "fn-greet", Trigger: planner.TriggerLadderExhausted,
		Revision: 0, Remaining: 2, History: []string{"attempt 1 a/m1 fail test_fail: TestGreet"},
		LastFailure: "Failing tests: TestGreet\n--- FAIL: TestGreet", TestSource: string(src), TestSHA256: lt["fn-greet"].SHA256,
		Secrets: []string{runSecret},
	}
}

func fileBytes(t *testing.T, g *rig, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(g.runDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReviseRewritesDescriptionAndTestsAndTouchesNothing(t *testing.T) {
	g := planned(t, map[string]string{"revise.fn-greet.txt": descriptionPatch, "testwrite.fn-greet-r1.txt": testwriteReply(t, revisedGreetTest)})
	nodeFile, testsFile, answers := fileBytes(t, g, "greeting/fn-greet.json"), fileBytes(t, g, "_state/leaf_tests.json"), fileBytes(t, g, "answers.json")
	before := len(g.stagesCalled())

	prop, err := planner.NewReviser(g.deps).Revise(context.Background(), reviseReq(t, g))
	if err != nil {
		t.Fatal(err)
	}
	if prop.Outcome != planner.OutcomeRewritten || prop.Class != "" {
		t.Fatalf("proposal = %+v", prop)
	}
	if got := g.stagesCalled()[before:]; len(got) != 2 || got[0] != "revise:fn-greet" || got[1] != "testwrite:fn-greet-r1" {
		t.Fatalf("stages = %v, want the revise call then the Test-writer call", got)
	}
	n, err := tree.ParseNode(prop.Body)
	if err != nil {
		t.Fatalf("the proposed body is not a valid node: %v", err)
	}
	raw, _ := n.Marshal()
	if string(append(raw, '\n')) != string(prop.Body) {
		t.Fatal("the body is not the bytes tree.Store would write")
	}
	var doc map[string]any
	_ = json.Unmarshal(prop.Body, &doc)
	if doc["revision"].(float64) != 1 || !strings.Contains(doc["description"].(string), "trimmed first") {
		t.Fatalf("revision/description = %v / %v", doc["revision"], doc["description"])
	}
	as := doc["assumptions"].([]any)
	if as[len(as)-1] != "revision 1: the planned test only used spaces" {
		t.Fatalf("assumptions = %v", as)
	}
	if !prop.TestChanged || prop.TestSource != revisedGreetTest || prop.TestSHA256 == reviseReq(t, g).TestSHA256 {
		t.Fatalf("test fields = %v %d bytes %s", prop.TestChanged, len(prop.TestSource), prop.TestSHA256)
	}
	if want := []string{"description", "tests"}; strings.Join(prop.Changed, ",") != strings.Join(want, ",") {
		t.Fatalf("changed = %v, want %v", prop.Changed, want)
	}
	if prop.Model == "" || len(prop.Assumptions) != 1 {
		t.Fatalf("model %q assumptions %v", prop.Model, prop.Assumptions)
	}
	// Nothing in the plan moved, and the approval still holds.
	for rel, was := range map[string][]byte{"greeting/fn-greet.json": nodeFile, "_state/leaf_tests.json": testsFile, "answers.json": answers} {
		if string(fileBytes(t, g, rel)) != string(was) {
			t.Errorf("%s changed", rel)
		}
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("approval no longer holds: %v", err)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "_state", "replies", "revise_fn-greet-1.txt")); err != nil {
		t.Errorf("the revise reply was not saved: %v", err)
	}
}

func TestReviseFlagsContractChangeSplitInputsAndLockedTests(t *testing.T) {
	cases := []struct {
		name, reply, class string
		settings           func(*settings.Config)
	}{
		{"contract change", "CONTRACT_CHANGE_REQUIRED\nThe function must also return the number of runes.", planner.ClassContractFlagged, nil},
		{"split", "SPLIT_REQUIRED\nTrimming, validating and formatting are three jobs.", planner.ClassSplitRequired, nil},
		{"inputs no longer match", `{"contract":{"inputs":[{"name":"who","type":"string"}]}}`, planner.ClassContractFlagged, nil},
		{"tests locked", `{"description":"changed","rewrite_tests":true}`, planner.ClassTestsLocked, func(c *settings.Config) { c.Revise.RewriteTests = "off" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := planned(t, map[string]string{"revise.fn-greet.txt": c.reply})
			if c.settings != nil {
				c.settings(g.cfg)
			}
			before := len(g.stagesCalled())
			prop, err := planner.NewReviser(g.deps).Revise(context.Background(), reviseReq(t, g))
			if err != nil || prop.Outcome != planner.OutcomeFlagged || prop.Class != c.class {
				t.Fatalf("proposal = %+v, %v; want flagged %s", prop, err, c.class)
			}
			if prop.Paragraph == "" || len(prop.Paragraph) > 600 || len(prop.Body) != 0 {
				t.Fatalf("paragraph %q body %d bytes", prop.Paragraph, len(prop.Body))
			}
			if got := g.stagesCalled()[before:]; len(got) != 1 {
				t.Fatalf("stages = %v, want the revise call only", got)
			}
		})
	}
}

func TestReviseRefusalsAreUnusableNotErrors(t *testing.T) {
	// files makes the fixture replies once the greeter is planned, so a case can
	// build its reply from the planned node.
	noop := func(t *testing.T, g *rig) map[string]string {
		var node map[string]any
		if err := json.Unmarshal(fileBytes(t, g, "greeting/fn-greet.json"), &node); err != nil {
			t.Fatal(err)
		}
		return map[string]string{"revise.fn-greet.txt": marshal(t, map[string]any{"description": node["description"]})}
	}
	fixed := func(files map[string]string) func(*testing.T, *rig) map[string]string {
		return func(*testing.T, *rig) map[string]string { return files }
	}
	cases := map[string]struct {
		files func(*testing.T, *rig) map[string]string
		class string
		calls int
	}{
		"not json twice": {fixed(map[string]string{"revise.fn-greet.txt": "I would rather not."}), planner.ClassReviseInvalid, 2},
		"unknown key":    {fixed(map[string]string{"revise.fn-greet.txt": `{"description":"x","test_file":"../evil_test.go"}`}), planner.ClassReviseInvalid, 2},
		"no op":          {noop, planner.ClassReviseNoop, 2},
		"secret":         {fixed(map[string]string{"revise.fn-greet.txt": `{"description":"use ` + runSecret + ` as the name"}`}), planner.ClassSecretInReply, 1},
		"secret in the test writer reply": {fixed(map[string]string{
			"revise.fn-greet.txt":       descriptionPatch,
			"testwrite.fn-greet-r1.txt": testwriteReply(t, strings.Replace(revisedGreetTest, `"Hello, Ada!", false},`, `"Hello, `+runSecret+`!", false},`, 1)),
		}), planner.ClassSecretInReply, 2},
		"test writer wrong function": {fixed(map[string]string{
			"revise.fn-greet.txt":       descriptionPatch,
			"testwrite.fn-greet-r1.txt": `{"test_file":"package greet\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n","tests":[]}`,
		}), planner.ClassReviseInvalid, 3},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			g := planned(t, nil)
			g.wire(variant(t, c.files(t, g)))
			before := len(g.stagesCalled())
			prop, err := planner.NewReviser(g.deps).Revise(context.Background(), reviseReq(t, g))
			if err != nil || prop.Outcome != planner.OutcomeUnusable || prop.Class != c.class {
				t.Fatalf("proposal = %+v, %v; want unusable %s", prop, err, c.class)
			}
			if got := g.stagesCalled()[before:]; len(got) != c.calls {
				t.Fatalf("stages = %v, want %d calls (the reply and the one repair call of the router)", got, c.calls)
			}
			// A reply that held a secret is never saved.
			entries, _ := os.ReadDir(filepath.Join(g.runDir, "_state", "replies"))
			for _, e := range entries {
				body, _ := os.ReadFile(filepath.Join(g.runDir, "_state", "replies", e.Name()))
				if strings.Contains(string(body), runSecret) {
					t.Errorf("%s holds the secret", e.Name())
				}
			}
		})
	}
}

func TestReviseRefusesUnsafeNodeIDs(t *testing.T) {
	g := planned(t, map[string]string{"revise.fn-greet.txt": descriptionPatch})
	for _, id := range []string{"../fn-greet", "fn-greet/../../x", "", "FN"} {
		req := reviseReq(t, g)
		req.NodeID = id
		if _, err := planner.NewReviser(g.deps).Revise(context.Background(), req); err == nil {
			t.Errorf("Revise accepted node id %q", id)
		}
	}
	req := reviseReq(t, g)
	req.Revision = 3 // the node is at revision 0
	if _, err := planner.NewReviser(g.deps).Revise(context.Background(), req); err == nil {
		t.Error("Revise accepted a revision the node is not at")
	}
}

// recCaller records every request's prompt and scope.
type recCaller struct {
	inner planner.Caller
	mu    sync.Mutex
	infos []router.CallInfo
	texts []string
}

func (c *recCaller) CallParsed(ctx context.Context, info router.CallInfo, req provider.Request, parse func(string) error) (router.Result, error) {
	c.mu.Lock()
	c.infos = append(c.infos, info)
	c.texts = append(c.texts, req.Messages[len(req.Messages)-1].Content)
	c.mu.Unlock()
	return c.inner.CallParsed(ctx, info, req, parse)
}

func TestRevisePrivacyNodeScopeNeverCarriesReasoning(t *testing.T) {
	g := planned(t, map[string]string{"revise.fn-greet.txt": descriptionPatch, "testwrite.fn-greet-r1.txt": testwriteReply(t, revisedGreetTest)})
	// Put a canary into every group node scope must not see.
	store := tree.NewStore(g.runDir)
	nodes, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := nodes.Nodes["fn-greet"].Marshal()
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["rationale"] = "RATIONALE-CANARY the reason this function exists in the brief"
	doc["alternatives"] = []any{map[string]any{"approach": "ALT-CANARY", "rejected_because": "ALT-CANARY-WHY"}}
	doc["refactor_notes"] = []any{map[string]any{"what": "REFACTOR-CANARY", "why": "later", "when": "later"}}
	doc["assumptions"] = []any{"ASSUME-CANARY"}
	doc["decisions"] = []any{map[string]any{"id": "q1", "question": "DECISION-CANARY?", "kind": "decision", "answer": "yes", "answered_by": "human", "raised_by": "clarify"}}
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	n, err := tree.ParseNode(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(n); err != nil {
		t.Fatal(err)
	}

	rec := &recCaller{inner: g.router}
	deps := g.deps
	deps.Caller = rec
	if _, err := planner.NewReviser(deps).Revise(context.Background(), reviseReq(t, g)); err != nil {
		t.Fatal(err)
	}
	for _, info := range rec.infos {
		if info.Scope != router.ScopeNode {
			t.Errorf("stage %s ran at scope %s, want node", info.Stage, info.Scope)
		}
	}
	for _, text := range rec.texts {
		for _, banned := range []string{"RATIONALE-CANARY", "ALT-CANARY", "REFACTOR-CANARY", "ASSUME-CANARY", "DECISION-CANARY", "brief_ref", "decision_ids", "open_questions"} {
			if strings.Contains(text, banned) {
				t.Errorf("a node-scope prompt carries %q", banned)
			}
		}
		if strings.Contains(text, runSecret) {
			t.Error("a prompt carries the secret")
		}
	}
	for _, rq := range g.fake.Requests() {
		for _, m := range rq.Messages {
			if strings.Contains(m.Content, runSecret) {
				t.Errorf("a %s message sent to the provider carries the secret", m.Role)
			}
		}
	}
	if !strings.Contains(rec.texts[0], "attempt 1 a/m1 fail test_fail") || !strings.Contains(rec.texts[0], "Failing tests: TestGreet") {
		t.Error("the revise prompt lacks the attempt history or the last failure")
	}

	// The component variant adds the brief section, rationale and assumptions, nothing else.
	g.cfg.Revise.Context = "component"
	rec = &recCaller{inner: g.router}
	deps.Caller = rec
	if _, err := planner.NewReviser(deps).Revise(context.Background(), reviseReq(t, g)); err != nil {
		t.Fatal(err)
	}
	for _, info := range rec.infos {
		if info.Stage == "revise:fn-greet" && info.Scope != router.ScopeComponent {
			t.Errorf("the component variant ran at scope %s", info.Scope)
		}
	}
	text := rec.texts[0]
	for _, want := range []string{"RATIONALE-CANARY", "ASSUME-CANARY"} {
		if !strings.Contains(text, want) {
			t.Errorf("the component prompt lacks %s", want)
		}
	}
	for _, banned := range []string{"ALT-CANARY", "REFACTOR-CANARY"} {
		if strings.Contains(text, banned) {
			t.Errorf("the component prompt carries %s", banned)
		}
	}
	if !regexp.MustCompile(`<brief_section>\n.+`).MatchString(text) {
		t.Error("the component prompt has an empty brief section")
	}
}

func TestReviseComponentVariantIsRefusedOnAPublicOnlyChain(t *testing.T) {
	g := planned(t, map[string]string{"revise.fn-greet.txt": descriptionPatch, "testwrite.fn-greet-r1.txt": testwriteReply(t, revisedGreetTest)})
	g.cfg.Providers[0].Visibility = settings.Public
	g.cfg.Revise.Context = "component"
	rec := &recCaller{inner: g.router}
	deps := g.deps
	deps.Caller = rec
	before := len(g.stagesCalled())
	_, err := planner.NewReviser(deps).Revise(context.Background(), reviseReq(t, g))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || !ce.OnlyPrivacy() {
		t.Fatalf("err = %v, want a privacy refusal", err)
	}
	if n := len(g.stagesCalled()) - before; n != 0 {
		t.Fatalf("%d request(s) reached a public provider", n)
	}
	// Node scope is allowed for a public provider under need_to_know.
	g.cfg.Revise.Context = "node"
	if prop, err := planner.NewReviser(deps).Revise(context.Background(), reviseReq(t, g)); err != nil || prop.Outcome != planner.OutcomeRewritten {
		t.Fatalf("node scope on a public provider: %+v, %v", prop, err)
	}
}
