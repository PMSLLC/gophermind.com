package projectrun

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const clearID = "gm-2026-09-30-777"

func clearScript(t *testing.T) string {
	t.Helper()
	_, f, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(f), "..", "..", "..", "scripts", "clear-project-state.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func clearGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// clearRepo builds a repo on main with a goal-baseline tag, then a second
// commit, an untracked file, a work branch, a run folder, another run folder
// and a run record.
func clearRepo(t *testing.T, workBranch string) (repo, cfg string) {
	t.Helper()
	skipIfNoGit(t)
	repo = t.TempDir()
	cfg = t.TempDir()
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	clearGit(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	clearGit(t, repo, "add", "a.txt")
	clearGit(t, repo, "commit", "-q", "-m", "one")
	clearGit(t, repo, "tag", "goal-baseline")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644)
	clearGit(t, repo, "add", "b.txt")
	clearGit(t, repo, "commit", "-q", "-m", "two")
	clearGit(t, repo, "branch", workBranch)
	clearGit(t, repo, "symbolic-ref", "HEAD", "refs/heads/"+workBranch)
	os.WriteFile(filepath.Join(repo, "junk.txt"), []byte("junk"), 0o644)
	run := filepath.Join(repo, ".gophermind", clearID)
	os.MkdirAll(filepath.Join(run, "_state"), 0o755)
	os.WriteFile(filepath.Join(run, "_state", "calls.jsonl"), []byte("{}\n"), 0o644)
	other := filepath.Join(repo, ".gophermind", "gm-2026-01-01-001")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "report.json"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(repo, ".gophermind", clearID+"-scratch"), 0o755)
	os.MkdirAll(filepath.Join(cfg, "runs"), 0o755)
	os.WriteFile(filepath.Join(cfg, "runs", clearID+".json"), []byte("{}"), 0o644)
	return repo, cfg
}

type clearRun struct {
	script string // path of the script to run
	cwd    string
	cfg    string
	home   string
	env    []string // extra NAME=value
	args   []string
}

// do runs the script under a git stub that logs every invocation and returns
// stderr, the exit error and the lines the stub logged.
func (c clearRun) do(t *testing.T) (out string, err error, gitCalls []string) {
	t.Helper()
	stub := t.TempDir()
	logf := filepath.Join(t.TempDir(), "git.log")
	// the stub must exec a real git binary: PATH may hold a wrapper that looks
	// up "git" on PATH again and would find the stub (a loop)
	real := ""
	for _, cand := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"} {
		if fi, serr := os.Stat(cand); serr == nil && !fi.IsDir() {
			real = cand
			break
		}
	}
	if real == "" {
		t.Skip("no real git binary found")
	}
	body := "#!/bin/sh\necho \"$*\" >> \"$GM_GIT_LOG\"\nexec \"$GM_REAL_GIT\" \"$@\"\n"
	if werr := os.WriteFile(filepath.Join(stub, "git"), []byte(body), 0o755); werr != nil {
		t.Fatal(werr)
	}
	script := c.script
	if script == "" {
		script = clearScript(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, c.args...)...)
	cmd.Dir = c.cwd
	cmd.Env = append([]string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + c.home,
		"GOPHERMIND_CONFIG_DIR=" + c.cfg, "GM_GIT_LOG=" + logf, "GM_REAL_GIT=" + real}, c.env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_, err = cmd.Output()
	b := stderr.Bytes()
	if raw, rerr := os.ReadFile(logf); rerr == nil {
		gitCalls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return string(b), err, gitCalls
}

