package revlog_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/revlog"
)

const node0 = `{"id":"fn-a","kind":"function","parent":"c","depends_on":[],"wave":1,"description":"old","contract":{"package":"p","file":"p/a.go","signature":"func A()","inputs":[],"outputs":[],"errors":[]},"context":{"constraints":["x"],"dependency_signatures":[]},"rationale":"why","revision":0}`
const node1 = `{"id":"fn-a","kind":"function","parent":"c","depends_on":[],"wave":1,"description":"new","contract":{"package":"p","file":"p/a.go","signature":"func A()","inputs":[],"outputs":[],"errors":[]},"context":{"constraints":["x"],"dependency_signatures":[]},"rationale":"why","revision":1}`

type fx struct {
	t    *testing.T
	dir  string
	disk revlog.Disk
}

func newFx(t *testing.T) *fx {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fn-a")
	test0, test1 := []byte("package p // t0\n"), []byte("package p // t1\n")
	if err := revlog.Stage(dir, 0, []byte(node0), test0); err != nil {
		t.Fatal(err)
	}
	if err := revlog.Stage(dir, 1, []byte(node1), test1); err != nil {
		t.Fatal(err)
	}
	h := revlog.Hash
	in := revlog.Record{Kind: revlog.KindIntent, N: 1, Trigger: "ladder_exhausted", BaseSHA256: h([]byte(node0)), BodySHA256: h([]byte(node1)),
		TestSHA256Before: h(test0), TestSHA256After: h(test1), Changed: []string{"description"}, At: "2026-10-09T00:00:00Z"}
	cm := in
	cm.Kind = revlog.KindCommit
	for _, r := range []revlog.Record{in, cm} {
		if err := revlog.Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	return &fx{t: t, dir: dir, disk: revlog.Disk{NodeSHA256: h([]byte(node1)), LeafTestSHA256: h(test1), TestFileSHA256: h(test1)}}
}

func TestVerifyAcceptsACommittedChain(t *testing.T) {
	f := newFx(t)
	tip, err := revlog.Verify(f.dir, f.disk)
	if err != nil || tip.N != 1 || tip.BodySHA256 != revlog.Hash([]byte(node1)) || tip.Open != nil {
		t.Fatalf("Verify = %+v, %v", tip, err)
	}
}

func TestVerifyRefusesEveryTamper(t *testing.T) {
	h := revlog.Hash
	cases := map[string]func(f *fx){
		"node file edited":        func(f *fx) { f.disk.NodeSHA256 = h([]byte("edited")) },
		"leaf_tests entry edited": func(f *fx) { f.disk.LeafTestSHA256 = h([]byte("edited")) },
		"test file edited":        func(f *fx) { f.disk.TestFileSHA256 = h([]byte("edited")) },
		"body-0 edited": func(f *fx) {
			os.WriteFile(filepath.Join(f.dir, revlog.BodyFile(0)), []byte(strings.Replace(node0, "old", "older", 1)), 0o600)
		},
		"body-1 edited": func(f *fx) {
			os.WriteFile(filepath.Join(f.dir, revlog.BodyFile(1)), []byte(strings.Replace(node1, "new", "newer", 1)), 0o600)
		},
		"intent line deleted": func(f *fx) {
			p := filepath.Join(f.dir, "log.jsonl")
			raw, _ := os.ReadFile(p)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			os.WriteFile(p, []byte(lines[1]+"\n"), 0o600)
		},
		"lines swapped": func(f *fx) {
			p := filepath.Join(f.dir, "log.jsonl")
			raw, _ := os.ReadFile(p)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			os.WriteFile(p, []byte(lines[1]+"\n"+lines[0]+"\n"), 0o600)
		},
		"unknown field in a record": func(f *fx) {
			p := filepath.Join(f.dir, "log.jsonl")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(raw), `"kind"`, `"extra":1,"kind"`, 1)), 0o600)
		},
		"frozen field changed in the tip body": func(f *fx) {
			bad := strings.Replace(node1, `"signature":"func A()"`, `"signature":"func B()"`, 1)
			os.WriteFile(filepath.Join(f.dir, revlog.BodyFile(1)), []byte(bad), 0o600)
			f.disk.NodeSHA256 = h([]byte(bad))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFx(t)
			mutate(f)
			if _, err := revlog.Verify(f.dir, f.disk); !errors.Is(err, revlog.ErrChain) {
				t.Fatalf("Verify = %v, want ErrChain", err)
			}
		})
	}
}

