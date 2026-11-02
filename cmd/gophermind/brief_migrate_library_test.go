package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/library"
	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
	"gophermind/gophermind-lib/serve"
)

// The fake tree: csvstat has five runs over two dates (four on 2026-10-29),
// slugify two runs on two dates, roman one. Old numbers interleave across
// titles the way the server's global counter did.
var migFixture = []struct{ old, title string }{
	{"gm-2026-10-28-001", "slugify"},
	{"gm-2026-10-28-007", "csvstat"},
	{"gm-2026-10-29-001", "csvstat"},
	{"gm-2026-10-29-002", "csvstat"},
	{"gm-2026-10-29-005", "csvstat"},
	{"gm-2026-10-29-010", "csvstat"},
	{"gm-2026-10-30-001", "Roman"},
	{"gm-2026-10-30-002", "slugify"},
}

var migExpected = map[string]string{
	"gm-2026-10-28-001": "slugify-v1-20261028-r001",
	"gm-2026-10-28-007": "csvstat-v1-20261028-r001",
	"gm-2026-10-29-001": "csvstat-v1-20261029-r001",
	"gm-2026-10-29-002": "csvstat-v1-20261029-r002",
	"gm-2026-10-29-005": "csvstat-v1-20261029-r003",
	"gm-2026-10-29-010": "csvstat-v1-20261029-r004",
	"gm-2026-10-30-001": "roman-v1-20261030-r001",
	"gm-2026-10-30-002": "slugify-v1-20261030-r001",
}

const migUnrelated = "gm-2026-09-29-900"