func TestClearScriptClears(t *testing.T) {
	for _, branch := range []string{"gm/" + clearID, "feature/venture"} {
		t.Run(branch, func(t *testing.T) {
			repo, cfg := clearRepo(t, branch)
			arch := t.TempDir()
			out, err, calls := clearRun{cwd: repo, cfg: cfg, home: t.TempDir(), args: []string{repo, clearID, branch, arch}}.do(t)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if len(calls) == 0 {
				t.Fatal("the happy path ran no git")
			}
			if head := strings.TrimSpace(clearGit(t, repo, "rev-parse", "HEAD")); head != strings.TrimSpace(clearGit(t, repo, "rev-parse", "goal-baseline^{commit}")) {
				t.Errorf("HEAD %s is not goal-baseline", head)
			}
			if got := strings.TrimSpace(clearGit(t, repo, "branch", "--list", branch)); got != "" {
				t.Errorf("work branch survives: %q", got)
			}
			if got := strings.TrimSpace(clearGit(t, repo, "symbolic-ref", "--short", "HEAD")); got != "main" {
				t.Errorf("HEAD on %q", got)
			}
			for _, p := range []string{filepath.Join(repo, ".gophermind", clearID), filepath.Join(repo, ".gophermind", clearID+"-scratch"), filepath.Join(repo, "junk.txt"), filepath.Join(repo, "b.txt"), filepath.Join(cfg, "runs", clearID+".json")} {
				if _, err := os.Stat(p); err == nil {
					t.Errorf("%s survives", p)
				}
			}
			bundle := filepath.Join(arch, "work-branch.bundle")
			if vout, verr := exec.Command("git", "-C", repo, "bundle", "verify", bundle).CombinedOutput(); verr != nil {
				t.Errorf("bundle does not verify: %v\n%s", verr, vout)
			}
			if tip, terr := os.ReadFile(filepath.Join(arch, "work-branch-tip.txt")); terr != nil || len(strings.TrimSpace(string(tip))) < 40 {
				t.Errorf("work-branch-tip.txt: %q, %v", tip, terr)
			}
			if rm, rerr := os.ReadFile(filepath.Join(arch, "removed-files.txt")); rerr != nil || !strings.Contains(string(rm), "junk.txt") || strings.Contains(string(rm), "junk\n") {
				t.Errorf("removed-files.txt should list names (junk.txt) and no contents: %q, %v", rm, rerr)
			}
			tgz := filepath.Join(arch, "attempt-"+clearID+".tar.gz")
			list, lerr := exec.Command("tar", "-tzf", tgz).Output()
			if lerr != nil {
				t.Fatalf("no archive: %v", lerr)
			}
			for _, want := range []string{clearID + "/_state/calls.jsonl", "gm-2026-01-01-001/report.json"} {
				if !strings.Contains(string(list), want) {
					t.Errorf("archive lacks %s (the whole .gophermind folder is archived):\n%s", want, list)
				}
			}
		})
	}
}

type refusal struct {
	name string
	tag  string // the guard: the line of the script carrying "# guard:<tag>"
	msg  string // the exact stderr text
	// build returns the repo, the HOME for the run, the arguments and extra env
	build func(t *testing.T, repo, arch string) (home string, args []string, env []string)
}

// soleGuard: no other guard covers these cases, so without its guard the run
// proceeds (exit 0) instead of being refused by a neighbour.
func (r refusal) soleGuard() bool { return r.tag == "home" }

// readOnlyGit: the script verifies BASELINE and BASE_BRANCH with read-only git
// calls before these refusals; no destructive git call may precede any refusal.
func (r refusal) readOnlyGit() bool { return r.tag == "baseline-exists" || r.tag == "base-exists" }

var destructiveGit = []string{"symbolic-ref", "branch -D", "reset", "clean -f", "clean -d", "bundle create", "checkout", "tag -d"}

func destructiveCalls(calls []string) []string {
	var bad []string
	for _, c := range calls {
		for _, d := range destructiveGit {
			if strings.Contains(c, d) && !strings.Contains(c, "clean -ndx") {
				bad = append(bad, c)
			}
		}
	}
	return bad
}

// repoState is the observable git state of a directory: HEAD, branches and the
// commit HEAD names. A directory that is not a repo yields "".
func repoState(dir string) string {
	var b strings.Builder
	for _, a := range [][]string{{"symbolic-ref", "-q", "HEAD"}, {"rev-parse", "-q", "--verify", "HEAD"}, {"branch", "--list"}, {"tag", "--list"}} {
		out, _ := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput()
		b.Write(out)
	}
	return b.String()
}

