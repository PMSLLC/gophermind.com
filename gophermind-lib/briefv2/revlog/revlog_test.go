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
