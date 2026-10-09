package projectrun

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestPreflightRepoChecks(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig)
		fail  string // check expected to fail, empty means all pass
	}{
		{"clean on main passes", func(r *rig) {}, ""},
		{"dirty tracked file", func(r *rig) { os.WriteFile(filepath.Join(r.repo, "README.md"), []byte("changed\n"), 0o600) }, "clean tree"},
		{"untracked file", func(r *rig) { os.WriteFile(filepath.Join(r.repo, "new.txt"), []byte("n"), 0o600) }, "clean tree"},
		{"other branch checked out", func(r *rig) { gitIn(r.t, r.repo, "checkout", "-q", "-b", "other") }, "repo"},
		{"detached head", func(r *rig) { gitIn(r.t, r.repo, "checkout", "-q", "--detach") }, "repo"},
		{"missing base branch", func(r *rig) { r.b.Front.BaseBranch = "nope" }, "repo"},
		{"not a git repo", func(r *rig) { r.o.Repo = r.t.TempDir() }, "repo"},
		{"expect-head mismatch", func(r *rig) { r.o.ExpectHead = "0000000000000000000000000000000000000000" }, "repo head"},
		{"expect-head match", func(r *rig) { r.o.ExpectHead = gitOut(r.t, r.repo, "rev-parse", "HEAD") }, ""},
		{"gophermind dirt ignored", func(r *rig) {
			os.MkdirAll(filepath.Join(r.repo, ".gophermind", "x"), 0o700)
			os.WriteFile(filepath.Join(r.repo, ".gophermind", "x", "f"), []byte("d"), 0o600)
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			c.setup(r)
			res := r.run()
			if c.fail == "" {
				if f := Failed(res.Checks); len(f) != 0 {
					t.Fatalf("failed: %+v", f)
				}
				return
			}
			got := wantFail(t, res, c.fail)
			for _, ch := range Failed(res.Checks) {
				if ch.Name != c.fail && ch.Name != "clean tree" && ch.Name != "repo head" {
					t.Errorf("unexpected failure %+v", ch)
				}
			}
			if strings.Contains(got.Detail, "new.txt") {
				t.Errorf("detail names file contents or paths: %q", got.Detail)
			}
		})
	}
}

// An interrupted run leaves the repo on its work branch; --resume must be able
// to start from there (found by the end-to-end resume test).
func TestPreflightRepoOnWorkBranchPassesOnlyWithResume(t *testing.T) {
	work := "gm/" + rigRunID
	for _, tc := range []struct {
		name   string
		branch string
		resume bool
		ok     bool
	}{
		{"work branch with resume", work, true, true},
		{"work branch without resume", work, false, false},
		{"another branch with resume", "other", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			gitIn(t, r.repo, "checkout", "-q", "-b", tc.branch)
			r.o.Resume = tc.resume
			if tc.ok {
				wantOK(t, r.run(), "repo")
			} else {
				wantFail(t, r.run(), "repo")
			}
		})
	}
}

func TestPreflightRepoFixNamesBranchAndRev(t *testing.T) {
	r := newRig(t)
	gitIn(t, r.repo, "checkout", "-q", "-b", "other")
	r.o.ExpectHead = gitOut(t, r.repo, "rev-parse", "HEAD")
	c := wantFail(t, r.run(), "repo")
	if !strings.Contains(c.Fix, "put the repo on branch main") {
		t.Errorf("fix %q", c.Fix)
	}
}