func refusals() []refusal {
	br := "gm/" + clearID
	std := func(mod func(a []string)) func(t *testing.T, repo, arch string) (string, []string, []string) {
		return func(t *testing.T, repo, arch string) (string, []string, []string) {
			a := []string{repo, clearID, br, arch}
			mod(a)
			return t.TempDir(), a, nil
		}
	}
	// homeIsRepo runs with HOME set to the repo itself, the dangerous case.
	homeIsRepo := func(spell func(t *testing.T, repo string) string) func(t *testing.T, repo, arch string) (string, []string, []string) {
		return func(t *testing.T, repo, arch string) (string, []string, []string) {
			return repo, []string{spell(t, repo), clearID, br, arch}, nil
		}
	}
	return []refusal{
		{"empty repo", "empty-repo", "repo is empty", std(func(a []string) { a[0] = "" })},
		{"relative repo", "abs", "repo must be an absolute path", std(func(a []string) { a[0] = "." })},
		{"dotdot", "dotdot", "repo must not contain a .. component", std(func(a []string) { a[0] = a[0] + "/sub/.." })},
		{"root", "root", "repo must not be /", std(func(a []string) { a[0] = "/" })},
		{"double slash", "root", "repo must not be /", std(func(a []string) { a[0] = "//" })},
		{"slash dot", "root", "repo must not be /", std(func(a []string) { a[0] = "/." })},
		{"home exact", "home", "repo must not be $HOME", homeIsRepo(func(t *testing.T, r string) string { return r })},
		{"home trailing slash", "home", "repo must not be $HOME", homeIsRepo(func(t *testing.T, r string) string { return r + "/" })},
		{"home double slash", "home", "repo must not be $HOME", homeIsRepo(func(t *testing.T, r string) string { return r + "//" })},
		{"home slash dot", "home", "repo must not be $HOME", homeIsRepo(func(t *testing.T, r string) string { return r + "/." })},
		{"home symlink", "home", "repo must not be $HOME", homeIsRepo(func(t *testing.T, r string) string {
			l := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(r, l); err != nil {
				t.Fatal(err)
			}
			return l
		})},
		{"ancestor of home", "ancestor", "repo must not contain $HOME", func(t *testing.T, repo, arch string) (string, []string, []string) {
			sub := filepath.Join(repo, "sub")
			os.MkdirAll(sub, 0o755)
			return sub, []string{repo, clearID, br, arch}, nil
		}},
		{"not a directory", "canon", "repo does not resolve to a directory", std(func(a []string) { a[0] = a[0] + "/nope" })},
		{"no .git", "nogit", "repo has no .git directory", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{t.TempDir(), clearID, br, arch}, nil
		}},
		{"bad id", "id", "id is not gm-YYYY-MM-DD-NNN", std(func(a []string) { a[1] = "gm-1" })},
		{"empty id", "id", "id is not gm-YYYY-MM-DD-NNN", std(func(a []string) { a[1] = "" })},
		{"empty branch", "branch-empty", "work branch is empty", std(func(a []string) { a[2] = "" })},
		{"dash branch", "branch-form", "work branch is not a valid branch name", std(func(a []string) { a[2] = "-D" })},
		{"dotdot branch", "branch-form", "work branch is not a valid branch name", std(func(a []string) { a[2] = "a..b" })},
		{"glob branch", "branch-form", "work branch is not a valid branch name", std(func(a []string) { a[2] = "a*b" })},
		{"base branch", "branch-base", "work branch equals the base branch", std(func(a []string) { a[2] = "main" })},
		{"dash BASE_BRANCH", "base-form", "BASE_BRANCH is not a valid branch name", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{repo, clearID, br, arch}, []string{"BASE_BRANCH=-x"}
		}},
		{"dash BASELINE", "baseline-form", "BASELINE is not a valid revision name", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{repo, clearID, br, arch}, []string{"BASELINE=-x"}
		}},
		{"no baseline tag", "baseline-exists", "BASELINE does not name a commit", func(t *testing.T, repo, arch string) (string, []string, []string) {
			clearGit(t, repo, "tag", "-d", "goal-baseline")
			return t.TempDir(), []string{repo, clearID, br, arch}, nil
		}},
		{"BASELINE nope", "baseline-exists", "BASELINE does not name a commit", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{repo, clearID, br, arch}, []string{"BASELINE=nope"}
		}},
		{"repo with no commits", "baseline-exists", "BASELINE does not name a commit", func(t *testing.T, repo, arch string) (string, []string, []string) {
			empty := t.TempDir()
			clearGit(t, empty, "init", "-q", "-b", "main")
			return t.TempDir(), []string{empty, clearID, br, arch}, nil
		}},
		{"detached HEAD without tag", "baseline-exists", "BASELINE does not name a commit", func(t *testing.T, repo, arch string) (string, []string, []string) {
			clearGit(t, repo, "tag", "-d", "goal-baseline")
			clearGit(t, repo, "checkout", "-q", "--detach")
			return t.TempDir(), []string{repo, clearID, br, arch}, nil
		}},
		{"BASE_BRANCH missing", "base-exists", "BASE_BRANCH does not name a branch", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{repo, clearID, br, arch}, []string{"BASE_BRANCH=nope"}
		}},
		{"gophermind is a symlink", "gm-symlink", "repo .gophermind must not be a symlink", func(t *testing.T, repo, arch string) (string, []string, []string) {
			out := t.TempDir()
			os.MkdirAll(filepath.Join(out, clearID), 0o755)
			os.RemoveAll(filepath.Join(repo, ".gophermind"))
			if err := os.Symlink(out, filepath.Join(repo, ".gophermind")); err != nil {
				t.Fatal(err)
			}
			return t.TempDir(), []string{repo, clearID, br, arch}, nil
		}},
		{"relative archive", "archive-abs", "archive dir must be an absolute path", std(func(a []string) { a[3] = "x" })},
		{"missing archive", "archive-exists", "archive dir does not exist", std(func(a []string) { a[3] = a[3] + "/nope" })},
		{"archive inside repo", "archive-inside", "archive dir must be outside the repo", std(func(a []string) {
			os.MkdirAll(a[0]+"/arch", 0o755)
			a[3] = a[0] + "/arch"
		})},
		{"too few args", "args", "usage: clear-project-state.sh <repo> <id> <work-branch> <archive-dir>", func(t *testing.T, repo, arch string) (string, []string, []string) {
			return t.TempDir(), []string{repo}, nil
		}},
	}
}

