package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/executor"
	"gophermind/gophermind-lib/build/gitland"
)

// RepoReset says what a planner-stage resume does to the target repository.
type RepoReset int

const (
	// RepoKeep leaves the repository alone (a node: resume, or a run whose executor never started).
	RepoKeep RepoReset = iota
	// RepoOwnedClone hard-resets a clone the service owns to the base branch.
	RepoOwnedClone
	// RepoRestoreFiles deletes the run's own work branch and puts the plan's files back (CLI --reset-repo).
	RepoRestoreFiles
)

// RepoRequest is the repository part of a stage resume.
type RepoRequest struct {
	Repo       string
	Mode       RepoReset
	OwnedUnder string // RepoOwnedClone only: the service's repos directory
}

// RepoState is what the executor recorded, read before the resume moves it.
type RepoState struct {
	Branch  string
	Base    string
	Started bool
}

const (
	msgExecutorState = "resume: executor state exists; pass --reset-repo to discard the run's branch and files, or resume from node:<id>"
	msgTestFiles     = "resume: test files from this run exist in the repo; pass --reset-repo to remove them"
)

// ReadExecutorState reads the work branch and the base branch of a run.
// started is true when _state/executor.json records a start or a branch. A
// missing or empty file is a run the executor never touched. The base is the
// brief's base_branch, "main" when unset, and empty when brief.md is missing
// or does not parse.
func ReadExecutorState(runDir string) (branch, base string, started bool, err error) {
	var st executor.State
	if err := readJSONFile(runDir, executorStatePath, &st); err != nil {
		return "", "", false, err
	}
	branch, started = st.Branch, st.StartedAt != "" || st.Branch != ""
	if raw, e := os.ReadFile(filepath.Join(runDir, "brief.md")); e == nil {
		if b, e := brief.Parse(raw); e == nil {
			base = b.Front.BaseBranch
			if base == "" {
				base = "main"
			}
		}
	}
	return branch, base, started, nil
}

// ResetRepo applies mode to repo and returns what it did. RepoKeep does nothing.
func ResetRepo(runDir, repo string, mode RepoReset, ownedUnder string, st RepoState) ([]string, error) {
	switch mode {
	case RepoKeep:
		return nil, nil
	case RepoOwnedClone:
		return resetOwnedClone(runDir, repo, ownedUnder, st)
	case RepoRestoreFiles:
		return restoreFiles(runDir, repo, st)
	}
	return nil, fmt.Errorf("resume: unknown repository mode %d", mode)
}

func resetOwnedClone(runDir, repo, ownedUnder string, st RepoState) ([]string, error) {
	real, err := ownedPath(repo, ownedUnder)
	if err != nil {
		return nil, err
	}
	if err := openRepo(runDir, real, st.Base); err != nil {
		return nil, err
	}
	gd, err := git(real, "rev-parse", "--absolute-git-dir")
	if err == nil {
		gd, err = filepath.EvalSymlinks(gd)
	}
	if rel, rerr := filepath.Rel(real, gd); err != nil || rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("resume: refusing to reset %s: its git directory is outside the clone", repo)
	}
	if _, err := git(real, "reset", "--hard", st.Base); err != nil {
		return nil, err
	}
	actions := []string{"git -C " + real + " reset --hard " + st.Base}
	// -e keeps the run folder when it lives inside the clone (.gophermind/<id>).
	if _, err := git(real, "clean", "-fd", "-e", ".gophermind"); err != nil {
		return actions, err
	}
	return append(actions, "git -C "+real+" clean -fd -e .gophermind"), nil
}

// openRepo checks that real is a repository root, that base is a usable local
// branch and that the run has not landed on it.
func openRepo(runDir, real, base string) error {
	cli, err := gitland.NewCLI(real, filepath.Base(runDir))
	if errors.Is(err, gitland.ErrNotRepoRoot) {
		return fmt.Errorf("resume: %s is not a repository root", real)
	}
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	defer cli.Close()
	if base == "" {
		return errors.New("resume: the base branch is unknown (brief.md); not resetting the repository")
	}
	if !gitland.ValidBranchName(base) {
		return fmt.Errorf("resume: %q is not a usable base branch", base)
	}
	if _, err := git(real, "rev-parse", "--verify", "--quiet", "refs/heads/"+base); err != nil {
		return fmt.Errorf("resume: base branch %s does not exist in %s", base, real)
	}
	n, err := cli.RunCommitsOn(base)
	if err != nil {
		return fmt.Errorf("resume: the commits of %s could not be read: %w", base, err)
	}
	if n > 0 {
		return fmt.Errorf("resume: run %s landed on %s; a resume cannot undo landed commits", filepath.Base(runDir), base)
	}
	return nil
}