func migBrief(old, repo, title string) string {
	return "---\nspec_version: \"2.0\"\nid: " + old + "\nrepo: " + repo + "\ntitle: " + title +
		"\nlanguage: go\n---\n# " + title + "\n\nThe " + title + " tool.\n"
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// migTree builds the fake tree under a temp root and returns the root.
func migTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	briefs, repos, runs := filepath.Join(root, "briefs"), filepath.Join(root, "repos"), filepath.Join(root, "runs")
	for i, f := range migFixture {
		repo := filepath.Join(repos, f.old)
		src := migBrief(f.old, repo, f.title)
		mustWrite(t, filepath.Join(briefs, f.old, "brief.md"), src)
		mustWrite(t, filepath.Join(briefs, f.old, "run.json"), fmt.Sprintf(
			"{\n  \"id\": %q,\n  \"title\": %q,\n  \"phase\": \"done\",\n  \"started_at\": \"2026-10-29T10:00:00Z\",\n  \"ask\": true\n}\n", f.old, f.title))
		mustWrite(t, filepath.Join(briefs, f.old, "run.log"), "planning "+f.old+" in "+repo+"\n")
		// The repo working copy: a run folder that runview can read, the
		// run's brief and a calls log that name the old id and repo path.
		runviewtest.BuildID(t, repo, f.old, runviewtest.Verified)
		dir := filepath.Join(repo, ".gophermind", f.old)
		mustWrite(t, filepath.Join(dir, "brief.md"), src)
		mustWrite(t, filepath.Join(dir, "_state", "calls.jsonl"), fmt.Sprintf(
			"{\"stage\":\"clarify\",\"prompt\":\"id: %s\\nrepo: %s\\n\"}\n{\"stage\":\"contract\",\"module\":\"github.com/jbrahy/%s\"}\n", f.old, repo, f.old))
		mustWrite(t, filepath.Join(dir, "logs", "run.log"), "log of "+f.old+"\n")
		mustWrite(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
		cfg := "[core]\n\trepositoryformatversion = 0\n"
		if i == 0 {
			cfg += "\tworktree = " + repo + "\n"
		}
		mustWrite(t, filepath.Join(repo, ".git", "config"), cfg)
		mustWrite(t, filepath.Join(repo, "go.mod"), "module github.com/jbrahy/"+f.old+"\n")
		mustWrite(t, filepath.Join(runs, f.old+".json"), fmt.Sprintf(
			"{\n  \"run_id\": %q,\n  \"repo\": %q,\n  \"run_dir\": %q,\n  \"brief_path\": %q,\n  \"started_at\": \"2026-10-29T10:00:00Z\"\n}\n",
			f.old, repo, dir, filepath.Join(dir, "brief.md")))
	}
	mustWrite(t, filepath.Join(runs, migUnrelated+".json"), "{\"run_id\": \""+migUnrelated+"\", \"repo\": \"/elsewhere\"}\n")
	return root
}

// treeHash hashes every path, mode, link and file under root, leaving out
// backup dirs at the top.
func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() && filepath.Dir(rel) == "." && strings.HasPrefix(rel, migrateBackupStem) {
			return filepath.SkipDir
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		line := rel + " " + fi.Mode().String()
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			line += " -> " + l
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if errors.Is(err, fs.ErrPermission) {
				line += " unreadable"
				break
			}
			if err != nil {
				return err
			}
			s := sha256.Sum256(b)
			line += " " + hex.EncodeToString(s[:])
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	for _, l := range lines {
		h.Write([]byte(l + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func runMigrate(t *testing.T, root string, extra ...string) (int, string) {
	t.Helper()
	args := []string{"migrate-library", "--briefs", filepath.Join(root, "briefs"), "--repos", filepath.Join(root, "repos"),
		"--runs", filepath.Join(root, "runs"), "--server-url", "http://127.0.0.1:1"}
	args = append(args, extra...)
	var out, errw bytes.Buffer
	rc := runBrief(args, nil, &out, &errw)
	return rc, out.String() + errw.String()
}

func backups(t *testing.T, root string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(root, migrateBackupStem+"*"))
	return m
}

func TestMigrateDryRunChangesNothing(t *testing.T) {
	root := migTree(t)
	before := treeHash(t, root)
	rc, out := runMigrate(t, root)
	if rc != 0 {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	for old, nw := range migExpected {
		if !strings.Contains(out, old+" -> "+nw+"\n") {
			t.Errorf("missing %s -> %s in\n%s", old, nw, out)
		}
	}
	if !strings.Contains(out, "dry run: 8 renames across 3 families, 1 skipped") {
		t.Errorf("summary:\n%s", out)
	}
	if after := treeHash(t, root); after != before {
		t.Error("dry run changed the tree")
	}
	if len(backups(t, root)) != 0 || exists(filepath.Join(root, "briefs", "library")) {
		t.Error("dry run made a backup or a library dir")
	}
}

// checkMigrated asserts the tree is fully migrated.
func checkMigrated(t *testing.T, root string) {
	t.Helper()
	briefs, repos, runs := filepath.Join(root, "briefs"), filepath.Join(root, "repos"), filepath.Join(root, "runs")
	ents, _ := os.ReadDir(briefs)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if len(names) != len(migExpected)+1 {
		t.Errorf("briefs holds %v", names)
	}
	aliases, err := library.LoadAliases(filepath.Join(briefs, "library"))
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != len(migExpected) {
		t.Errorf("aliases %v", aliases)
	}
	titles := map[string]string{}
	for _, f := range migFixture {
		titles[f.old] = f.title
	}
	for old, nw := range migExpected {
		if exists(filepath.Join(briefs, old)) || exists(filepath.Join(repos, old)) || exists(filepath.Join(runs, old+".json")) {
			t.Errorf("%s still there", old)
		}
		if aliases.Resolve(old) != nw {
			t.Errorf("alias %s -> %s", old, aliases.Resolve(old))
		}
		newRepo := filepath.Join(repos, nw)
		want := migBrief(nw, newRepo, titles[old])
		if b, _ := os.ReadFile(filepath.Join(briefs, nw, "brief.md")); string(b) != want {
			t.Errorf("%s brief.md:\n%s", nw, b)
		}
		runDir := filepath.Join(newRepo, ".gophermind", nw)
		if b, _ := os.ReadFile(filepath.Join(runDir, "brief.md")); string(b) != want {
			t.Errorf("%s repo brief.md:\n%s", nw, b)
		}
		calls, _ := os.ReadFile(filepath.Join(runDir, "_state", "calls.jsonl"))
		if strings.Contains(string(calls), old) || !strings.Contains(string(calls), "id: "+nw) || !strings.Contains(string(calls), "repo: "+newRepo) {
			t.Errorf("%s calls.jsonl:\n%s", nw, calls)
		}
		if b, _ := os.ReadFile(filepath.Join(briefs, nw, "run.log")); string(b) != "planning "+old+" in "+filepath.Join(repos, old)+"\n" {
			t.Errorf("run.log changed: %s", b)
		}
		if b, _ := os.ReadFile(filepath.Join(runDir, "logs", "run.log")); string(b) != "log of "+old+"\n" {
			t.Errorf("logs/run.log changed: %s", b)
		}
		if b, _ := os.ReadFile(filepath.Join(newRepo, "go.mod")); string(b) != "module github.com/jbrahy/"+old+"\n" {
			t.Errorf("go.mod changed: %s", b)
		}
		var rec migRecord
		b, _ := os.ReadFile(filepath.Join(briefs, nw, "run.json"))
		if err := json.Unmarshal(b, &rec); err != nil || rec.ID != nw || rec.Family != strings.SplitN(nw, "-v1-", 2)[0] || rec.Version != "v1" || !rec.Ask || rec.Phase != "done" {
			t.Errorf("%s run.json %+v %v", nw, rec, err)
		}
		var runRec map[string]string
		b, _ = os.ReadFile(filepath.Join(runs, nw+".json"))
		if err := json.Unmarshal(b, &runRec); err != nil || runRec["run_id"] != nw || runRec["repo"] != newRepo ||
			runRec["run_dir"] != runDir || runRec["brief_path"] != filepath.Join(runDir, "brief.md") {
			t.Errorf("%s run record %v %v", nw, runRec, err)
		}
		if err := serve.CheckRun(briefs, repos, nw); err != nil {
			t.Errorf("run does not load: %v", err)
		}
		v, err := runview.Open(newRepo, nw)
		if err != nil {
			t.Fatal(err)
		}
		hasRoot := false
		for _, n := range v.State().Nodes {
			if n.ID == nw {
				hasRoot = true
			}
			if n.ID == old || n.Parent == old {
				t.Errorf("%s: node still names the old id: %+v", nw, n)
			}
		}
		if !hasRoot {
			t.Errorf("%s: no root node with the new id", nw)
		}
	}
	first := filepath.Join(repos, migExpected[migFixture[0].old], ".git", "config")
	if b, _ := os.ReadFile(first); !strings.Contains(string(b), "worktree = "+filepath.Join(repos, migExpected[migFixture[0].old])) {
		t.Errorf(".git/config not rewritten:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(runs, migUnrelated+".json")); !strings.Contains(string(b), "/elsewhere") {
		t.Error("unrelated run record changed")
	}
	st, err := library.Open(filepath.Join(briefs, "library"))
	if err != nil {
		t.Fatal(err)
	}
	fams, _ := st.List()
	if len(fams) != 3 {
		t.Fatalf("families %+v", fams)
	}
	for _, f := range fams {
		if len(f.Versions) != 1 || f.Versions[0].Name != "v1" || !f.Versions[0].Locked || f.Versions[0].Label != "imported" {
			t.Errorf("family %+v", f)
		}
		_, text, _ := st.Get(f.Name, "v1")
		if strings.Contains(string(text), "id:") || strings.Contains(string(text), "repo:") || !strings.Contains(string(text), "title: ") {
			t.Errorf("%s v1 text:\n%s", f.Name, text)
		}
	}
}

func TestMigrateApplyRenamesAll(t *testing.T) {
	root := migTree(t)
	rc, out := runMigrate(t, root, "--apply")
	if rc != 0 {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	checkMigrated(t, root)
	if !strings.Contains(out, "migrated: families 3, runs migrated 8, skipped 1, errors 0") {
		t.Errorf("counts:\n%s", out)
	}
	b := backups(t, root)
	if len(b) != 1 {
		t.Fatalf("backups %v", b)
	}
	for _, p := range []string{"briefs/gm-2026-10-29-001/brief.md", "repos/gm-2026-10-29-001/.gophermind/gm-2026-10-29-001/_state/calls.jsonl", "runs/gm-2026-10-29-001.json"} {
		if !exists(filepath.Join(b[0], p)) {
			t.Errorf("backup lacks %s", p)
		}
	}
	if exists(filepath.Join(b[0], "runs", migUnrelated+".json")) {
		t.Error("backup copied an unrelated run record")
	}
}

func TestMigrateRefusesWhenServerAnswers(t *testing.T) {
	root := migTree(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Write([]byte("ok"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	before := treeHash(t, root)
	rc, out := runMigrate(t, root, "--apply", "--server-url", srv.URL)
	if rc == 0 || !strings.Contains(out, "answers /healthz") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before || len(backups(t, root)) != 0 {
		t.Error("refused apply changed the tree")
	}
}

func TestMigrateRefusesWhenRunLive(t *testing.T) {
	for _, phase := range []string{"planning", "running", "waiting"} {
		root := migTree(t)
		p := filepath.Join(root, "briefs", "gm-2026-10-29-002", "run.json")
		b, _ := os.ReadFile(p)
		mustWrite(t, p, strings.Replace(string(b), `"done"`, `"`+phase+`"`, 1))
		before := treeHash(t, root)
		rc, out := runMigrate(t, root, "--apply")
		if rc == 0 || !strings.Contains(out, "gm-2026-10-29-002 ("+phase+")") {
			t.Fatalf("%s: rc %d\n%s", phase, rc, out)
		}
		if treeHash(t, root) != before || len(backups(t, root)) != 0 {
			t.Errorf("%s: refused apply changed the tree", phase)
		}
	}
}

func TestMigrateDifferingTextStopsBeforeChanging(t *testing.T) {
	root := migTree(t)
	p := filepath.Join(root, "briefs", "gm-2026-10-29-005", "brief.md")
	b, _ := os.ReadFile(p)
	mustWrite(t, p, string(b)+"\nAn extra line.\n")
	before := treeHash(t, root)
	rc, out := runMigrate(t, root, "--apply")
	if rc == 0 || !strings.Contains(out, "family csvstat: the brief text differs") ||
		!strings.Contains(out, filepath.Join(root, "briefs", "gm-2026-10-29-005")) ||
		!strings.Contains(out, filepath.Join(root, "briefs", "gm-2026-10-29-001")) {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before || len(backups(t, root)) != 0 {
		t.Error("refused apply changed the tree")
	}
}

func TestMigrateInterruptedThenRerun(t *testing.T) {
	root := migTree(t)
	migrateAfterRename = func(n int) error {
		if n == 2 {
			return errors.New("injected failure")
		}
		return nil
	}
	rc, out := runMigrate(t, root, "--apply")
	migrateAfterRename = nil
	if rc == 0 || !strings.Contains(out, "injected failure") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	rc, out = runMigrate(t, root, "--apply")
	if rc != 0 || !strings.Contains(out, "resuming") {
		t.Fatalf("rerun rc %d\n%s", rc, out)
	}
	checkMigrated(t, root)
	if len(backups(t, root)) != 1 {
		t.Errorf("backups %v", backups(t, root))
	}
}

func TestMigrateInterruptedMidRunResolvesEarlierAliases(t *testing.T) {
	root := migTree(t)
	// Fail on the first rename of the third run: two runs are finished.
	migrateAfterRename = func(n int) error {
		if n == 9 {
			return errors.New("injected failure")
		}
		return nil
	}
	rc, _ := runMigrate(t, root, "--apply")
	migrateAfterRename = nil
	if rc == 0 {
		t.Fatal("no failure")
	}
	a, err := library.LoadAliases(filepath.Join(root, "briefs", "library"))
	if err != nil || len(a) != 2 || a.Resolve("gm-2026-10-28-001") != migExpected["gm-2026-10-28-001"] {
		t.Errorf("partial aliases %v %v", a, err)
	}
}

func TestMigrateRollbackRestores(t *testing.T) {
	root := migTree(t)
	before := treeHash(t, root)
	if rc, out := runMigrate(t, root, "--apply"); rc != 0 {
		t.Fatalf("apply rc %d\n%s", rc, out)
	}
	if treeHash(t, root) == before {
		t.Fatal("apply changed nothing")
	}
	rc, out := runMigrate(t, root, "--rollback")
	if rc != 0 {
		t.Fatalf("rollback rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before {
		t.Errorf("tree differs after rollback\n%s", out)
	}
	if len(backups(t, root)) != 1 {
		t.Error("rollback removed the backup")
	}

	// An interrupted migration rolls back too.
	root = migTree(t)
	before = treeHash(t, root)
	migrateAfterRename = func(n int) error {
		if n == 6 {
			return errors.New("injected failure")
		}
		return nil
	}
	rc, _ = runMigrate(t, root, "--apply")
	migrateAfterRename = nil
	if rc == 0 {
		t.Fatal("no failure")
	}
	if rc, out := runMigrate(t, root, "--rollback"); rc != 0 {
		t.Fatalf("rollback rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before {
		t.Error("tree differs after rolling back an interrupted migration")
	}
	// After a rollback the migration can run again.
	time.Sleep(1100 * time.Millisecond) // a new backup stamp
	if rc, out := runMigrate(t, root, "--apply"); rc != 0 {
		t.Fatalf("apply after rollback rc %d\n%s", rc, out)
	}
	checkMigrated(t, root)
}

func TestMigrateSkipsUnrelatedRunRecord(t *testing.T) {
	root := migTree(t)
	rec := filepath.Join(root, "runs", migUnrelated+".json")
	orig, _ := os.ReadFile(rec)
	rc, out := runMigrate(t, root)
	if rc != 0 || !strings.Contains(out, "skipped "+rec+" (no briefs folder)") {
		t.Fatalf("dry run rc %d\n%s", rc, out)
	}
	rc, out = runMigrate(t, root, "--apply")
	if rc != 0 || !strings.Contains(out, "skipped "+rec) {
		t.Fatalf("apply rc %d\n%s", rc, out)
	}
	if b, _ := os.ReadFile(rec); !bytes.Equal(b, orig) {
		t.Error("unrelated record changed")
	}
}

func TestMigrateIdempotentSecondApplyRefuses(t *testing.T) {
	root := migTree(t)
	if rc, out := runMigrate(t, root, "--apply"); rc != 0 {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	after := treeHash(t, root)
	rc, out := runMigrate(t, root, "--apply")
	if rc == 0 || !strings.Contains(out, "already migrated") {
		t.Fatalf("second apply rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != after || len(backups(t, root)) != 1 {
		t.Error("second apply changed the tree")
	}
	// With the journal gone (kept by hand elsewhere), the library still says so.
	if err := os.Remove(filepath.Join(root, "briefs", "library", migrateJournalName)); err != nil {
		t.Fatal(err)
	}
	if rc, out := runMigrate(t, root, "--apply"); rc == 0 || !strings.Contains(out, "already migrated") {
		t.Fatalf("apply without journal rc %d\n%s", rc, out)
	}
}

func TestMigrateRefusesExistingBackup(t *testing.T) {
	root := migTree(t)
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	old := migrateNow
	migrateNow = func() time.Time { return fixed }
	defer func() { migrateNow = old }()
	if err := os.Mkdir(filepath.Join(root, migrateBackupStem+"20261101T120000Z"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, root)
	rc, out := runMigrate(t, root, "--apply")
	if rc == 0 || !strings.Contains(out, "already exists") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before {
		t.Error("tree changed")
	}
}

func TestMigrateRefusesUncopyableFile(t *testing.T) {
	root := migTree(t)
	p := filepath.Join(root, "repos", "gm-2026-10-29-001", ".gophermind", "gm-2026-10-29-001-scratch", "noaccess")
	mustWrite(t, p, "secret")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o644)
	if f, err := os.Open(p); err == nil {
		f.Close()
		t.Skip("running as a user that reads mode-000 files")
	}
	before := treeHash(t, root)
	rc, out := runMigrate(t, root)
	if rc == 0 || !strings.Contains(out, "the backup cannot copy "+p) {
		t.Fatalf("dry run rc %d\n%s", rc, out)
	}
	rc, out = runMigrate(t, root, "--apply")
	if rc == 0 || !strings.Contains(out, "the backup cannot copy "+p) {
		t.Fatalf("apply rc %d\n%s", rc, out)
	}
	if treeHash(t, root) != before || len(backups(t, root)) != 0 {
		t.Error("refused apply changed the tree")
	}
}
