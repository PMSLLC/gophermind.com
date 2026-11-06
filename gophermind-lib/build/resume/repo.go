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
		return resetOwnedClone(repo, ownedUnder, st)
	case RepoRestoreFiles:
		return restoreFiles(runDir, repo, st)
	}
	return nil, fmt.Errorf("resume: unknown repository mode %d", mode)
}

func resetOwnedClone(repo, ownedUnder string, st RepoState) ([]string, error) {
	real, err := ownedPath(repo, ownedUnder)
	if err != nil {
		return nil, err
	}
	if st.Base == "" {
		return nil, errors.New("resume: the base branch is unknown (brief.md); not resetting the repository")
	}
	if err := git(real, "reset", "--hard", st.Base); err != nil {
		return nil, err
	}
	actions := []string{"git -C " + real + " reset --hard " + st.Base}
	if err := git(real, "clean", "-fd"); err != nil {
		return actions, err
	}
	return append(actions, "git -C "+real+" clean -fd"), nil
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

func restoreFiles(runDir, repo string, st RepoState) ([]string, error) {
	real, err := realRepo(repo)
	if err != nil {
		return nil, err
	}
	if st.Base == "" {
		return nil, errors.New("resume: the base branch is unknown (brief.md); not resetting the repository")
	}
	_, files, err := planFiles(runDir, real)
	if err != nil {
		return nil, err
	}
	// Check every path before anything changes.
	for _, f := range files {
		if _, err := repoPath(real, f); err != nil {
			return nil, err
		}
	}
	var actions []string
	if st.Branch != "" && st.Branch != st.Base && git(real, "rev-parse", "--verify", "--quiet", "refs/heads/"+st.Branch) == nil {
		if err := dropWorkBranch(runDir, real, st); err != nil {
			return nil, err
		}
		actions = append(actions, "deleted branch "+st.Branch)
	}
	for _, f := range files {
		abs, err := repoPath(real, f)
		if err != nil {
			return actions, err
		}
		if git(real, "cat-file", "-e", st.Base+":"+f) == nil {
			if err := git(real, "checkout", st.Base, "--", f); err != nil {
				return actions, err
			}
			actions = append(actions, "restored "+f)
			continue
		}
		if _, err := os.Lstat(abs); err != nil {
			continue
		}
		if err := os.Remove(abs); err != nil {
			return actions, fmt.Errorf("resume: removing %s: %w", f, err)
		}
		actions = append(actions, "removed "+f)
	}
	return actions, nil
}

// dropWorkBranch deletes st.Branch after the executor's own foreign-commit
// check finds no commit on it that the run did not make.
func dropWorkBranch(runDir, repo string, st RepoState) error {
	cli, err := gitland.NewCLI(repo, filepath.Base(runDir))
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	defer cli.Close()
	if err := cli.Start(st.Base, st.Branch, nil); err != nil { // the branch exists: Start switches to it
		return fmt.Errorf("resume: the work branch %s could not be checked out: %w", st.Branch, err)
	}
	n, err := cli.ForeignCommits("")
	if err != nil {
		return fmt.Errorf("resume: the commits of %s could not be read: %w", st.Branch, err)
	}
	if n > 0 {
		return fmt.Errorf("resume: branch %s holds %d commit(s) this run did not make; not deleting it", st.Branch, n)
	}
	if err := git(repo, "checkout", st.Base); err != nil {
		return err
	}
	return git(repo, "branch", "-D", st.Branch)
}

// git runs git in repo. The gitland CLI refuses checkout, reset, clean and
// branch -D by design, so these calls cannot go through it.
func git(repo string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("resume: git %s: %v: %s", args[0], err, msg)
	}
	return nil
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
// plan adds to them when it can be loaded.
func planFiles(runDir, repo string) (tests, all []string, err error) {
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
		if p, e := executor.LoadPlan(runDir, repo); e == nil {
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

// keptTestFiles returns the plan test files that still exist in repo.
func keptTestFiles(runDir, repo string) ([]string, error) {
	real, err := realRepo(repo)
	if err != nil {
		return nil, err
	}
	tests, _, err := planFiles(runDir, real)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range tests {
		abs, err := repoPath(real, f)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(abs); err == nil {
			out = append(out, f)
		}
	}
	return out, nil
}
