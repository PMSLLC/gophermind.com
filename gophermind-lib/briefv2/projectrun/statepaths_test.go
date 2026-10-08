package projectrun

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func spByAction(ps []StatePath, action string) []StatePath {
	var out []StatePath
	for _, p := range ps {
		if p.Action == action {
			out = append(out, p)
		}
	}
	return out
}

func spFind(t *testing.T, ps []StatePath, action, path string) StatePath {
	t.Helper()
	for _, p := range ps {
		if p.Action == action && p.Path == path {
			return p
		}
	}
	t.Fatalf("no %s line for %q in %+v", action, path, ps)
	return StatePath{}
}

// snapshot maps every path under the roots to its size and content hash input.
func treeSnap(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				m[p] = "dir"
				return nil
			}
			b, _ := os.ReadFile(p)
			m[p] = string(b)
			return nil
		})
	}
	return m
}

func TestStatePathsListEverything(t *testing.T) {
	r := newRig(t)
	r.declare("DATABASE_URL", "TEST_DATABASE_URL")
	ps, err := StatePaths(r.o, r.b, r.env)
	if err != nil {
		t.Fatal(err)
	}
	id := rigRunID
	g := filepath.Join(r.repo, ".gophermind")
	want := []StatePath{
		{"delete_dir", "per-project-repo", filepath.Join(g, id)},
		{"delete_dir", "per-project-repo", filepath.Join(g, id+"-scratch")},
		{"keep", "per-project-repo", filepath.Join(r.repo, ".git", "info", "exclude")},
		{"git_branch_delete", "per-project-repo-git", "gm/" + id},
		{"git_reset", "per-project-repo-git", r.repo},
		{"delete_file", "per-project-global", filepath.Join(r.cfgDir, "runs", id+".json")},
		{"keep", "global", r.vaultPath},
		{"keep", "global", filepath.Join(r.cfgDir, "gophermind.yaml")},
		{"keep", "global", r.cfg.Executor.GoModCache},
	}
	if len(ps) != len(want)+1 {
		t.Fatalf("got %d lines, want %d: %+v", len(ps), len(want)+1, ps)
	}
	for i, w := range want {
		if ps[i] != w {
			t.Errorf("line %d = %+v, want %+v", i, ps[i], w)
		}
	}
	ext := ps[len(ps)-1]
	if ext.Action != "external" || ext.Scope != "external" ||
		ext.Path != "declared secrets: DATABASE_URL, TEST_DATABASE_URL; drop and recreate the databases behind the postgres URLs" {
		t.Errorf("external line %+v", ext)
	}
}

func TestStatePathsDefaultModCacheWithoutSettings(t *testing.T) {
	r := newRig(t)
	os.Remove(filepath.Join(r.cfgDir, "gophermind.yaml"))
	ps, err := StatePaths(r.o, r.b, r.env)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	spFind(t, ps, "keep", filepath.Join(home, ".gophermind", "gomodcache"))
	if _, err := os.Stat(filepath.Join(r.cfgDir, "gophermind.yaml")); err == nil {
		t.Fatal("StatePaths created the settings file")
	}
}

func TestStatePathsNoSideEffects(t *testing.T) {
	r := newRig(t)
	r.seed("harness", map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:5432/d"})
	var calls int32
	r.env.Dial = func(context.Context, string) error { atomic.AddInt32(&calls, 1); return errors.New("no") }
	r.env.Backends = func() (blackboard.Blackboard, ledger.Ledger) { atomic.AddInt32(&calls, 1); return nil, nil }
	r.env.Git = func(context.Context, string, ...string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("no")
	}
	vdir := filepath.Dir(r.vaultPath)
	before := treeSnap(t, r.cfgDir, r.repo, vdir)
	if _, err := StatePaths(r.o, r.b, r.env); err != nil {
		t.Fatal(err)
	}
	after := treeSnap(t, r.cfgDir, r.repo, vdir)
	if len(before) != len(after) {
		t.Fatalf("entries before %d after %d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
	if calls != 0 {
		t.Fatalf("Dial, Backends or Git called %d times", calls)
	}
}

func TestStatePathsNoSecrets(t *testing.T) {
	r := newRig(t)
	const canary = "CANARY-pw-7731"
	r.declare("DATABASE_URL")
	r.seed("harness", map[string]string{"DATABASE_URL": "postgres://u:" + canary + "@127.0.0.1:5432/d"})
	ps, err := StatePaths(r.o, r.b, r.env)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	PrintStatePaths(&buf, ps)
	if strings.Contains(buf.String(), canary) {
		t.Fatalf("secret value printed: %s", buf.String())
	}
}

func TestStatePathsRepoOverride(t *testing.T) {
	r := newRig(t)
	other := t.TempDir()
	r.b.Front.Repo = "/nonexistent/brief/repo"
	r.o.Repo = other
	ps, err := StatePaths(r.o, r.b, r.env)
	if err != nil {
		t.Fatal(err)
	}
	spFind(t, ps, "delete_dir", filepath.Join(other, ".gophermind", rigRunID))
	spFind(t, ps, "git_reset", other)
	for _, p := range ps {
		if strings.Contains(p.Path, "/nonexistent/brief/repo") {
			t.Errorf("brief path leaked: %+v", p)
		}
	}
}

func TestStatePathsHasNoDatabaseRow(t *testing.T) {
	r := newRig(t)
	ps, err := StatePaths(r.o, r.b, r.env)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		low := strings.ToLower(p.Action + " " + p.Path)
		if strings.Contains(low, "rows") || strings.Contains(low, "git_clean") || strings.Contains(low, ".db") || strings.Contains(low, "sqlite") {
			t.Errorf("database or clean line: %+v", p)
		}
	}
	dirs := spByAction(ps, "delete_dir")
	if len(dirs) != 2 {
		t.Fatalf("delete_dir lines %+v", dirs)
	}
	if ext := spByAction(ps, "external"); len(ext) != 1 {
		t.Fatalf("external lines %+v", ext)
	}
}

func TestStatePathsRefusesBadIDs(t *testing.T) {
	for _, id := range []string{"", "../x", "/abs", "gm-2026-10-08-001/../../x", "gm-2026-10-08-1"} {
		r := newRig(t)
		r.b.Front.ID = id
		if _, err := StatePaths(r.o, r.b, r.env); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestPrintStatePathsFormat(t *testing.T) {
	var buf bytes.Buffer
	PrintStatePaths(&buf, []StatePath{{"keep", "global", "/p"}})
	if buf.String() != "keep\tglobal\t/p\n" {
		t.Fatalf("%q", buf.String())
	}
}