func TestVerifyOpenIntentAndRefusal(t *testing.T) {
	h := revlog.Hash
	// An intent with no commit: the node may be at the base or at the new body.
	f := newFx(t)
	raw, _ := os.ReadFile(filepath.Join(f.dir, "log.jsonl"))
	first := strings.SplitN(string(raw), "\n", 2)[0] + "\n"
	os.WriteFile(filepath.Join(f.dir, "log.jsonl"), []byte(first), 0o600)
	for _, d := range []revlog.Disk{
		{NodeSHA256: h([]byte(node0)), LeafTestSHA256: h([]byte("package p // t0\n")), TestFileSHA256: h([]byte("package p // t0\n"))},
		{NodeSHA256: h([]byte(node1)), LeafTestSHA256: h([]byte("package p // t1\n")), TestFileSHA256: h([]byte("package p // t0\n"))},
	} {
		tip, err := revlog.Verify(f.dir, d)
		if err != nil || tip.Open == nil || tip.Open.N != 1 || tip.N != 0 {
			t.Fatalf("open intent: %+v, %v", tip, err)
		}
	}
	if _, err := revlog.Verify(f.dir, revlog.Disk{NodeSHA256: h([]byte("other")), LeafTestSHA256: h([]byte("package p // t0\n")), TestFileSHA256: h([]byte("package p // t0\n"))}); !errors.Is(err, revlog.ErrChain) {
		t.Fatalf("open intent with a foreign node file: %v", err)
	}
	// A refused record with the same n closes the intent: the disk must be at revision 0.
	if err := revlog.Append(f.dir, revlog.Record{Kind: revlog.KindRefused, N: 1, Class: "revise_vacuous_test", At: "2026-10-09T00:00:01Z"}); err != nil {
		t.Fatal(err)
	}
	rev0 := revlog.Disk{NodeSHA256: h([]byte(node0)), LeafTestSHA256: h([]byte("package p // t0\n")), TestFileSHA256: h([]byte("package p // t0\n"))}
	if tip, err := revlog.Verify(f.dir, rev0); err != nil || tip.N != 0 || tip.Open != nil {
		t.Fatalf("after a refusal at revision 0: %+v, %v", tip, err)
	}
	if _, err := revlog.Verify(f.dir, f.disk); !errors.Is(err, revlog.ErrChain) {
		t.Fatalf("after a refusal with the new body on disk: %v", err)
	}
	// The executor removes the staged files of a refused revision: the chain still verifies.
	if err := revlog.RemoveStaged(f.dir, 1); err != nil {
		t.Fatal(err)
	}
	if tip, err := revlog.Verify(f.dir, rev0); err != nil || tip.N != 0 || tip.Open != nil {
		t.Fatalf("after a refusal and the removal of its staged files: %+v, %v", tip, err)
	}
}

func TestVerifyAcceptsOnlyFlagAndRefusedRecordsWithoutBodies(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fn-b")
	for _, r := range []revlog.Record{{Kind: revlog.KindFlag, Class: "contract_flagged", At: "t"}, {Kind: revlog.KindRefused, N: 1, Class: "revise_invalid", At: "t"}} {
		if err := revlog.Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if tip, err := revlog.Verify(dir, revlog.Disk{}); err != nil || tip.N != 0 {
		t.Fatalf("Verify = %+v, %v", tip, err)
	}
}

func TestCheckFrozenNamesTheField(t *testing.T) {
	for _, c := range []struct{ name, edit, field string }{
		{"signature", strings.Replace(node1, `"signature":"func A()"`, `"signature":"func B()"`, 1), "contract.signature"},
		{"depends_on", strings.Replace(node1, `"depends_on":[]`, `"depends_on":["x"]`, 1), "depends_on"},
		{"wave", strings.Replace(node1, `"wave":1`, `"wave":2`, 1), "wave"},
		{"rationale", strings.Replace(node1, `"why"`, `"other"`, 1), "rationale"},
		{"package", strings.Replace(node1, `"package":"p"`, `"package":"q"`, 1), "contract.package"},
		{"dependency_signatures", strings.Replace(node1, `"dependency_signatures":[]`, `"dependency_signatures":["func Z()"]`, 1), "context.dependency_signatures"},
	} {
		err := revlog.CheckFrozen([]byte(node0), []byte(c.edit))
		var fe *revlog.FrozenError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: err = %v, want field %s", c.name, err, c.field)
		}
	}
	if err := revlog.CheckFrozen([]byte(node0), []byte(node1)); err != nil {
		t.Fatalf("a description and revision change is allowed: %v", err)
	}
}

