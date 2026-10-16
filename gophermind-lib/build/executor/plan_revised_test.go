package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/revlog"
	"gophermind/gophermind-lib/build/tree"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// forgeRevision writes a valid committed chain for the node by hand: the way
// the revise stage leaves a run folder after one rewrite of the description.
// It returns the staged revision 0 and 1 bodies.
func forgeRevision(t *testing.T, g *rig, id, description string) (body0, body1 []byte) {
	t.Helper()
	key := g.plan.nodeKey(id)
	nodePath := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(key, "tree/")))
	body0 = readFile(t, nodePath)
	var doc map[string]any
	if err := json.Unmarshal(body0, &doc); err != nil {
		t.Fatal(err)
	}
	doc["description"], doc["revision"] = description, 1
	doc["assumptions"] = append(doc["assumptions"].([]any), "revision 1: forged by the test")
	raw, _ := json.Marshal(doc)
	n, err := tree.ParseNode(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := n.Marshal()
	body1 = append(b, '\n')
	test := readFile(t, filepath.Join(g.repo, filepath.FromSlash(g.plan.Leaf(id).TestFile)))
	dir, err := revlog.NodeDir(g.runDir, id)
	if err != nil {
		t.Fatal(err)
	}
	for k, body := range [][]byte{body0, body1} {
		if err := revlog.Stage(dir, k, body, test); err != nil {
			t.Fatal(err)
		}
	}
	in := revlog.Record{Kind: revlog.KindIntent, N: 1, Trigger: "ladder_exhausted", BaseSHA256: revlog.Hash(body0), BodySHA256: revlog.Hash(body1),
		TestSHA256Before: revlog.Hash(test), TestSHA256After: revlog.Hash(test), Changed: []string{"description"}, At: "2026-10-09T00:00:00Z"}
	cm := in
	cm.Kind = revlog.KindCommit
	for _, r := range []revlog.Record{in, cm} {
		if err := revlog.Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := tree.NewStore(g.runDir).Write(n); err != nil {
		t.Fatal(err)
	}
	return body0, body1
}

func TestLoadPlanAcceptsARevisedNodeAndTheApprovalStillHolds(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	_, h0, err := planner.RenderPlan(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	body0, body1 := forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
	p, err := LoadPlan(g.runDir, g.repo)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	key := p.nodeKey(leafID)
	if p.Hashes[key] != revlog.Hash(body0) {
		t.Fatalf("Hashes[%s] = %s, want the hash of body-0", key, p.Hashes[key])
	}
	if tip := p.Revised[leafID]; tip.N != 1 || tip.BodySHA256 != revlog.Hash(body1) || tip.Open != nil {
		t.Fatalf("tip = %+v", tip)
	}
	l := p.Leaf(leafID)
	if l.Revision != 1 || l.Description != "REVISED-SENTENCE for the greeting." || len(l.Behaviour) == 0 {
		t.Fatalf("leaf = revision %d description %q behaviour %v", l.Revision, l.Description, l.Behaviour)
	}
	if v := p.View(l, "src"); v.Description != l.Description || len(v.Behaviour) != len(l.Behaviour) {
		t.Fatal("View does not carry the revised text to the packer")
	}
	// The approval is untouched: same plan hash, VerifyApproval passes.
	if _, h1, err := planner.RenderPlan(g.runDir); err != nil || h1 != h0 {
		t.Fatalf("plan hash before %s after %s (%v)", h0, h1, err)
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("VerifyApproval: %v", err)
	}
	// An unrevised leaf of the same plan is untouched.
	if other := p.Leaf("fn-farewell"); other != nil && (other.Revision != 0 || other.Description != "" || other.Behaviour != nil) {
		t.Fatal("an unrevised leaf carries revised text")
	}
}

func TestLoadPlanRefusesATamperedChain(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, g *rig, dir string, key string){
		"the revised node edited by hand": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(key, "tree/")))
			os.WriteFile(p, []byte(strings.Replace(string(readFile(t, p)), "REVISED-SENTENCE", "SNEAKY-SENTENCE", 1)), 0o600)
		},
		"body-0 edited": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(dir, revlog.BodyFile(0))
			os.WriteFile(p, append(readFile(t, p), ' '), 0o600)
		},
		"a log line deleted": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(dir, "log.jsonl")
			lines := strings.Split(strings.TrimSpace(string(readFile(t, p))), "\n")
			os.WriteFile(p, []byte(lines[1]+"\n"), 0o600)
		},
		"the log lines swapped": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(dir, "log.jsonl")
			lines := strings.Split(strings.TrimSpace(string(readFile(t, p))), "\n")
			os.WriteFile(p, []byte(lines[1]+"\n"+lines[0]+"\n"), 0o600)
		},
		"leaf_tests.json changed": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(g.runDir, "_state", "leaf_tests.json")
			var m map[string]planner.LeafTest
			json.Unmarshal(readFile(t, p), &m)
			lt := m[leafID]
			lt.SHA256 = strings.Repeat("0", 64)
			m[leafID] = lt
			raw, _ := json.Marshal(m)
			os.WriteFile(p, raw, 0o600)
		},
		"the test file edited": func(t *testing.T, g *rig, dir, key string) {
			p := filepath.Join(g.repo, filepath.FromSlash(g.plan.Leaf(leafID).TestFile))
			os.WriteFile(p, append(readFile(t, p), []byte("\n// sneaky\n")...), 0o644)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			mutate(t, g, dir, g.plan.nodeKey(leafID))
			_, err := LoadPlan(g.runDir, g.repo)
			asStop(t, err, "failed", "plan_changed")
		})
	}
}