// runRefusal builds a fresh repo, runs the script (or a mutated copy) and
// returns the output, the error, the git calls, and the repo for inspection.
func runRefusal(t *testing.T, r refusal, script string) (out string, err error, calls []string, repo, cfg string) {
	t.Helper()
	repo, cfg = clearRepo(t, "gm/"+clearID)
	arch := t.TempDir()
	home, args, env := r.build(t, repo, arch)
	out, err, calls = clearRun{script: script, cwd: repo, cfg: cfg, home: home, env: env, args: args}.do(t)
	return
}

func TestClearScriptRefusesAndRunsNoGit(t *testing.T) {
	for _, r := range refusals() {
		t.Run(r.name, func(t *testing.T) {
			repo, cfg := clearRepo(t, "gm/"+clearID)
			arch := t.TempDir()
			home, args, env := r.build(t, repo, arch)
			before := repoState(repo)
			var beforeTarget string
			if len(args) > 0 {
				beforeTarget = repoState(args[0])
			}
			out, err, calls := clearRun{cwd: repo, cfg: cfg, home: home, env: env, args: args}.do(t)
			if err == nil {
				t.Fatalf("not refused:\n%s", out)
			}
			if got, want := strings.TrimRight(out, "\n"), "clear-project-state: "+r.msg; got != want {
				t.Fatalf("stderr = %q, want exactly %q", got, want)
			}
			if r.readOnlyGit() {
				if bad := destructiveCalls(calls); len(bad) != 0 {
					t.Fatalf("destructive git ran before the refusal: %v", bad)
				}
			} else if len(calls) != 0 {
				t.Fatalf("git ran before the refusal: %v", calls)
			}
			if repoState(repo) != before || (len(args) > 0 && repoState(args[0]) != beforeTarget) {
				t.Fatal("git state changed (HEAD, branches or tags)")
			}
			if _, err := os.Stat(filepath.Join(repo, "junk.txt")); err != nil {
				t.Fatal("repo was cleaned")
			}
			if _, err := os.Stat(filepath.Join(cfg, "runs", clearID+".json")); err != nil {
				t.Fatal("run record deleted")
			}
			if entries, _ := os.ReadDir(arch); len(entries) != 0 && r.readOnlyGit() {
				t.Fatalf("archive written before the refusal: %v", entries)
			}
		})
	}
}