func TestNodeDirRefusesBadIDsAndLinks(t *testing.T) {
	run := t.TempDir()
	for _, id := range []string{"", "../x", "a/b", "Fn", ".hidden"} {
		if _, err := revlog.NodeDir(run, id); err == nil {
			t.Errorf("NodeDir accepted %q", id)
		}
	}
	elsewhere := t.TempDir()
	os.MkdirAll(filepath.Join(run, "_state"), 0o700)
	if err := os.Symlink(elsewhere, filepath.Join(run, "_state", "revisions")); err != nil {
		t.Fatal(err)
	}
	if _, err := revlog.NodeDir(run, "fn-a"); err == nil {
		t.Error("NodeDir followed a linked revisions folder")
	}
}

func TestFilesAreOwnerOnlyAndSummarize(t *testing.T) {
	f := newFx(t)
	for _, name := range []string{"log.jsonl", revlog.BodyFile(1), revlog.TestFile(1)} {
		fi, err := os.Stat(filepath.Join(f.dir, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v, want 0600", name, fi.Mode().Perm(), err)
		}
	}
	if fi, _ := os.Stat(f.dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", fi.Mode().Perm())
	}
	recs, err := revlog.Read(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	recs = append(recs, revlog.Record{Kind: revlog.KindRefused, N: 2, Class: "revise_noop"}, revlog.Record{Kind: revlog.KindFlag, Class: "contract_flagged"})
	s := revlog.Summarize(recs)
	if s.Commits != 1 || !s.TestsRewritten || s.Flag != "contract_flagged" || len(s.Refusals) != 1 || s.Refusals[0] != "revise_noop" || len(s.Changed) != 1 || s.Changed[0] != "description" {
		t.Fatalf("Summarize = %+v", s)
	}
}

// richNode holds every top-level field of the node schema, so the table below
// can change each frozen one in isolation.
const richNode = `{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"c","children":[],"title":"t","description":"old","brief_ref":"b","wave":1,"model_tier":"standard","revision":0,"depends_on":["d"],` +
	`"context":{"dependency_signatures":["func D()"],"files_read":["p/d.go"],"constraints":["x"],"notes":["n"]},` +
	`"contract":{"package":"p","file":"p/a.go","signature":"func A()","inputs":[],"outputs":[],"errors":[],"side_effects":[]},` +
	`"tests":[],"network":false,"secrets":[],"budget":{"max_revisions":2},"assumptions":[],"status":"pending","claim":{},"attempts":0,"result":{},"node_class":"pure",` +
	`"rationale":"why","requirement_ids":["r1"],"construction":"c","alternatives":["a"],"portability":"p","security":"s","performance":"f","observability":"o","refactor_notes":"rn","profile_hooks":[],"open_questions":[],"decision_ids":["d1"],"decisions":[]}`

// setField returns richNode with the dotted field (top-level, or contract.x / context.x) set to v.
func setField(t *testing.T, field string, v any) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(richNode), &doc); err != nil {
		t.Fatal(err)
	}
	if k, sub, ok := strings.Cut(field, "."); ok {
		doc[k].(map[string]any)[sub] = v
	} else {
		doc[field] = v
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCheckFrozenCoversEveryFrozenField(t *testing.T) {
	frozen := []string{"spec_version", "id", "kind", "parent", "children", "title", "brief_ref", "wave", "model_tier", "depends_on",
		"network", "secrets", "budget", "status", "claim", "attempts", "result", "node_class", "rationale", "requirement_ids",
		"alternatives", "refactor_notes", "open_questions", "decision_ids", "decisions",
		"contract.package", "contract.file", "contract.signature", "context.dependency_signatures", "context.files_read"}
	for _, f := range frozen {
		err := revlog.CheckFrozen([]byte(richNode), setField(t, f, "changed"))
		var fe *revlog.FrozenError
		if !errors.As(err, &fe) || fe.Field != f {
			t.Errorf("%s: err = %v, want FrozenError for %s", f, err, f)
		}
	}
	// A frozen field that is removed or added is a change too.
	var doc map[string]any
	_ = json.Unmarshal([]byte(richNode), &doc)
	delete(doc, "rationale")
	gone, _ := json.Marshal(doc)
	var fe *revlog.FrozenError
	if err := revlog.CheckFrozen([]byte(richNode), gone); !errors.As(err, &fe) || fe.Field != "rationale" {
		t.Errorf("removed rationale: %v", err)
	}
	extra := setField(t, "brand_new_field", "x")
	if err := revlog.CheckFrozen([]byte(richNode), extra); !errors.As(err, &fe) {
		t.Errorf("an unknown new top-level field must be frozen: %v", err)
	}
	// The fields a revision may change.
	for _, f := range []string{"description", "tests", "construction", "security", "observability", "performance", "portability", "profile_hooks",
		"assumptions", "revision", "contract.inputs", "contract.outputs", "contract.errors", "contract.side_effects", "context.constraints", "context.notes"} {
		if err := revlog.CheckFrozen([]byte(richNode), setField(t, f, []string{"changed"})); err != nil {
			t.Errorf("%s may change but CheckFrozen said: %v", f, err)
		}
	}
	if err := revlog.CheckFrozen([]byte("{"), []byte(richNode)); err == nil {
		t.Error("an unparsable base must be refused")
	}
}

// chainFx builds a log whose hash chain is VALID (intent, commit, staged files
// all agree) for a tip body that differs from revision 0, so Verify gets past
// every hash check and reaches CheckFrozen.
func chainFx(t *testing.T, tip []byte, commit bool) (string, revlog.Disk) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fn-a")
	h := revlog.Hash
	t0, t1 := []byte("package p // t0\n"), []byte("package p // t1\n")
	if err := revlog.Stage(dir, 0, []byte(richNode), t0); err != nil {
		t.Fatal(err)
	}
	if err := revlog.Stage(dir, 1, tip, t1); err != nil {
		t.Fatal(err)
	}
	in := revlog.Record{Kind: revlog.KindIntent, N: 1, Trigger: "ladder_exhausted", BaseSHA256: h([]byte(richNode)), BodySHA256: h(tip),
		TestSHA256Before: h(t0), TestSHA256After: h(t1), Changed: []string{"description"}, At: "t"}
	if err := revlog.Append(dir, in); err != nil {
		t.Fatal(err)
	}
	if commit {
		in.Kind = revlog.KindCommit
		if err := revlog.Append(dir, in); err != nil {
			t.Fatal(err)
		}
	}
	return dir, revlog.Disk{NodeSHA256: h(tip), LeafTestSHA256: h(t1), TestFileSHA256: h(t1)}
}

func TestVerifyReachesCheckFrozenWithAValidHashChain(t *testing.T) {
	good := setField(t, "description", "new")
	dir, disk := chainFx(t, good, true)
	if tip, err := revlog.Verify(dir, disk); err != nil || tip.N != 1 {
		t.Fatalf("control: a valid chain with only description changed must verify: %+v, %v", tip, err)
	}
	bad := setField(t, "contract.signature", "func B()")
	dir, disk = chainFx(t, bad, true)
	_, err := revlog.Verify(dir, disk)
	if !errors.Is(err, revlog.ErrChain) || !strings.Contains(err.Error(), "frozen field contract.signature") {
		t.Fatalf("committed tip with a frozen change = %v, want ErrChain naming the frozen field (not a hash mismatch)", err)
	}
	// An open (uncommitted) intent whose staged body changes a frozen field is refused too.
	dir, disk = chainFx(t, bad, false)
	_, err = revlog.Verify(dir, disk)
	if !errors.Is(err, revlog.ErrChain) || !strings.Contains(err.Error(), "frozen field contract.signature") {
		t.Fatalf("open intent with a frozen change = %v, want ErrChain naming the frozen field", err)
	}
}

// buildChain stages revisions 0..revs of richNode (description d<k>) and logs
// intent/commit pairs. forgeBefore[k] replaces the test_sha256_before of the
// intent (and commit) for revision k. With openLast the last revision has an
// intent and no commit; the returned Disk is then the base state of that
// revision, with the on-disk test hash equal to the intent's (possibly forged) Before.
func buildChain(t *testing.T, revs int, forgeBefore map[int]string, openLast bool) (string, revlog.Disk) {
	t.Helper()
	h := revlog.Hash
	dir := filepath.Join(t.TempDir(), "fn-a")
	bodies := [][]byte{[]byte(richNode)}
	tests := [][]byte{[]byte("package p // t0\n")}
	if err := revlog.Stage(dir, 0, bodies[0], tests[0]); err != nil {
		t.Fatal(err)
	}
	var disk revlog.Disk
	for k := 1; k <= revs; k++ {
		bodies = append(bodies, setField(t, "description", "d"+string(rune('0'+k))))
		tests = append(tests, []byte("package p // t"+string(rune('0'+k))+"\n"))
		if err := revlog.Stage(dir, k, bodies[k], tests[k]); err != nil {
			t.Fatal(err)
		}
		before := h(tests[k-1])
		if f, ok := forgeBefore[k]; ok {
			before = f
		}
		in := revlog.Record{Kind: revlog.KindIntent, N: k, Trigger: "ladder_exhausted", BaseSHA256: h(bodies[k-1]), BodySHA256: h(bodies[k]),
			TestSHA256Before: before, TestSHA256After: h(tests[k]), Changed: []string{"description"}, At: "t"}
		if err := revlog.Append(dir, in); err != nil {
			t.Fatal(err)
		}
		if openLast && k == revs {
			return dir, revlog.Disk{NodeSHA256: h(bodies[k-1]), LeafTestSHA256: before, TestFileSHA256: before}
		}
		in.Kind = revlog.KindCommit
		if err := revlog.Append(dir, in); err != nil {
			t.Fatal(err)
		}
		disk = revlog.Disk{NodeSHA256: h(bodies[k]), LeafTestSHA256: h(tests[k]), TestFileSHA256: h(tests[k])}
	}
	return dir, disk
}

func TestVerifyLinksIntentBeforeToThePreviousTip(t *testing.T) {
	forged := revlog.Hash([]byte("an arbitrary test file"))
	for _, c := range []struct {
		name string
		revs int
		open bool
	}{{"n=1 open", 1, true}, {"n=2 open", 2, true}, {"n=1 committed", 1, false}, {"n=2 committed", 2, false}} {
		t.Run(c.name, func(t *testing.T) {
			if dir, disk := buildChain(t, c.revs, nil, c.open); true {
				if _, err := revlog.Verify(dir, disk); err != nil {
					t.Fatalf("control (unforged): %v", err)
				}
			}
			dir, disk := buildChain(t, c.revs, map[int]string{c.revs: forged}, c.open)
			if _, err := revlog.Verify(dir, disk); !errors.Is(err, revlog.ErrChain) {
				t.Fatalf("forged Before = %v, want ErrChain", err)
			}
		})
	}
}

func TestVerifyTwoCommittedRevisions(t *testing.T) {
	h := revlog.Hash
	dir, disk := buildChain(t, 2, nil, false)
	b1, _, err := revlog.ReadStaged(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	b2, _, _ := revlog.ReadStaged(dir, 2)
	tip, err := revlog.Verify(dir, disk)
	if err != nil || tip.N != 2 || tip.BodySHA256 != h(b2) || tip.Open != nil {
		t.Fatalf("0 -> 1 -> 2: %+v, %v", tip, err)
	}
	recs, _ := revlog.Read(dir)
	if len(recs) != 4 || recs[2].Kind != revlog.KindIntent || recs[2].N != 2 || recs[2].BaseSHA256 != h(b1) {
		t.Fatalf("the n=2 intent must extend revision 1's body: %+v", recs)
	}
	// The tip must be revision 2: the disk at revision 1 no longer verifies.
	d1 := revlog.Disk{NodeSHA256: h(b1), LeafTestSHA256: h([]byte("package p // t1\n")), TestFileSHA256: h([]byte("package p // t1\n"))}
	if _, err := revlog.Verify(dir, d1); !errors.Is(err, revlog.ErrChain) {
		t.Fatalf("disk at revision 1 after commit 2 = %v, want ErrChain", err)
	}
	// Tamper body-1 after commit 2.
	p1 := filepath.Join(dir, revlog.BodyFile(1))
	if err := os.WriteFile(p1, []byte(strings.Replace(string(b1), "d1", "dX", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := revlog.Verify(dir, disk); !errors.Is(err, revlog.ErrChain) {
		t.Fatalf("body-1 tampered after commit 2 = %v, want ErrChain", err)
	}
	// Tamper the n=1 record after n=2 exists.
	var lp string
	var raw []byte
	var lines []string
	dir, disk = buildChain(t, 2, nil, false)
	lp = filepath.Join(dir, "log.jsonl")
	raw, _ = os.ReadFile(lp)
	lines = strings.Split(strings.TrimSpace(string(raw)), "\n")
	var r1 revlog.Record
	_ = json.Unmarshal([]byte(lines[0]), &r1)
	r1.BodySHA256 = h([]byte("other"))
	forged, _ := json.Marshal(r1)
	lines[0] = string(forged)
	os.WriteFile(lp, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	if _, err := revlog.Verify(dir, disk); !errors.Is(err, revlog.ErrChain) {
		t.Fatalf("n=1 record body hash forged after n=2 = %v, want ErrChain", err)
	}
}

func TestLogAndStagedFilesRefuseLinks(t *testing.T) {
	f := newFx(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	raw, _ := os.ReadFile(filepath.Join(f.dir, "log.jsonl"))
	os.WriteFile(elsewhere, raw, 0o600)
	lp := filepath.Join(f.dir, "log.jsonl")
	os.Remove(lp)
	if err := os.Symlink(elsewhere, lp); err != nil {
		t.Fatal(err)
	}
	if _, err := revlog.Read(f.dir); err == nil {
		t.Error("Read followed a linked log.jsonl")
	}
	if err := revlog.Append(f.dir, revlog.Record{Kind: revlog.KindFlag, Class: "contract_flagged", At: "t"}); err == nil {
		t.Error("Append followed a linked log.jsonl")
	}
	after, _ := os.ReadFile(elsewhere)
	if string(after) != string(raw) {
		t.Error("a write went through the link")
	}
	if _, err := revlog.Verify(f.dir, f.disk); err == nil {
		t.Error("Verify accepted a linked log")
	}

	g := newFx(t)
	bp := filepath.Join(g.dir, revlog.BodyFile(1))
	target := filepath.Join(t.TempDir(), "body")
	os.WriteFile(target, []byte(node1), 0o600)
	os.Remove(bp)
	os.Symlink(target, bp)
	if _, _, err := revlog.ReadStaged(g.dir, 1); err == nil {
		t.Error("ReadStaged followed a linked body")
	}
	if err := revlog.Stage(g.dir, 1, []byte(node1), []byte("package p // t1\n")); err == nil {
		t.Error("Stage wrote over a linked body")
	}
}

func TestStageZeroIsCreateOnly(t *testing.T) {
	f := newFx(t)
	if err := revlog.Stage(f.dir, 0, []byte("other"), []byte("other")); err == nil {
		t.Fatal("Stage(0) overwrote body-0")
	}
	if b, _, err := revlog.ReadStaged(f.dir, 0); err != nil || string(b) != node0 {
		t.Fatalf("body-0 changed: %v", err)
	}
	if err := revlog.Stage(f.dir, 5, []byte("a"), []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := revlog.Stage(f.dir, 5, []byte("b"), []byte("b")); err != nil {
		t.Fatalf("Stage(k>=1) must overwrite atomically: %v", err)
	}
	if b, _, _ := revlog.ReadStaged(f.dir, 5); string(b) != "b" {
		t.Fatal("overwrite did not take effect")
	}
}

func TestAppendAfterATornTail(t *testing.T) {
	f := newFx(t)
	lp := filepath.Join(f.dir, "log.jsonl")
	fh, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fh.WriteString(`{"kind":"fl`)
	fh.Close()
	if recs, err := revlog.Read(f.dir); err != nil || len(recs) != 2 {
		t.Fatalf("a torn last line must be ignored: %d, %v", len(recs), err)
	}
	if err := revlog.Append(f.dir, revlog.Record{Kind: revlog.KindFlag, Class: "contract_flagged", At: "t"}); err != nil {
		t.Fatal(err)
	}
	recs, err := revlog.Read(f.dir)
	if err != nil || len(recs) != 3 || recs[2].Kind != revlog.KindFlag {
		t.Fatalf("after Append: %d records, %v", len(recs), err)
	}
	if _, err := revlog.Verify(f.dir, f.disk); err != nil {
		t.Fatalf("Verify after a torn tail and an Append: %v", err)
	}
}

func TestAppendBoundsFieldLengths(t *testing.T) {
	long := strings.Repeat("x", 5000)
	many := make([]string, 200)
	for i := range many {
		many[i] = "f"
	}
	for name, r := range map[string]revlog.Record{
		"model":       {Kind: revlog.KindFlag, Model: long, At: "t"},
		"class":       {Kind: revlog.KindFlag, Class: long, At: "t"},
		"assumption":  {Kind: revlog.KindFlag, Assumptions: []string{long}, At: "t"},
		"assumptions": {Kind: revlog.KindFlag, Assumptions: many, At: "t"},
		"changed":     {Kind: revlog.KindFlag, Changed: many, At: "t"},
		"changed one": {Kind: revlog.KindFlag, Changed: []string{long}, At: "t"},
		"hash":        {Kind: revlog.KindFlag, BodySHA256: long, At: "t"},
	} {
		dir := filepath.Join(t.TempDir(), "fn-a")
		err := revlog.Append(dir, r)
		if err == nil || strings.Contains(err.Error(), "xxxx") {
			t.Errorf("%s: err = %v, want a fixed refusal", name, err)
		}
		if _, serr := os.Stat(filepath.Join(dir, "log.jsonl")); serr == nil {
			t.Errorf("%s: a refused record was written", name)
		}
	}
	if err := revlog.Append(filepath.Join(t.TempDir(), "fn-a"), revlog.Record{Kind: revlog.KindFlag, Model: "m", Assumptions: []string{"revision 1: ok"}, At: "t"}); err != nil {
		t.Fatalf("a normal record: %v", err)
	}
}

func TestCheckFrozenNonObjectContractAndContext(t *testing.T) {
	var fe *revlog.FrozenError
	for _, k := range []string{"contract", "context"} {
		mk := func(v string) []byte { return []byte(`{"id":"a","` + k + `":` + v + `}`) }
		if err := revlog.CheckFrozen(mk(`"x"`), mk(`"y"`)); !errors.As(err, &fe) || fe.Field != k {
			t.Errorf("%s: differing strings = %v", k, err)
		}
		if err := revlog.CheckFrozen(mk(`{"a":1}`), mk(`"y"`)); !errors.As(err, &fe) || fe.Field != k {
			t.Errorf("%s: object vs string = %v", k, err)
		}
		if err := revlog.CheckFrozen(mk(`[1]`), mk(`null`)); !errors.As(err, &fe) || fe.Field != k {
			t.Errorf("%s: array vs null = %v", k, err)
		}
		if err := revlog.CheckFrozen(mk(`"x"`), mk(`"x"`)); err != nil {
			t.Errorf("%s: equal strings = %v", k, err)
		}
	}
}