func writeRecord(t *testing.T, cfgDir, repo, runDir string) {
	t.Helper()
	os.MkdirAll(filepath.Join(cfgDir, "runs"), 0o700)
	b, _ := json.Marshal(map[string]string{"run_id": rigRunID, "repo": repo, "run_dir": runDir})
	if err := os.WriteFile(filepath.Join(cfgDir, "runs", rigRunID+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightStaleStateAndBranch(t *testing.T) {
	runDir := func(r *rig) string { return filepath.Join(r.repo, ".gophermind", rigRunID) }
	cases := []struct {
		name  string
		setup func(r *rig)
		fail  bool
		has   string
	}{
		{"all absent", func(r *rig) {}, false, ""},
		{"run folder present", func(r *rig) {
			os.MkdirAll(runDir(r), 0o700)
			os.WriteFile(filepath.Join(runDir(r), "listed-file-xyz.json"), []byte("{}"), 0o600)
		}, true, rigRunID},
		{"scratch folder present", func(r *rig) { os.MkdirAll(runDir(r)+"-scratch", 0o700) }, true, rigRunID + "-scratch"},
		{"branch present", func(r *rig) { gitIn(r.t, r.repo, "branch", "gm/"+rigRunID) }, true, "gm/" + rigRunID},
		{"run record present", func(r *rig) { writeRecord(r.t, r.cfgDir, r.repo, runDir(r)) }, true, rigRunID + ".json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			c.setup(r)
			res := r.run()
			if !c.fail {
				wantOK(t, res, "stale state")
				return
			}
			got := wantFail(t, res, "stale state")
			if !strings.Contains(got.Detail, c.has) {
				t.Errorf("detail %q does not name %q", got.Detail, c.has)
			}
			if strings.Contains(got.Detail, "listed-file-xyz") {
				t.Errorf("detail lists files of the run folder: %q", got.Detail)
			}
			if !strings.Contains(got.Fix, "--print-state-paths") || !strings.Contains(got.Fix, "--resume") {
				t.Errorf("fix %q", got.Fix)
			}
		})
	}

	t.Run("resume with nothing fails", func(t *testing.T) {
		r := newRig(t)
		r.o.Resume = true
		got := wantFail(t, r.run(), "stale state")
		if !strings.Contains(got.Detail, "nothing to resume") {
			t.Errorf("detail %q", got.Detail)
		}
		if _, ok := find(r.run(), "clean tree"); ok {
			t.Error("clean tree must be skipped with --resume")
		}
	})
	t.Run("resume with folder but no record fails", func(t *testing.T) {
		r := newRig(t)
		r.o.Resume = true
		os.MkdirAll(runDir(r), 0o700)
		wantFail(t, r.run(), "stale state")
	})
	t.Run("resume with folder and record passes", func(t *testing.T) {
		r := newRig(t)
		r.o.Resume = true
		os.MkdirAll(runDir(r), 0o700)
		writeRecord(t, r.cfgDir, r.repo, runDir(r))
		wantOK(t, r.run(), "stale state")
	})
	t.Run("resume with a record for another repo fails", func(t *testing.T) {
		r := newRig(t)
		r.o.Resume = true
		os.MkdirAll(runDir(r), 0o700)
		writeRecord(t, r.cfgDir, t.TempDir(), runDir(r))
		wantFail(t, r.run(), "stale state")
	})
}

func TestPreflightGraded(t *testing.T) {
	head := func(r *rig) string { return gitOut(r.t, r.repo, "rev-parse", "HEAD") }
	t.Run("clean passes", func(t *testing.T) {
		r := newRig(t)
		r.o.Graded, r.o.ExpectHead = true, head(r)
		wantOK(t, r.run(), "graded")
	})
	t.Run("without expect-head fails", func(t *testing.T) {
		r := newRig(t)
		r.o.Graded = true
		wantFail(t, r.run(), "graded")
	})
	t.Run("with resume fails", func(t *testing.T) {
		r := newRig(t)
		r.o.Graded, r.o.ExpectHead, r.o.Resume = true, head(r), true
		wantFail(t, r.run(), "graded")
	})
	leftovers := map[string]func(r *rig){
		"run folder": func(r *rig) { os.MkdirAll(filepath.Join(r.repo, ".gophermind", rigRunID), 0o700) },
		"scratch":    func(r *rig) { os.MkdirAll(filepath.Join(r.repo, ".gophermind", rigRunID+"-scratch"), 0o700) },
		"branch":     func(r *rig) { gitIn(r.t, r.repo, "branch", "gm/"+rigRunID) },
		"run record": func(r *rig) { writeRecord(r.t, r.cfgDir, r.repo, filepath.Join(r.repo, ".gophermind", rigRunID)) },
	}
	for name, mk := range leftovers {
		t.Run("leftover "+name, func(t *testing.T) {
			r := newRig(t)
			r.o.Graded, r.o.ExpectHead = true, head(r)
			mk(r)
			got := wantFail(t, r.run(), "graded")
			if got.Detail == "" {
				t.Error("no detail naming the leftover")
			}
		})
	}
	t.Run("not graded has no graded check", func(t *testing.T) {
		r := newRig(t)
		if _, ok := find(r.run(), "graded"); ok {
			t.Error("graded check present without --graded")
		}
	})
}

func TestPreflightLanding(t *testing.T) {
	r := newRig(t)
	wantOK(t, r.run(), "landing")
	r.b.Front.Landing = "pull_request"
	got := wantFail(t, r.run(), "landing")
	if !strings.Contains(got.Detail, "landing") {
		t.Errorf("detail %q does not name the field", got.Detail)
	}
}

func TestPreflightWorkBranch(t *testing.T) {
	r := newRig(t)
	res := r.run()
	for _, c := range res.Checks {
		if c.Name == "work branch" {
			t.Fatal("a good work branch must not add a check line")
		}
	}
	for _, bad := range []string{"-x", "main"} {
		r.b.Front.WorkBranch = bad
		got := wantFail(t, r.run(), "work branch")
		if !strings.Contains(got.Fix, "work_branch") {
			t.Errorf("fix %q does not name the field", got.Fix)
		}
	}
	r.b.Front.WorkBranch = "feature/venture"
	for _, c := range r.run().Checks {
		if c.Name == "work branch" {
			t.Fatal("a custom work branch is allowed")
		}
	}
}