// Removing a guard from a copy of the script must make its case fail: the
// refusal message disappears.
func TestClearScriptEveryGuardIsLoadBearing(t *testing.T) {
	src, err := os.ReadFile(clearScript(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refusals() {
		t.Run(r.name, func(t *testing.T) {
			var kept []string
			removed := 0
			for _, l := range strings.Split(string(src), "\n") {
				if strings.Contains(l, "# guard:"+r.tag) {
					removed++
					continue
				}
				kept = append(kept, l)
			}
			if removed == 0 {
				t.Fatalf("no line is tagged guard:%s", r.tag)
			}
			mut := filepath.Join(t.TempDir(), "mutant.sh")
			if err := os.WriteFile(mut, []byte(strings.Join(kept, "\n")), 0o755); err != nil {
				t.Fatal(err)
			}
			if o, serr := exec.Command("bash", "-n", mut).CombinedOutput(); serr != nil {
				t.Fatalf("the mutant does not parse, so the case would pass vacuously: %s", o)
			}
			out, merr, _, _, _ := runRefusal(t, r, mut)
			if strings.TrimRight(out, "\n") == "clear-project-state: "+r.msg {
				t.Fatalf("the refusal survives without its guard %s:\n%s", r.tag, out)
			}
			if r.soleGuard() && merr != nil {
				t.Fatalf("guard %s is masked: without it the run is still refused:\n%s", r.tag, out)
			}
		})
	}
}

// A tracked .gophermind symlink is restored by reset --hard; the script must
// notice before it deletes through the link.
func lateSymlinkRepo(t *testing.T) (repo, cfg, outside string) {
	t.Helper()
	skipIfNoGit(t)
	repo, cfg, outside = t.TempDir(), t.TempDir(), t.TempDir()
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	os.MkdirAll(filepath.Join(outside, clearID), 0o755)
	os.WriteFile(filepath.Join(outside, clearID, "keep.txt"), []byte("keep"), 0o644)
	clearGit(t, repo, "init", "-q", "-b", "main")
	if err := os.Symlink(outside, filepath.Join(repo, ".gophermind")); err != nil {
		t.Fatal(err)
	}
	clearGit(t, repo, "add", ".gophermind")
	clearGit(t, repo, "commit", "-q", "-m", "one")
	clearGit(t, repo, "tag", "goal-baseline")
	clearGit(t, repo, "rm", "-q", ".gophermind")
	clearGit(t, repo, "commit", "-q", "-m", "two")
	os.MkdirAll(filepath.Join(repo, ".gophermind", clearID), 0o755)
	clearGit(t, repo, "branch", "gm/"+clearID)
	return repo, cfg, outside
}

func TestClearScriptRefusesSymlinkRestoredByReset(t *testing.T) {
	repo, cfg, outside := lateSymlinkRepo(t)
	out, err, _ := clearRun{cwd: repo, cfg: cfg, home: t.TempDir(), args: []string{repo, clearID, "gm/" + clearID, t.TempDir()}}.do(t)
	if err == nil || !strings.Contains(out, "clear-project-state: repo .gophermind became a symlink") {
		t.Fatalf("want the late symlink refusal, got err=%v\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(outside, clearID, "keep.txt")); serr != nil {
		t.Fatal("a file outside the repo was deleted")
	}
}

func TestClearScriptLateSymlinkGuardIsLoadBearing(t *testing.T) {
	src, err := os.ReadFile(clearScript(t))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, l := range strings.Split(string(src), "\n") {
		if !strings.Contains(l, "# guard:gm-symlink-late") && !strings.Contains(l, "# guard:rm-target") && !strings.Contains(l, "# guard:rm-parent") {
			kept = append(kept, l)
		}
	}
	mut := filepath.Join(t.TempDir(), "mutant.sh")
	if err := os.WriteFile(mut, []byte(strings.Join(kept, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	if o, serr := exec.Command("bash", "-n", mut).CombinedOutput(); serr != nil {
		t.Fatalf("the mutant does not parse: %s", o)
	}
	repo, cfg, outside := lateSymlinkRepo(t)
	clearRun{script: mut, cwd: repo, cfg: cfg, home: t.TempDir(), args: []string{repo, clearID, "gm/" + clearID, t.TempDir()}}.do(t)
	if _, serr := os.Stat(filepath.Join(outside, clearID, "keep.txt")); serr == nil {
		t.Fatal("without the late guards nothing outside the repo was deleted: the scenario does not exercise them")
	}
}