func TestPlanIntactComparesARevisedNodeToItsTip(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
	p, err := LoadPlan(g.runDir, g.repo)
	if err != nil {
		t.Fatal(err)
	}
	rc := &runCtx{o: Options{RunDir: g.runDir}, plan: p}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact on a valid chain: %+v", se)
	}
	// A hand edit of the revised node, or of an unrevised one, stops the run.
	for _, id := range []string{leafID, "fn-farewell"} {
		path := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(p.nodeKey(id), "tree/")))
		orig := readFile(t, path)
		os.WriteFile(path, append(append([]byte{}, orig...), ' '), 0o600)
		if se := rc.planIntact(); se == nil || se.Reason != "plan_changed" {
			t.Errorf("a hand edit of %s was not caught: %+v", id, se)
		}
		os.WriteFile(path, orig, 0o600)
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("restored files: %+v", se)
	}
}

func TestReloadLeafUpdatesTheLeafInPlaceAndRefusesAFrozenChange(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	l := g.plan.Leaf(leafID)
	if l.Revision != 0 {
		t.Fatal("setup")
	}
	forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
	if err := g.plan.ReloadLeaf(leafID); err != nil {
		t.Fatal(err)
	}
	if g.plan.Leaf(leafID) != l || l.Revision != 1 || l.Description != "REVISED-SENTENCE for the greeting." {
		t.Fatalf("leaf after reload = %+v", l)
	}
	// A node file whose signature moved is refused.
	path := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(g.plan.nodeKey(leafID), "tree/")))
	raw := readFile(t, path)
	os.WriteFile(path, []byte(strings.Replace(string(raw), l.Signature, "func Greet(who string) (string, error)", 1)), 0o600)
	if err := g.plan.ReloadLeaf(leafID); err == nil {
		t.Fatal("ReloadLeaf accepted a changed signature")
	}
}

func TestLoadPlanRefusesAMissingOrEmptiedLogAndAMissingChain(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, g *rig, dir string){
		"log.jsonl removed": func(t *testing.T, g *rig, dir string) {
			if err := os.Remove(filepath.Join(dir, "log.jsonl")); err != nil {
				t.Fatal(err)
			}
		},
		"log.jsonl emptied": func(t *testing.T, g *rig, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "log.jsonl"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the revisions dir of the node deleted": func(t *testing.T, g *rig, dir string) {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		},
		"the node revision field edited": func(t *testing.T, g *rig, dir string) {
			// A coherent forgery: the node file and nothing else changes, so only
			// the revision field disagrees with the chain tip.
			p := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(g.plan.nodeKey(leafID), "tree/")))
			raw := readFile(t, p)
			if !strings.Contains(string(raw), `"revision": 1`) && !strings.Contains(string(raw), `"revision":1`) {
				t.Fatalf("setup: no revision 1 in the node file")
			}
			os.WriteFile(p, []byte(strings.Replace(strings.Replace(string(raw), `"revision": 1`, `"revision": 2`, 1), `"revision":1`, `"revision":2`, 1)), 0o600)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			mutate(t, g, dir)
			_, err := LoadPlan(g.runDir, g.repo)
			asStop(t, err, "failed", "plan_changed")
		})
	}
}

