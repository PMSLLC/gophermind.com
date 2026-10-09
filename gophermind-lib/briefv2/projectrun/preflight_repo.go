package projectrun

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/planner"
)

const clearFix = "clear the attempt: run the state paths with --print-state-paths and follow docs/briefv2/project-runbook.md, or pass --resume"

var safeIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (p *pre) repoArg() string {
	if p.o.Repo != "" {
		return p.o.Repo
	}
	return p.b.Front.Repo
}

func (p *pre) putOn(base string) string {
	rev := p.o.ExpectHead
	if rev == "" {
		rev = "the intended commit"
	}
	return fmt.Sprintf("put the repo on branch %s at %s", base, rev)
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// repoChecks returns repo, then repo head (with ExpectHead) and clean tree
// (not with Resume).
func (p *pre) repoChecks() []Check {
	var out []Check
	base := p.b.Front.BaseBranch
	repo, rerr := planner.ResolveRepo(p.repoArg())
	repoCheck := func() Check {
		if rerr != nil {
			return fail("repo", rerr.Error(), "point --repo or the brief repo field at an existing git work tree")
		}
		if out, err := p.env.Git(p.ctx, repo, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
			return fail("repo", repo+" is not a git work tree", p.putOn(base))
		}
		if top, err := p.env.Git(p.ctx, repo, "rev-parse", "--show-toplevel"); err != nil || !sameDir(strings.TrimSpace(top), repo) {
			return fail("repo", repo+" is not the top level of its work tree", p.putOn(base))
		}
		if base == "" {
			return fail("repo", "the brief names no base_branch", "set base_branch in the brief")
		}
		if _, err := p.env.Git(p.ctx, repo, "rev-parse", "--verify", "refs/heads/"+base+"^{commit}"); err != nil {
			return fail("repo", "base branch "+base+" does not exist in "+repo, p.putOn(base))
		}
		head, err := p.env.Git(p.ctx, repo, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return fail("repo", "HEAD is detached in "+repo, p.putOn(base))
		}
		// An interrupted run leaves the repo on its work branch; --resume
		// continues from there.
		if h := strings.TrimSpace(head); h != base && !(p.o.Resume && h == p.b.Front.WorkBranchName()) {
			return fail("repo", "HEAD is on branch "+h+", not the base branch "+base, p.putOn(base))
		}
		return pass("repo", repo+" on branch "+strings.TrimSpace(head))
	}()
	out = append(out, repoCheck)

	if p.o.ExpectHead != "" {
		c := fail("repo head", "HEAD could not be compared with the expected head", p.putOn(base))
		if rerr == nil {
			got, err1 := p.env.Git(p.ctx, repo, "rev-parse", "HEAD")
			want, err2 := p.env.Git(p.ctx, repo, "rev-parse", "--verify", p.o.ExpectHead+"^{commit}")
			got, want = strings.TrimSpace(got), strings.TrimSpace(want)
			switch {
			case err2 != nil:
				c = fail("repo head", "the expected head "+p.o.ExpectHead+" is not a commit in the repo", p.putOn(base))
			case err1 != nil:
				c = fail("repo head", "HEAD could not be read", p.putOn(base))
			case got != want:
				c = fail("repo head", fmt.Sprintf("HEAD is %s, expected %s", short(got), short(want)), p.putOn(base))
			default:
				c = pass("repo head", "HEAD is "+short(got))
			}
		}
		out = append(out, c)
	}

	if !p.o.Resume {
		c := fail("clean tree", "not checked: the repo path did not resolve", p.putOn(base))
		if rerr == nil {
			n, err := envcheck.DirtyOutsideTests(repo, "")
			switch {
			case err != nil:
				c = fail("clean tree", "git status could not be read in "+repo, "git -C "+repo+" status")
			case n > 0:
				c = fail("clean tree", fmt.Sprintf("%d changed path(s) outside .gophermind/", n), "read the dirt with git -C "+repo+" status, then commit or discard it")
			default:
				c = pass("clean tree", "")
			}
		}
		out = append(out, c)
	}
	return out
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// staleState tests only that the run folder, the scratch folder, the work
// branch and the run record exist; nothing inside the folders is read. It
// returns the leftovers it found for a fresh run, so graded can name them.
func (p *pre) staleState() (Check, []string) {
	id := p.b.Front.ID
	if !safeIDRE.MatchString(id) || strings.Contains(id, "..") {
		return fail("stale state", "the brief id is not a safe folder name", "fix id in the brief"), nil
	}
	repo, rerr := planner.ResolveRepo(p.repoArg())
	if rerr != nil {
		return notChecked("stale state", "the repo path did not resolve", clearFix), nil
	}
	runDir := filepath.Join(repo, ".gophermind", id)
	scratch := runDir + "-scratch"
	exists := func(path string) bool { _, err := os.Lstat(path); return err == nil }

	if p.o.Resume {
		if !exists(runDir) {
			return fail("stale state", "nothing to resume: no run folder at "+runDir, "run without --resume to start a new attempt"), nil
		}
		rec, err := planner.LookupRun(id)
		if err != nil {
			return fail("stale state", "nothing to resume: no run record for "+id, "run without --resume to start a new attempt"), nil
		}
		if !sameDir(rec.Repo, repo) {
			return fail("stale state", "nothing to resume: the run record names another repo", "run without --resume to start a new attempt"), nil
		}
		return pass("stale state", "run folder and run record found for "+id), nil
	}

	var left []string
	if exists(runDir) {
		left = append(left, "run folder "+runDir)
	}
	if exists(scratch) {
		left = append(left, "scratch folder "+scratch)
	}
	branch := p.b.Front.WorkBranchName()
	if out, err := p.env.Git(p.ctx, repo, "branch", "--list", branch); err == nil && strings.TrimSpace(out) != "" {
		left = append(left, "branch "+branch)
	}
	if dir, err := p.env.ConfigDir(); err == nil {
		if rec := filepath.Join(dir, "runs", id+".json"); exists(rec) {
			left = append(left, "run record "+rec)
		}
	}
	if len(left) > 0 {
		return fail("stale state", "left by an earlier attempt: "+strings.Join(left, "; "), clearFix), left
	}
	return pass("stale state", ""), nil
}

func (p *pre) landing() Check {
	if err := gitland.ValidateLanding(p.b.Front.Landing); err != nil {
		return fail("landing", err.Error(), "set the landing field of the brief to commit or diff_only")
	}
	return pass("landing", "")
}

// workBranch reports only a failure: a work_branch that starts with a dash
// would be read as a git option, and the base branch is not a work branch.
func (p *pre) workBranch() []Check {
	wb := p.b.Front.WorkBranch
	if wb == "" {
		return nil
	}
	if strings.HasPrefix(wb, "-") || wb == p.b.Front.BaseBranch {
		return []Check{fail("work branch", "work_branch must not start with - or equal the base branch", "set work_branch in the brief to a branch other than "+p.b.Front.BaseBranch)}
	}
	return nil
}

// graded: a graded attempt starts from a cleared state, at a known commit,
// with no --resume rescue.
func (p *pre) graded(leftovers []string) Check {
	fix := "pass --expect-head <rev>, do not pass --resume, and clear the earlier attempt (" + clearFix + ")"
	switch {
	case p.o.ExpectHead == "":
		return fail("graded", "--graded needs --expect-head", fix)
	case p.o.Resume:
		return fail("graded", "--graded cannot be combined with --resume", fix)
	case len(leftovers) > 0:
		return fail("graded", "leftover state: "+leftovers[0], fix)
	}
	return pass("graded", "")
}
