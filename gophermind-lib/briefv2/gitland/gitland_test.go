package gitland

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var prefixTokens = []string{
	"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.sshCommand=",
	"-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", "-c", "protocol.ext.allow=never",
}

func testGitBin(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GITLAND_TEST_GIT"); p != "" {
		return p
	}
	p, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	return p
}

// gx runs the real git directly (not through the guard) with the CLI's own environment.
func gx(t *testing.T, c *CLI, args ...string) string {
	t.Helper()
	cmd := exec.Command(testGitBin(t), args...)
	cmd.Dir = c.dir
	cmd.Env = c.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func put(t *testing.T, c *CLI, rel, content string) {
	t.Helper()
	p := filepath.Join(c.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hostileGit lets the tests that plant hooks and hostile config run on machines whose git on PATH is a
// guard wrapper that refuses such repos. CI with plain git is unaffected.
func hostileGit(t *testing.T) {
	t.Helper()
	if os.Getenv("GITLAND_TEST_GIT") == "" {
		if _, err := os.Stat("/usr/bin/git"); err == nil {
			t.Setenv("GITLAND_TEST_GIT", "/usr/bin/git")
		}
	}
}

// initRepo runs git init in dir with a scratch environment.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command(testGitBin(t), "init", "-q", "-b", "main")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
}

func useTestGit(t *testing.T) {
	t.Helper()
	testGit = testGitBin(t)
	t.Cleanup(func() { testGit = "" })
}

func newRepo(t *testing.T) *CLI {
	t.Helper()
	useTestGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := NewCLI(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	put(t, c, "README.md", "readme\n")
	put(t, c, "go.mod", "module x\n")
	gx(t, c, "add", "--", "README.md", "go.mod")
	gx(t, c, "commit", "-m", "base")
	return c
}

// recording points the CLI at a script that logs argv, one line per invocation, then execs the real git.
func recording(t *testing.T, c *CLI) string {
	t.Helper()
	real := testGitBin(t)
	logPath := filepath.Join(t.TempDir(), "argv.log")
	script := filepath.Join(t.TempDir(), "git")
	body := "#!/bin/sh\nline=\"\"\nfor a in \"$@\"; do line=\"$line $(printf %s \"$a\" | tr '\\n' '~')\"; done\n" +
		"printf '%s\\n' \"${line# }\" >> '" + logPath + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	c.git = script
	return logPath
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func scenario(t *testing.T, c *CLI) {
	t.Helper()
	put(t, c, "a_test.go", "package x\n")
	put(t, c, "stub.go", "package x\n")
	put(t, c, "go.mod", "module x\n\ngo 1.22\n")
	if err := c.Start("main", "gm/x", []string{"a_test.go", "stub.go", "go.mod"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.CommitWave0([]string{"a_test.go", "stub.go", "go.mod"}); err != nil {
		t.Fatalf("Wave0: %v", err)
	}
	if err := os.Remove(filepath.Join(c.dir, "stub.go")); err != nil {
		t.Fatal(err)
	}
	put(t, c, "real.go", "package x\n")
	if _, err := c.CommitLeaf("fn-a", "Greet", []string{"real.go"}, []string{"stub.go"}); err != nil {
		t.Fatalf("Leaf: %v", err)
	}
	put(t, c, "real2.go", "package x\n")
	if _, err := c.CommitRepair("fn-a", 1, []string{"real2.go"}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	put(t, c, "README.md", "changed\n")
	put(t, c, "junk.txt", "junk\n")
	if err := c.Restore([]string{"README.md", "junk.txt"}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	put(t, c, "new.txt", "n\n")
	if _, err := c.Dirty(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diff("main"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.LeafCommit("fn-a"); err != nil || !ok {
		t.Fatalf("LeafCommit: %v %v", ok, err)
	}
	if _, err := c.Head(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Branch(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Finish("built, Acceptance passed 2 of 2"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func TestGitLandingCommits(t *testing.T) {
	hostileGit(t)
	c := newRepo(t)
	// hostile global config in the HOME the CLI does not use, and a hostile default hook in the repo
	marker := filepath.Join(t.TempDir(), "marker")
	hooks := filepath.Join(c.dir, ".git", "hooks")
	_ = os.MkdirAll(hooks, 0o755)
	hook := "#!/bin/sh\ntouch '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	evil := "[user]\n name = Evil\n email = evil@example.com\n[commit]\n gpgsign = true\n[gpg]\n program = /bin/false\n"
	if err := os.WriteFile(filepath.Join(c.home, ".gitconfig"), []byte(evil), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Start("main", "gm/x", nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "a_test.go", "package x\n")
	put(t, c, "b_test.go", "package x\n")
	put(t, c, "a.go", "package x\n")
	put(t, c, "b.go", "package x\n")
	put(t, c, "go.mod", "module x\n\ngo 1.22\n")
	w0 := []string{"a_test.go", "b_test.go", "a.go", "b.go", "go.mod"}
	if _, err := c.CommitWave0(w0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(c.dir, "a.go")); err != nil {
		t.Fatal(err)
	}
	put(t, c, "real.go", "package x\n")
	put(t, c, "unrelated.txt", "x\n")
	leaf, err := c.CommitLeaf("fn-a", "Greet", []string{"real.go"}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	subj := strings.Split(gx(t, c, "log", "--format=%s"), "\n")
	if len(subj) != 3 || subj[0] != "gm(fn-a): Greet" || !strings.HasPrefix(subj[1], "gm(wave0): ") || subj[2] != "base" {
		t.Fatalf("subjects %q", subj)
	}
	files := strings.Fields(gx(t, c, "show", "--no-renames", "--name-status", "--format=", leaf))
	sort.Strings(files)
	if strings.Join(files, " ") != "A D a.go real.go" {
		t.Fatalf("leaf files %q", files)
	}
	if got := gx(t, c, "log", "-1", "--format=%(trailers:key=GopherMind-Node,valueonly)"); got != "fn-a" {
		t.Fatalf("trailer %q", got)
	}
	for _, f := range []string{"%an <%ae>", "%cn <%ce>"} {
		if got := gx(t, c, "log", "--format="+f); strings.Contains(got, "Evil") || !strings.Contains(got, "GopherMind <gophermind@localhost>") {
			t.Fatalf("identity %q", got)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("hook ran")
	}
	if !strings.Contains(gx(t, c, "status", "--porcelain"), "unrelated.txt") {
		t.Fatal("unrelated file lost")
	}
}

func TestCommitRepairSubject(t *testing.T) {
	c := newRepo(t)
	if err := c.Start("main", "gm/x", nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "f.go", "package x\n")
	if _, err := c.CommitRepair("fn-a", 2, []string{"f.go"}); err != nil {
		t.Fatal(err)
	}
	if got := gx(t, c, "log", "-1", "--format=%s"); got != "gm(fn-a): repair round 2" {
		t.Fatal(got)
	}
}

func TestCommitLeafExactPaths(t *testing.T) {
	t.Run("unrelated staged", func(t *testing.T) {
		c := newRepo(t)
		if err := c.Start("main", "gm/x", nil); err != nil {
			t.Fatal(err)
		}
		put(t, c, "other.txt", "o\n")
		gx(t, c, "add", "--", "other.txt")
		put(t, c, "real.go", "package x\n")
		_, err := c.CommitLeaf("fn-a", "t", []string{"real.go"}, nil)
		if !errors.Is(err, ErrIndexNotClean) {
			t.Fatalf("err %v", err)
		}
		if got := gx(t, c, "diff", "--cached", "--name-only"); got != "other.txt" {
			t.Fatalf("index %q", got)
		}
		if !strings.Contains(gx(t, c, "status", "--porcelain"), "?? real.go") {
			t.Fatal("named file not unstaged")
		}
	})
	t.Run("nothing to commit", func(t *testing.T) {
		c := newRepo(t)
		if err := c.Start("main", "gm/x", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := c.CommitLeaf("fn-a", "t", []string{"README.md"}, nil); !errors.Is(err, ErrNothingToCommit) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("bad paths", func(t *testing.T) {
		c := newRepo(t)
		if err := c.Start("main", "gm/x", nil); err != nil {
			t.Fatal(err)
		}
		logPath := recording(t, c)
		for _, p := range []string{"../x", "/etc/passwd", "-rf", ".git/config", "a/../b", "", "./x"} {
			if _, err := c.CommitLeaf("fn-a", "t", []string{p}, nil); err == nil {
				t.Fatalf("accepted %q", p)
			}
			if _, err := c.CommitLeaf("fn-a", "t", nil, []string{p}); err == nil {
				t.Fatalf("accepted remove %q", p)
			}
		}
		if l := logLines(t, logPath); len(l) != 0 {
			t.Fatalf("git was called: %q", l)
		}
	})
}

func TestFinishFastForward(t *testing.T) {
	c := newRepo(t)
	if err := c.Start("main", "gm/x", nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "a_test.go", "package x\n")
	if _, err := c.CommitWave0([]string{"a_test.go"}); err != nil {
		t.Fatal(err)
	}
	put(t, c, "a.go", "package x\n")
	if _, err := c.CommitLeaf("fn-a", "A", []string{"a.go"}, nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "b.go", "package x\n")
	second, err := c.CommitLeaf("fn-b", "B", []string{"b.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	final, err := c.Finish("built, Acceptance passed 2 of 2")
	if err != nil {
		t.Fatal(err)
	}
	if gx(t, c, "rev-parse", "main") != gx(t, c, "rev-parse", "gm/x") {
		t.Fatal("main and work differ")
	}
	if final != gx(t, c, "rev-parse", "--short", "main") {
		t.Fatal("returned hash")
	}
	if gx(t, c, "log", "-1", "--format=%s", "main") != "gm(run): built, Acceptance passed 2 of 2" {
		t.Fatal("subject")
	}
	if gx(t, c, "rev-parse", "main^{tree}") != gx(t, c, "rev-parse", "main~1^{tree}") {
		t.Fatal("tree differs")
	}
	if gx(t, c, "rev-parse", "--short", "main~1") != second {
		t.Fatal("parent")
	}
	if b, _ := c.Branch(); b != "main" {
		t.Fatalf("branch %q", b)
	}
	for _, a := range strings.Split(gx(t, c, "log", "main~4..main", "--format=%an"), "\n") {
		if a != "GopherMind" {
			t.Fatalf("author %q", a)
		}
	}
}

func TestLandingBlockedWhenMainMoved(t *testing.T) {
	c := newRepo(t)
	if err := c.Start("main", "gm/x", nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "a.go", "package x\n")
	if _, err := c.CommitLeaf("fn-a", "A", []string{"a.go"}, nil); err != nil {
		t.Fatal(err)
	}
	gx(t, c, "switch", "main")
	gx(t, c, "commit", "--allow-empty", "-m", "moved")
	moved := gx(t, c, "rev-parse", "main")
	gx(t, c, "switch", "gm/x")
	_, err := c.Finish("done")
	if !errors.Is(err, ErrLandingBlocked) {
		t.Fatalf("err %v", err)
	}
	if gx(t, c, "rev-parse", "main") != moved {
		t.Fatal("main moved")
	}
	if b, _ := c.Branch(); b != "gm/x" {
		t.Fatalf("branch %q", b)
	}
	if !strings.Contains(gx(t, c, "log", "gm/x", "--format=%s"), "gm(fn-a): A") {
		t.Fatal("leaf lost")
	}
	if strings.Contains(gx(t, c, "reflog"), "reset") {
		t.Fatal("reset in reflog")
	}
}

var forbiddenSub = map[string]bool{"push": true, "reset": true, "rebase": true, "checkout": true, "stash": true, "clean": true,
	"gc": true, "prune": true, "remote": true, "fetch": true, "pull": true, "filter-branch": true, "update-ref": true, "branch": true}

func checkLog(t *testing.T, lines []string) {
	t.Helper()
	if len(lines) == 0 {
		t.Fatal("empty log")
	}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < len(prefixTokens) || strings.Join(f[:len(prefixTokens)], " ") != strings.Join(prefixTokens, " ") {
			t.Fatalf("no fixed prefix: %q", l)
		}
		rest := f[len(prefixTokens):]
		for i, a := range rest {
			if a == "--" {
				break
			}
			if a == "--force" || a == "-f" || a == "--hard" || a == "--force-with-lease" {
				t.Fatalf("forbidden flag in %q", l)
			}
			if i == 0 && forbiddenSub[a] {
				t.Fatalf("forbidden subcommand in %q", l)
			}
		}
	}
}

func TestNoPushForceResetRebase(t *testing.T) {
	c := newRepo(t)
	logPath := recording(t, c)
	scenario(t, c)
	checkLog(t, logLines(t, logPath))

	t.Run("guard", func(t *testing.T) {
		c := newRepo(t)
		logPath := recording(t, c)
		for _, a := range [][]string{{"push"}, {"reset", "--hard"}, {"checkout", "main"}, {"rebase", "main"},
			{"commit", "--amend", "--force"}, {"branch", "-D", "x"}, {"stash"}, {"clean", "-fd"}, {"remote", "add", "o", "u"},
			{"fetch"}, {"pull"}, {"gc"}, {"prune"}, {"filter-branch"}, {"update-ref", "refs/heads/main", "HEAD"},
			{"status", "--no-verify"}, {"log", "--force-with-lease"}} {
			if _, err := c.run(a...); !errors.Is(err, ErrForbiddenGitArgs) {
				t.Fatalf("%v: %v", a, err)
			}
		}
		if l := logLines(t, logPath); len(l) != 0 {
			t.Fatalf("log %q", l)
		}
	})
}

func TestGitInvocationsNeutraliseRepoConfig(t *testing.T) {
	hostileGit(t)
	c := newRepo(t)
	markers := t.TempDir()
	script := func(name string) string {
		p := filepath.Join(markers, name+".sh")
		body := "#!/bin/sh\ntouch '" + filepath.Join(markers, name+".marker") + "'\ncat\n"
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	hooks := filepath.Join(markers, "hooks")
	_ = os.MkdirAll(hooks, 0o755)
	for _, h := range []string{"pre-commit", "post-commit", "reference-transaction", "commit-msg"} {
		body := "#!/bin/sh\ntouch '" + filepath.Join(markers, h+".marker") + "'\n"
		if err := os.WriteFile(filepath.Join(hooks, h), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[core]\n\tfsmonitor = " + script("fsmonitor") + "\n\tsshCommand = " + script("ssh") + "\n\thooksPath = " + hooks + "\n" +
		"[diff]\n\texternal = " + script("diffext") + "\n[diff \"evil\"]\n\ttextconv = " + script("textconv") + "\n"
	put(t, c, ".gitattributes", "*.txt diff=evil\n")
	gx(t, c, "add", "--", ".gitattributes")
	gx(t, c, "commit", "-m", "attrs")
	f, err := os.OpenFile(filepath.Join(c.dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(cfg)
	_ = f.Close()

	logPath := recording(t, c)
	scenario(t, c)
	ents, _ := os.ReadDir(markers)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".marker") {
			t.Fatalf("marker %s", e.Name())
		}
	}
	lines := logLines(t, logPath)
	checkLog(t, lines)
	for _, l := range lines {
		if !strings.Contains(l, "core.fsmonitor=false") || !strings.Contains(l, "core.sshCommand= ") {
			t.Fatalf("prefix %q", l)
		}
	}
}

func TestLeafCommitLookup(t *testing.T) {
	c := newRepo(t)
	if err := c.Start("main", "gm/x", nil); err != nil {
		t.Fatal(err)
	}
	put(t, c, "t_test.go", "package x\n")
	if _, err := c.CommitWave0([]string{"t_test.go"}); err != nil {
		t.Fatal(err)
	}
	put(t, c, "a.go", "package x\n")
	a, err := c.CommitLeaf("fn-a", "A", []string{"a.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, c, "ab.go", "package x\n")
	ab, err := c.CommitLeaf("fn-ab", "AB", []string{"ab.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, c, "r.go", "package x\n")
	if _, err := c.CommitRepair("fn-a", 1, []string{"r.go"}); err != nil {
		t.Fatal(err)
	}
	gx(t, c, "commit", "--allow-empty", "-m", "other subject", "-m", "GopherMind-Node: fn-zz")
	gx(t, c, "commit", "--allow-empty", "-m", "unrelated", "-m", "mentions GopherMind-Node: fn-a in body")
	if h, ok, err := c.LeafCommit("fn-a"); err != nil || !ok || h != a {
		t.Fatalf("fn-a: %q %v %v want %q", h, ok, err, a)
	}
	if h, ok, err := c.LeafCommit("fn-ab"); err != nil || !ok || h != ab {
		t.Fatalf("fn-ab: %q %v %v", h, ok, err)
	}
	for _, id := range []string{"fn-zz", "nope", "fn-"} {
		if _, ok, err := c.LeafCommit(id); err != nil || ok {
			t.Fatalf("%s: %v %v", id, ok, err)
		}
	}
}

func TestStartRefusesDirtyTree(t *testing.T) {
	check := func(t *testing.T) {
		c := newRepo(t)
		put(t, c, "secret-content.txt", "TOPSECRET\n")
		err := c.Start("main", "gm/x", nil)
		if !errors.Is(err, ErrDirtyTree) {
			t.Fatalf("err %v", err)
		}
		if strings.Contains(err.Error(), "TOPSECRET") || !strings.Contains(err.Error(), "1") {
			t.Fatalf("text %q", err)
		}
		if gx(t, c, "branch", "--list", "gm/x") != "" {
			t.Fatal("branch created")
		}
		if err := c.Start("nomain", "gm/x", nil); err == nil {
			t.Fatal("missing base accepted")
		}
		// resume: work branch exists, dirty tree allowed
		gx(t, c, "branch", "gm/x")
		if err := c.Start("main", "gm/x", nil); err != nil {
			t.Fatalf("resume: %v", err)
		}
		if b, _ := c.Branch(); b != "gm/x" {
			t.Fatalf("branch %q", b)
		}
	}
	t.Run("TestFreshRunDirtyTreeRefused", check)
}

func TestStartAllowsListedDirtyPaths(t *testing.T) {
	c := newRepo(t)
	put(t, c, "a_test.go", "package x\n")
	put(t, c, "b_test.go", "package x\n")
	if err := c.Start("main", "gm/one", []string{"a_test.go"}); !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("err %v", err)
	}
	if gx(t, c, "branch", "--list", "gm/one") != "" {
		t.Fatal("branch created")
	}
	put(t, c, "README.md", "changed\n")
	if err := c.Start("main", "gm/two", []string{"a_test.go", "b_test.go"}); !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("tracked change accepted: %v", err)
	}
	gx(t, c, "restore", "--", "README.md")
	logPath := recording(t, c)
	for _, p := range []string{"../x", "/abs/path", ".git/config"} {
		if err := c.Start("main", "gm/three", []string{p}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if l := logLines(t, logPath); len(l) != 0 {
		t.Fatalf("git called: %q", l)
	}
	if err := c.Start("main", "gm/x", []string{"a_test.go", "b_test.go", "not_dirty.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitWave0([]string{"a_test.go", "b_test.go"}); err != nil {
		t.Fatal(err)
	}
	if d, err := c.Dirty(); err != nil || len(d) != 0 {
		t.Fatalf("dirty %v %v", d, err)
	}
}

func TestNoRemoteRequired(t *testing.T) {
	c := newRepo(t)
	if gx(t, c, "remote") != "" {
		t.Fatal("remote present")
	}
	logPath := recording(t, c)
	scenario(t, c)
	for _, l := range logLines(t, logPath) {
		for _, w := range strings.Fields(l)[len(prefixTokens):][:1] {
			for _, bad := range []string{"remote", "fetch", "pull", "push"} {
				if w == bad {
					t.Fatalf("%q", l)
				}
			}
		}
	}
}

func TestDiffOnlyPatch(t *testing.T) {
	c := newRepo(t)
	put(t, c, "README.md", "changed\n")
	put(t, c, "new.txt", "brand new\n")
	put(t, c, ".gophermind/run/x.json", "{}\n")
	if err := os.Remove(filepath.Join(c.dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	before := gx(t, c, "log", "--format=%H")
	patch, err := c.Diff("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"--- a/README.md", "+++ b/README.md", "new file mode", "+++ b/new.txt", "deleted file mode", "--- a/go.mod"} {
		if !bytes.Contains(patch, []byte(w)) {
			t.Fatalf("patch lacks %q", w)
		}
	}
	if bytes.Contains(patch, []byte(".gophermind")) {
		t.Fatal(".gophermind in patch")
	}
	if gx(t, c, "log", "--format=%H") != before {
		t.Fatal("history changed")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	gx(t, c, "clone", "-q", c.dir, clone)
	cmd := exec.Command(testGitBin(t), "apply", "--check")
	cmd.Dir = clone
	cmd.Env = c.env()
	cmd.Stdin = bytes.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply --check: %v %s", err, out)
	}
}

func TestRestore(t *testing.T) {
	c := newRepo(t)
	put(t, c, "README.md", "changed\n")
	gx(t, c, "add", "--", "README.md")
	put(t, c, "junk.txt", "j\n")
	outside := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(c.dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(c.dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, c, "d/f.txt", "f\n")
	if err := c.Restore([]string{"README.md", "junk.txt", "link"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(c.dir, "README.md")); string(b) != "readme\n" {
		t.Fatalf("readme %q", b)
	}
	if gx(t, c, "diff", "--cached", "--name-only") != "" {
		t.Fatal("still staged")
	}
	if _, err := os.Lstat(filepath.Join(c.dir, "junk.txt")); err == nil {
		t.Fatal("junk kept")
	}
	if _, err := os.Lstat(filepath.Join(c.dir, "link")); err == nil {
		t.Fatal("link kept")
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "keep" {
		t.Fatal("target touched")
	}
	for _, p := range []string{"d", "../x", outside} {
		if err := c.Restore([]string{p}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if _, err := os.Stat(filepath.Join(c.dir, "d", "f.txt")); err != nil {
		t.Fatal("directory content touched")
	}
}

func TestDirtyLists(t *testing.T) {
	c := newRepo(t)
	put(t, c, "old.txt", "old content here\n")
	put(t, c, "mod.txt", "m\n")
	gx(t, c, "add", "--", "old.txt", "mod.txt")
	gx(t, c, "commit", "-m", "more")
	put(t, c, "mod.txt", "m2\n")
	if err := os.Remove(filepath.Join(c.dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	gx(t, c, "mv", "old.txt", "renamed.txt")
	put(t, c, "sp ace\nnl.txt", "x\n")
	put(t, c, "sub/new.txt", "x\n")
	got, err := c.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go.mod", "mod.txt", "old.txt", "renamed.txt", "sp ace\nnl.txt", "sub/new.txt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateLanding(t *testing.T) {
	for _, m := range []string{"diff_only", "commit", ""} {
		if err := ValidateLanding(m); err != nil {
			t.Fatalf("%q: %v", m, err)
		}
	}
	t.Run("schema enum", func(t *testing.T) {
		b, err := os.ReadFile("../schema/brief-frontmatter.schema.json")
		if err != nil {
			t.Fatal(err)
		}
		var s struct {
			Properties struct {
				Landing struct {
					Enum []string `json:"enum"`
				} `json:"landing"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		if len(s.Properties.Landing.Enum) == 0 {
			t.Fatal("no landing enum")
		}
		for _, v := range s.Properties.Landing.Enum {
			err := ValidateLanding(v)
			if v == "pull_request" {
				if err == nil {
					t.Fatal("pull_request accepted")
				}
			} else if err != nil {
				t.Fatalf("%s: %v", v, err)
			}
		}
	})
	t.Run("TestPullRequestUnsupported", func(t *testing.T) {
		err := ValidateLanding("pull_request")
		want := `brief field landing: "pull_request" is not supported by this executor`
		if err == nil || err.Error() != want {
			t.Fatalf("err %v", err)
		}
		if err := ValidateLanding("bogus-value"); err == nil || strings.Contains(err.Error(), "bogus") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestBranchNamesValidated(t *testing.T) {
	c := newRepo(t)
	logPath := recording(t, c)
	for _, p := range [][2]string{{"main", "../x"}, {"-D", "w"}, {"main", "a b"}, {"main", ""}, {"", "w"}, {"main", "-x"}, {"main", "a..b"}} {
		if err := c.Start(p[0], p[1], nil); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if _, err := c.Diff("-x"); err == nil {
		t.Fatal("diff accepted flag base")
	}
	if l := logLines(t, logPath); len(l) != 0 {
		t.Fatalf("git called %q", l)
	}
}

func TestCloseGuardedDelete(t *testing.T) {
	useTestGit(t)
	d1, d2 := t.TempDir(), t.TempDir()
	initRepo(t, d1)
	initRepo(t, d2)
	c, err := NewCLI(d1, "r1")
	if err != nil {
		t.Fatal(err)
	}
	home := c.home
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		t.Fatal("no home")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); err == nil {
		t.Fatal("home kept")
	}

	c2, err := NewCLI(d2, "r2")
	if err != nil {
		t.Fatal(err)
	}
	real := c2.home
	nested := filepath.Join(t.TempDir(), "gm-gitland-home-nested")
	_ = os.MkdirAll(nested, 0o755)
	c2.home = nested
	if err := c2.Close(); err == nil {
		t.Fatal("nested path deleted")
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatal("nested removed")
	}
	other, err := os.MkdirTemp("", "other-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(other)
	c2.home = other
	if err := c2.Close(); err == nil {
		t.Fatal("unprefixed deleted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("unprefixed removed")
	}
	c2.home = real
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewCLIRequiresRepoRoot(t *testing.T) {
	useTestGit(t)
	outer := t.TempDir()
	initRepo(t, outer)
	sub := filepath.Join(outer, "sub")
	plain := filepath.Join(outer, "plain")
	for _, d := range []string{sub, plain} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, sub)
	if err := os.RemoveAll(filepath.Join(sub, ".git")); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{sub, plain} {
		c, err := NewCLI(d, "r1")
		if !errors.Is(err, ErrNotRepoRoot) || c != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if strings.Contains(err.Error(), outer) {
			t.Fatal("path in error")
		}
	}
	ok, err := NewCLI(outer, "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Close()
	if gx(t, ok, "branch", "--list", "gm/*") != "" {
		t.Fatal("branch in outer repo")
	}
}

func TestGuardMerge(t *testing.T) {
	for _, a := range [][]string{
		{"merge", "gm/x"}, {"merge", "--no-ff", "--ff-only", "gm/x"}, {"merge", "--ff-only", "--squash", "gm/x"},
		{"merge", "--ff-only", "-X", "ours", "gm/x"}, {"merge", "--ff-only", "-Xours", "gm/x"},
		{"merge", "--ff-only", "--strategy=ours", "gm/x"}, {"merge", "--ff-only", "-s", "ours", "gm/x"},
		{"merge", "--ff-only", "--strategy", "ours", "gm/x"},
	} {
		if _, err := guard(a); !errors.Is(err, ErrForbiddenGitArgs) {
			t.Fatalf("%v: %v", a, err)
		}
	}
	if sub, err := guard([]string{"merge", "--ff-only", "gm/x"}); err != nil || sub != "merge" {
		t.Fatal(err)
	}
}

func TestProductionIgnoresGitEnv(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	t.Setenv("GITLAND_TEST_GIT", "/nonexistent/git")
	testGit = ""
	c, err := NewCLI(dir, "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.git == "/nonexistent/git" {
		t.Fatal("env redirected git")
	}
}

func TestCleanTextRuneBoundary(t *testing.T) {
	in := strings.Repeat("a", 71) + "\u00e9\u00e9"
	out := cleanText(in, 72)
	if !utf8.ValidString(out) || len(out) > 72 {
		t.Fatalf("%q", out)
	}
}

func TestRestoreRefusesTrackedDirectory(t *testing.T) {
	c := newRepo(t)
	put(t, c, "d/f.txt", "f\n")
	gx(t, c, "add", "--", "d/f.txt")
	gx(t, c, "commit", "-m", "d")
	put(t, c, "d/f.txt", "changed\n")
	if err := c.Restore([]string{"d"}); err == nil {
		t.Fatal("directory accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(c.dir, "d/f.txt")); string(b) != "changed\n" {
		t.Fatal("file touched")
	}
}

func TestLeafCommitReturnsGitError(t *testing.T) {
	c := newRepo(t)
	c.git = "/bin/false"
	if _, ok, err := c.LeafCommit("fn-a"); err == nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
}

func TestEnvHasNoInheritedGit(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere")
	c := newRepo(t)
	have := map[string]bool{}
	for _, e := range c.env() {
		have[strings.SplitN(e, "=", 2)[0]] = true
	}
	for _, k := range []string{"GIT_EDITOR", "GIT_PAGER", "GIT_TERMINAL_PROMPT", "GIT_CEILING_DIRECTORIES"} {
		if !have[k] {
			t.Fatalf("missing %s", k)
		}
	}
	if strings.Contains(strings.Join(c.env(), "\n"), "/elsewhere") {
		t.Fatal("GIT_DIR inherited")
	}
}