// ownedPath returns repo, absolute and symlink-resolved, when it lies strictly inside ownedUnder.
func ownedPath(repo, ownedUnder string) (string, error) {
	refuse := fmt.Errorf("resume: refusing to reset %s: not under %s", repo, ownedUnder)
	if repo == "" || ownedUnder == "" || !filepath.IsAbs(repo) || !filepath.IsAbs(ownedUnder) {
		return "", refuse
	}
	r, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", refuse
	}
	o, err := filepath.EvalSymlinks(ownedUnder)
	if err != nil {
		return "", refuse
	}
	rel, err := filepath.Rel(o, r)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", refuse
	}
	return r, nil
}

// fileOp is one plan file: restore it from base, or remove it.
type fileOp struct {
	file, abs string
	restore   bool
}

func restoreFiles(runDir, repo string, st RepoState) ([]string, error) {
	real, err := realRepo(repo)
	if err != nil {
		return nil, err
	}
	if err := openRepo(runDir, real, st.Base); err != nil {
		return nil, err
	}
	_, files, err := planFiles(runDir, real, true)
	if err != nil {
		return nil, err
	}
	// Decide and check everything before anything changes.
	cur, _ := git(real, "symbolic-ref", "--short", "-q", "HEAD")
	onWork := st.Branch != "" && cur == st.Branch
	var ops []fileOp
	for _, f := range files {
		abs, err := repoPath(real, f)
		if err != nil {
			return nil, err
		}
		fi, lerr := os.Lstat(abs)
		if lerr == nil && fi.IsDir() {
			return nil, fmt.Errorf("resume: plan path %s is a folder; not touching it", f)
		}
		if !onWork && lerr == nil {
			// Off the run's branch a change to a listed path is a person's, not the run's leftover.
			_, tracked := git(real, "ls-files", "--error-unmatch", "--", f)
			_, atBase := git(real, "cat-file", "-e", st.Base+":"+f)
			_, same := git(real, "diff", "--quiet", "HEAD", "--", f)
			if (tracked == nil && same != nil) || (tracked != nil && atBase == nil) {
				return nil, fmt.Errorf("resume: %s has uncommitted changes; commit or stash them, then pass --reset-repo again", f)
			}
		}
		kind, kerr := git(real, "cat-file", "-t", st.Base+":"+f)
		switch {
		case kerr == nil && kind == "blob":
			ops = append(ops, fileOp{f, abs, true})
		case kerr == nil:
			return nil, fmt.Errorf("resume: plan path %s is a %s at %s, not a file; not touching it", f, kind, st.Base)
		case lerr == nil:
			ops = append(ops, fileOp{f, abs, false})
		}
	}
	dropBranch := false
	if st.Branch != "" && st.Branch != st.Base {
		if !gitland.ValidBranchName(st.Branch) {
			return nil, fmt.Errorf("resume: %q is not a usable work branch name", st.Branch)
		}
		if _, err := git(real, "rev-parse", "--verify", "--quiet", "refs/heads/"+st.Branch); err == nil {
			n, err := foreignOnBranch(runDir, real, st)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, fmt.Errorf("resume: branch %s holds %d commit(s) this run did not make; not deleting it", st.Branch, n)
			}
			dropBranch = true
		}
	}

	var actions []string
	for _, o := range ops {
		if o.restore {
			if _, err := git(real, "checkout", st.Base, "--", o.file); err != nil {
				return actions, err
			}
			actions = append(actions, "restored "+o.file)
			continue
		}
		if err := os.Remove(o.abs); err != nil {
			return actions, fmt.Errorf("resume: removing %s: %w", o.file, err)
		}
		actions = append(actions, "removed "+o.file)
	}
	if dropBranch {
		// Leave the work branch only when HEAD is on it; any other HEAD stays where it was.
		if onWork {
			if _, err := git(real, "checkout", st.Base); err != nil {
				return actions, fmt.Errorf("resume: the listed files were already restored; the switch to %s was refused, so nothing is lost: %w", st.Base, err)
			}
		}
		if _, err := git(real, "branch", "-D", st.Branch); err != nil {
			return actions, err
		}
		actions = append(actions, "deleted branch "+st.Branch)
	}
	return actions, nil
}

// foreignOnBranch counts the commits on the work branch that are not the run's
// own, reading both branches without switching.
func foreignOnBranch(runDir, real string, st RepoState) (int, error) {
	cli, err := gitland.NewCLI(real, filepath.Base(runDir))
	if err != nil {
		return 0, fmt.Errorf("resume: %w", err)
	}
	defer cli.Close()
	n, err := cli.ForeignCommitsBetween(st.Base, st.Branch)
	if err != nil {
		return 0, fmt.Errorf("resume: the commits of %s could not be read: %w", st.Branch, err)
	}
	return n, nil
}

// git runs git in repo with gitland's hardened arguments and a scratch
// environment, and returns its trimmed output. The gitland CLI refuses
// checkout, reset, clean and branch -D by design, so these calls cannot go
// through it.
func git(repo string, args ...string) (string, error) {
	full := append(gitland.HardenedArgs(), "-C", repo)
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.TempDir(),
		"LANG=C",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_LITERAL_PATHSPECS=1",
		"GIT_CEILING_DIRECTORIES=" + filepath.Dir(repo),
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return "", fmt.Errorf("resume: git %s: %v: %s", args[0], err, msg)
	}
	return strings.TrimSpace(out.String()), nil
}