func TestLoadPlanRefusesAnUnrevisedNodeThatClaimsARevision(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	p := filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(g.plan.nodeKey(leafID), "tree/")))
	raw := string(readFile(t, p))
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	doc["revision"] = 1
	n, err := tree.ParseNode(mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.NewStore(g.runDir).Write(n); err != nil {
		t.Fatal(err)
	}
	_, err = LoadPlan(g.runDir, g.repo)
	asStop(t, err, "failed", "plan_changed")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An open intent (staged, not yet committed) lets planIntact accept the last
// committed hash or the open body for THAT node only.
func TestPlanIntactAcceptsTheOpenBodyForItsOwnNodeOnly(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	body0, body1 := forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	logPath := filepath.Join(dir, "log.jsonl")
	lines := strings.Split(strings.TrimSpace(string(readFile(t, logPath))), "\n")
	if err := os.WriteFile(logPath, []byte(lines[0]+"\n"), 0o600); err != nil { // the commit record is not there yet
		t.Fatal(err)
	}
	p, err := LoadPlan(g.runDir, g.repo)
	if err != nil {
		t.Fatalf("LoadPlan with an open intent: %v", err)
	}
	if tip := p.Revised[leafID]; tip.Open == nil || tip.N != 0 {
		t.Fatalf("tip = %+v", tip)
	}
	rc := &runCtx{o: Options{RunDir: g.runDir}, plan: p}
	path := func(id string) string {
		return filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(p.nodeKey(id), "tree/")))
	}
	if se := rc.planIntact(); se != nil { // node file holds the open body
		t.Fatalf("open body refused: %+v", se)
	}
	os.WriteFile(path(leafID), body0, 0o600) // the committed side
	if se := rc.planIntact(); se != nil {
		t.Fatalf("base body refused: %+v", se)
	}
	os.WriteFile(path(leafID), body1, 0o600)
	other := path("fn-farewell")
	orig := readFile(t, other)
	os.WriteFile(other, append(append([]byte{}, orig...), ' '), 0o600)
	if se := rc.planIntact(); se == nil || se.Reason != "plan_changed" {
		t.Fatalf("an edit of the other node was not caught: %+v", se)
	}
	os.WriteFile(other, orig, 0o600)
	os.WriteFile(path(leafID), append(append([]byte{}, body1...), ' '), 0o600)
	if se := rc.planIntact(); se == nil || se.Reason != "plan_changed" {
		t.Fatalf("a third body for the open node was accepted: %+v", se)
	}
}

// TestPlanIntactReprovesTheRevisionChain is spec R3 during a run: an edit of
// body-0.json, of a log line, or of leaf_tests.json after the plan was loaded is
// plan_changed at planIntact, as it is at LoadPlan.
func TestPlanIntactReprovesTheRevisionChain(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, g *rig, dir string){
		"body-0.json edited": func(t *testing.T, g *rig, dir string) {
			p := filepath.Join(dir, revlog.BodyFile(0))
			os.WriteFile(p, append(readFile(t, p), ' '), 0o600)
		},
		"the commit line deleted": func(t *testing.T, g *rig, dir string) {
			p := filepath.Join(dir, "log.jsonl")
			lines := strings.SplitAfter(string(readFile(t, p)), "\n")
			os.WriteFile(p, []byte(lines[0]), 0o600)
		},
		"a log line edited": func(t *testing.T, g *rig, dir string) {
			p := filepath.Join(dir, "log.jsonl")
			raw := string(readFile(t, p))
			i := strings.LastIndex(raw, `"body_sha256":"`) + len(`"body_sha256":"`)
			c := byte('0')
			if raw[i] == '0' {
				c = '1'
			}
			os.WriteFile(p, []byte(raw[:i]+string(c)+raw[i+1:]), 0o600)
		},
		"leaf_tests.json edited": func(t *testing.T, g *rig, dir string) {
			if err := planner.SetLeafTestSHA(g.runDir, leafID, strings.Repeat("ab", 32)); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			forgeRevision(t, g, leafID, "REVISED-SENTENCE for the greeting.")
			p, err := LoadPlan(g.runDir, g.repo)
			if err != nil {
				t.Fatal(err)
			}
			rc := &runCtx{o: Options{RunDir: g.runDir, Repo: g.repo}, plan: p}
			if se := rc.planIntact(); se != nil {
				t.Fatalf("planIntact on a valid chain: %+v", se)
			}
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			mutate(t, g, dir)
			if se := rc.planIntact(); se == nil || se.Reason != "plan_changed" {
				t.Fatalf("%s mid-run was not caught by planIntact: %+v", name, se)
			}
		})
	}
}
