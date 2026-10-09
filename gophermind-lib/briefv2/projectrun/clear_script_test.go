package projectrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
// commit, an untracked file, a work branch, a run folder and a run record.
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
	os.MkdirAll(filepath.Join(repo, ".gophermind", clearID+"-scratch"), 0o755)
	os.MkdirAll(filepath.Join(cfg, "runs"), 0o755)
	os.WriteFile(filepath.Join(cfg, "runs", clearID+".json"), []byte("{}"), 0o644)
	return repo, cfg
}

func runClear(t *testing.T, cwd, cfg string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{clearScript(t)}, args...)...)
	cmd.Dir = cwd
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GOPHERMIND_CONFIG_DIR=" + cfg}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestClearScriptClears(t *testing.T) {
	for _, branch := range []string{"gm/" + clearID, "feature/venture"} {
		t.Run(branch, func(t *testing.T) {
			repo, cfg := clearRepo(t, branch)
			arch := t.TempDir()
			if out, err := runClear(t, repo, cfg, repo, clearID, branch, arch); err != nil {
				t.Fatalf("%v\n%s", err, out)
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
			if _, err := os.Stat(filepath.Join(arch, "attempt-"+clearID+".tar.gz")); err != nil {
				t.Errorf("no archive: %v", err)
			}
		})
	}
}

func TestClearScriptRefusesBadArguments(t *testing.T) {
	repo, cfg := clearRepo(t, "gm/"+clearID)
	arch := t.TempDir()
	before := treeSnap(t, repo)
	headBefore := clearGit(t, repo, "rev-parse", "HEAD")
	branches := clearGit(t, repo, "branch", "--list")
	home := t.TempDir()
	cases := map[string][]string{
		"empty repo":     {"", clearID, "gm/" + clearID, arch},
		"relative repo":  {".", clearID, "gm/" + clearID, arch},
		"root":           {"/", clearID, "gm/" + clearID, arch},
		"no .git":        {home, clearID, "gm/" + clearID, arch},
		"bad id":         {repo, "gm-1", "gm/" + clearID, arch},
		"empty id":       {repo, "", "gm/" + clearID, arch},
		"empty branch":   {repo, clearID, "", arch},
		"dash branch":    {repo, clearID, "-D", arch},
		"base branch":    {repo, clearID, "main", arch},
		"relative arch":  {repo, clearID, "gm/" + clearID, "x"},
		"missing arch":   {repo, clearID, "gm/" + clearID, filepath.Join(arch, "nope")},
		"too few args":   {repo},
		"no args at all": {},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			// cwd is the real temp repo: a script that fell back to "." would wreck it
			if out, err := runClear(t, repo, cfg, args...); err == nil {
				t.Fatalf("not refused:\n%s", out)
			}
			if clearGit(t, repo, "rev-parse", "HEAD") != headBefore || clearGit(t, repo, "branch", "--list") != branches {
				t.Fatal("git state changed")
			}
			after := treeSnap(t, repo)
			if len(after) != len(before) {
				t.Fatalf("tree changed: %d entries before, %d after", len(before), len(after))
			}
			if _, err := os.Stat(filepath.Join(cfg, "runs", clearID+".json")); err != nil {
				t.Fatal("run record deleted")
			}
		})
	}
}

func TestClearScriptRefusesHome(t *testing.T) {
	repo, cfg := clearRepo(t, "gm/"+clearID)
	cmd := exec.Command("bash", clearScript(t), repo, clearID, "gm/"+clearID, t.TempDir())
	cmd.Dir = repo
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + repo, "GOPHERMIND_CONFIG_DIR=" + cfg}
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a repo equal to $HOME was not refused:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(repo, "junk.txt")); err != nil {
		t.Fatal("repo was cleaned")
	}
}