func realRepo(repo string) (string, error) {
	if repo == "" || !filepath.IsAbs(repo) {
		return "", fmt.Errorf("resume: the repository path %q is not absolute", repo)
	}
	return filepath.EvalSymlinks(repo)
}

// repoPath joins rel under the symlink-resolved repo and refuses a result that
// leaves it, by name or through a link in an existing parent folder.
func repoPath(repo, rel string) (string, error) {
	bad := fmt.Errorf("resume: %q is not a path inside the repository", rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", bad
	}
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if back, err := filepath.Rel(repo, full); err != nil || back == "." || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", bad
	}
	dir := filepath.Dir(full)
	for {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", bad
		}
		dir = next
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", bad
	}
	if back, err := filepath.Rel(repo, real); err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", bad
	}
	return full, nil
}

type testRef struct {
	TestFile string `json:"test_file"`
}

// planFiles lists the repo files the run placed, as slash paths, sorted: tests
// is the test files only, all adds the contract type files, the stubs, the leaf
// files and go.mod and go.sum. The saved test lists are read first; the loaded
// plan adds to them. With strict, a plan that exists but cannot be loaded is an
// error, not a shorter list.
func planFiles(runDir, repo string, strict bool) (tests, all []string, err error) {
	ts, as := map[string]bool{}, map[string]bool{}
	addTest := func(p string) {
		if p != "" {
			ts[p], as[p] = true, true
		}
	}
	var list []string
	if err := readJSONFile(runDir, "_state/test_files.json", &list); err != nil {
		return nil, nil, err
	}
	for _, p := range list {
		addTest(p)
	}
	for _, rel := range []string{"_state/leaf_tests.json", "_state/acceptance_tests.json"} {
		m := map[string]testRef{}
		if err := readJSONFile(runDir, rel, &m); err != nil {
			return nil, nil, err
		}
		for _, v := range m {
			addTest(v.TestFile)
		}
	}
	var tw struct {
		Nodes   map[string]testRef `json:"nodes"`
		Pending map[string]testRef `json:"pending"`
	}
	if err := readJSONFile(runDir, "_state/testwriter.json", &tw); err != nil {
		return nil, nil, err
	}
	for _, v := range tw.Nodes {
		addTest(v.TestFile)
	}
	for _, v := range tw.Pending {
		addTest(v.TestFile)
	}
	if present(runDir, "approval.json") {
		p, e := executor.LoadPlan(runDir, repo)
		if e != nil && strict {
			return nil, nil, fmt.Errorf("resume: the approved plan could not be read: %w; not resetting", e)
		}
		if e == nil {
			for _, t := range p.Contracts.Types {
				as[t.File] = true
			}
			for _, l := range p.Leaves {
				as[l.File], as[l.StubFile] = true, true
				addTest(l.TestFile)
			}
			for _, at := range p.AcceptTests {
				addTest(at.TestFile)
			}
			as["go.mod"], as["go.sum"] = true, true
		}
	}
	delete(ts, "")
	delete(as, "")
	return keys(ts), keys(as), nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readJSONFile decodes a run file into v; a missing or empty file leaves v alone.
func readJSONFile(runDir, rel string, v any) error {
	full, err := inside(runDir, rel)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(full)
	if os.IsNotExist(err) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("resume: %s: %w", rel, err)
	}
	return nil
}

// keptTestFiles returns the plan test files that still exist in repo and are
// absent at base or differ from it: a file identical to base is not the run's.
// With no usable base every existing file counts.
func keptTestFiles(runDir, repo, base string) ([]string, error) {
	real, err := realRepo(repo)
	if err != nil {
		return nil, err
	}
	cli, err := gitland.NewCLI(real, filepath.Base(runDir))
	if errors.Is(err, gitland.ErrNotRepoRoot) {
		return nil, fmt.Errorf("resume: %s is not a repository root", real)
	}
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	cli.Close()
	tests, _, err := planFiles(runDir, real, false)
	if err != nil {
		return nil, err
	}
	haveBase := false
	if gitland.ValidBranchName(base) {
		_, e := git(real, "rev-parse", "--verify", "--quiet", "refs/heads/"+base)
		haveBase = e == nil
	}
	var out []string
	for _, f := range tests {
		abs, err := repoPath(real, f)
		if err != nil {
			return nil, err
		}
		fi, err := os.Lstat(abs)
		if err != nil {
			continue
		}
		if haveBase && fi.Mode().IsRegular() {
			if kind, e := git(real, "cat-file", "-t", base+":"+f); e == nil && kind == "blob" {
				want, e1 := git(real, "rev-parse", base+":"+f)
				got, e2 := git(real, "hash-object", "--", abs)
				if e1 == nil && e2 == nil && want == got {
					continue
				}
			}
		}
		out = append(out, f)
	}
	return out, nil
}
