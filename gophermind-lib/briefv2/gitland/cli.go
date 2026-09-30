package gitland

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	homePrefix   = "gm-gitland-home-"
	subjectLimit = 72
	gitTimeout   = 5 * time.Minute
)

// testGit redirects the git binary. It is set only from _test.go; production always resolves git
// through exec.LookPath once, in NewCLI.
var testGit string

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	idRe     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// fixedPrefix starts every git command so nothing in the target repo's configuration can run.
var fixedPrefix = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "core.sshCommand=",
	"-c", "commit.gpgsign=false",
	"-c", "core.autocrlf=false",
	"-c", "protocol.ext.allow=never",
}

var forbiddenSubs = map[string]bool{
	"push": true, "reset": true, "rebase": true, "checkout": true, "stash": true, "clean": true, "gc": true,
	"prune": true, "remote": true, "fetch": true, "pull": true, "filter-branch": true, "update-ref": true,
}

// CLI runs the git binary with fixed argument vectors, never a shell.
type CLI struct {
	dir   string
	git   string
	runID string
	home  string
	mu    sync.Mutex
	base  string
	work  string
}

var _ Repo = (*CLI)(nil)

// NewCLI prepares a CLI for the repo at dir. runID goes into commit trailers.
func NewCLI(dir, runID string) (*CLI, error) {
	if !idRe.MatchString(runID) {
		return nil, errors.New("gitland: invalid run id")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.New("gitland: invalid repo directory")
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, errors.New("gitland: repo directory is not a directory")
	}
	bin := testGit
	if bin == "" {
		bin, err = exec.LookPath("git")
		if err != nil {
			return nil, errors.New("gitland: git binary not found")
		}
	}
	home, err := os.MkdirTemp("", homePrefix)
	if err != nil {
		return nil, errors.New("gitland: cannot make private home")
	}
	c := &CLI{dir: abs, git: bin, runID: runID, home: home}
	out, err := c.run("rev-parse", "--show-toplevel")
	if err == nil {
		var top string
		top, err = filepath.EvalSymlinks(strings.TrimRight(string(out), "\n"))
		if err == nil && top != abs {
			err = ErrNotRepoRoot
		}
	}
	if err != nil {
		_ = c.Close()
		return nil, ErrNotRepoRoot
	}
	return c, nil
}

// Close removes the private HOME made by NewCLI, only when it still looks like ours.
func (c *CLI) Close() error {
	home := c.home
	if home == "" {
		return nil
	}
	tmp := filepath.Clean(os.TempDir())
	if filepath.Dir(home) != tmp || !strings.HasPrefix(filepath.Base(home), homePrefix) || len(filepath.Base(home)) <= len(homePrefix) {
		return errors.New("gitland: refusing to delete a home directory that is not ours")
	}
	st, err := os.Lstat(home)
	if err != nil {
		c.home = ""
		return nil
	}
	if !st.IsDir() {
		return errors.New("gitland: private home is not a directory")
	}
	if err := os.RemoveAll(home); err != nil {
		return errors.New("gitland: cannot remove private home")
	}
	c.home = ""
	return nil
}

// env is built from scratch: no inherited GIT_* variable reaches git.
func (c *CLI) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + c.home,
		"LANG=C",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=GopherMind",
		"GIT_COMMITTER_NAME=GopherMind",
		"GIT_AUTHOR_EMAIL=gophermind@localhost",
		"GIT_COMMITTER_EMAIL=gophermind@localhost",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_EDITOR=true",
		"GIT_PAGER=cat",
		"GIT_CEILING_DIRECTORIES=" + filepath.Dir(c.dir),
		"GIT_LITERAL_PATHSPECS=1",
	}
}

type gitErr struct {
	sub  string
	code int
}

func (e *gitErr) Error() string {
	return fmt.Sprintf("gitland: git %s failed with exit code %d", e.sub, e.code)
}

// guard applies the denylist to the part of args before any "--".
func guard(args []string) (string, error) {
	sub := ""
	noVerify := false
	for _, a := range args {
		if a == "--" {
			break
		}
		switch a {
		case "--force", "-f", "--force-with-lease", "--hard":
			return "", ErrForbiddenGitArgs
		case "--no-verify":
			noVerify = true
		}
		if sub == "" && !strings.HasPrefix(a, "-") {
			sub = a
		}
	}
	if sub == "" || forbiddenSubs[sub] {
		return "", ErrForbiddenGitArgs
	}
	if noVerify && sub != "commit" {
		return "", ErrForbiddenGitArgs
	}
	if sub == "merge" {
		ff := false
		for _, a := range args {
			if a == "--" {
				break
			}
			switch {
			case a == "--ff-only":
				ff = true
			case a == "--no-ff", a == "--squash", a == "-s", a == "--strategy", strings.HasPrefix(a, "--strategy="),
				strings.HasPrefix(a, "-X"), strings.HasPrefix(a, "--strategy-option"), strings.HasPrefix(a, "-s"):
				return "", ErrForbiddenGitArgs
			}
		}
		if !ff {
			return "", ErrForbiddenGitArgs
		}
	}
	if sub == "branch" {
		for _, a := range args {
			if a == "--" {
				break
			}
			switch a {
			case "-D", "-d", "-f", "-M":
				return "", ErrForbiddenGitArgs
			}
		}
	}
	return sub, nil
}

// runCode is the single place git is spawned. A non-zero exit is returned as the code with a nil error.
func (c *CLI) runCode(args ...string) ([]byte, int, error) { return c.runIn(nil, args...) }

// runIn is runCode with data on stdin.
func (c *CLI) runIn(stdin []byte, args ...string) ([]byte, int, error) {
	sub, err := guard(args)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append(append([]string{}, fixedPrefix...), args...)
	cmd := exec.CommandContext(ctx, c.git, full...)
	cmd.Dir = c.dir
	cmd.Env = c.env()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.Bytes(), ee.ExitCode(), nil
		}
		return nil, 0, fmt.Errorf("gitland: git %s could not run", sub)
	}
	return out.Bytes(), 0, nil
}

func (c *CLI) run(args ...string) ([]byte, error) { return c.runStdin(nil, args...) }

func (c *CLI) runStdin(stdin []byte, args ...string) ([]byte, error) {
	out, code, err := c.runIn(stdin, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		sub, _ := guard(args)
		return nil, &gitErr{sub: sub, code: code}
	}
	return out, nil
}

func validBranch(n string) bool {
	return branchRe.MatchString(n) && !strings.Contains(n, "..") && !strings.HasPrefix(n, "-") && !strings.HasPrefix(n, "/") && !strings.HasSuffix(n, "/") && !strings.HasSuffix(n, ".lock")
}

func validPath(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "-") || filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return false
	}
	if filepath.Clean(p) != p || strings.Contains(p, `\`) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || part == "" || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func validPaths(ps []string) error {
	for _, p := range ps {
		if !validPath(p) {
			return errors.New("gitland: invalid path")
		}
	}
	return nil
}

func (c *CLI) Start(baseBranch, workBranch string, allowDirty []string) error {
	if !validBranch(baseBranch) || !validBranch(workBranch) {
		return errors.New("gitland: invalid branch name")
	}
	if err := validPaths(allowDirty); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, code, err := c.runCode("rev-parse", "--verify", "--quiet", "refs/heads/"+baseBranch); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("gitland: base branch %s does not exist", baseBranch)
	}
	c.base, c.work = baseBranch, workBranch
	if _, code, err := c.runCode("rev-parse", "--verify", "--quiet", "refs/heads/"+workBranch); err != nil {
		return err
	} else if code == 0 {
		_, err := c.run("switch", workBranch)
		return err
	}
	dirty, err := c.Dirty()
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, p := range allowDirty {
		allowed[p] = true
	}
	var foreign []string
	for _, p := range dirty {
		if !allowed[p] {
			foreign = append(foreign, p)
		}
	}
	if len(foreign) > 0 {
		shown := foreign
		if len(shown) > 20 {
			shown = shown[:20]
		}
		q := make([]string, len(shown))
		for i, p := range shown {
			q[i] = fmt.Sprintf("%q", p)
		}
		return fmt.Errorf("%w: %d path(s) not allowed: %s", ErrDirtyTree, len(foreign), strings.Join(q, ", "))
	}
	_, err = c.run("switch", "-c", workBranch, baseBranch)
	return err
}

func (c *CLI) short(rev string) (string, error) {
	out, err := c.run("rev-parse", "--short", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *CLI) Head() (string, error) { return c.short("HEAD") }

func (c *CLI) Branch() (string, error) {
	out, err := c.run("symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func splitNul(b []byte) []string {
	var r []string
	for _, s := range bytes.Split(b, []byte{0}) {
		if len(s) > 0 {
			r = append(r, string(s))
		}
	}
	return r
}

func (c *CLI) Dirty() ([]string, error) {
	out, err := c.run("status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	fields := bytes.Split(out, []byte{0})
	set := map[string]bool{}
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		set[string(f[3:])] = true
		if f[0] == 'R' || f[0] == 'C' || f[1] == 'R' || f[1] == 'C' {
			i++
			if i < len(fields) && len(fields[i]) > 0 {
				set[string(fields[i])] = true
			}
		}
	}
	res := make([]string, 0, len(set))
	for p := range set {
		res = append(res, p)
	}
	sort.Strings(res)
	return res, nil
}

// Ignored lists the ignored files (NUL-separated status, so quoted and odd names are exact), except the
// .gophermind/ run folder.
func (c *CLI) Ignored() ([]string, error) {
	out, err := c.run("status", "--porcelain=v1", "-z", "--ignored=traditional", "-uall")
	if err != nil {
		return nil, err
	}
	var res []string
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) < 4 || f[0] != '!' || f[1] != '!' {
			continue
		}
		p := string(f[3:])
		if p == ".gophermind" || strings.HasPrefix(p, ".gophermind/") {
			continue
		}
		res = append(res, p)
	}
	sort.Strings(res)
	return res, nil
}

func cleanText(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || r == utf8.RuneError:
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut]
	}
	return strings.TrimSpace(out)
}

// commitPaths stages exactly the named paths, proves the index holds nothing else, and commits.
func (c *CLI) commitPaths(add, remove []string, subject string, trailers []string) (string, error) {
	named := append(append([]string{}, add...), remove...)
	if err := validPaths(named); err != nil {
		return "", err
	}
	if len(named) == 0 {
		return "", ErrNothingToCommit
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.run(append([]string{"add", "-A", "--"}, named...)...); err != nil {
		return "", err
	}
	out, err := c.run("diff", "--cached", "--name-only", "--no-renames", "-z")
	if err != nil {
		return "", err
	}
	staged := splitNul(out)
	ok := map[string]bool{}
	for _, p := range named {
		ok[p] = true
	}
	var mine []string
	foreign := false
	for _, p := range staged {
		if ok[p] {
			mine = append(mine, p)
		} else {
			foreign = true
		}
	}
	if foreign {
		if len(mine) > 0 {
			if _, err := c.run(append([]string{"restore", "--staged", "--"}, mine...)...); err != nil {
				return "", err
			}
		}
		return "", ErrIndexNotClean
	}
	if len(staged) == 0 {
		return "", ErrNothingToCommit
	}
	if _, err := c.run("commit", "--no-verify", "-m", subject, "-m", strings.Join(trailers, "\n")); err != nil {
		return "", err
	}
	return c.short("HEAD")
}

func (c *CLI) runTrailer() string { return "GopherMind-Run: " + c.runID }

func (c *CLI) CommitWave0(paths []string) (string, error) {
	return c.commitPaths(paths, nil, "gm(wave0): tests, types, stubs and go.mod", []string{c.runTrailer()})
}

func (c *CLI) CommitLeaf(nodeID, title string, add, remove []string) (string, error) {
	if !idRe.MatchString(nodeID) {
		return "", errors.New("gitland: invalid node id")
	}
	subject := "gm(" + nodeID + "): " + cleanText(title, subjectLimit)
	return c.commitPaths(add, remove, subject, []string{"GopherMind-Node: " + nodeID, c.runTrailer()})
}

func (c *CLI) CommitRepair(nodeID string, round int, add []string) (string, error) {
	if !idRe.MatchString(nodeID) {
		return "", errors.New("gitland: invalid node id")
	}
	subject := fmt.Sprintf("gm(%s): repair round %d", nodeID, round)
	return c.commitPaths(add, nil, subject, []string{"GopherMind-Node: " + nodeID, fmt.Sprintf("GopherMind-Repair: %d", round), c.runTrailer()})
}

func (c *CLI) Finish(msg string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.base == "" || c.work == "" {
		return "", errors.New("gitland: Finish called before Start")
	}
	if cur, err := c.Branch(); err != nil {
		return "", err
	} else if cur != c.work {
		if _, err := c.run("switch", c.work); err != nil {
			return "", err
		}
	}
	if _, code, err := c.runCode("diff", "--cached", "--quiet"); err != nil {
		return "", err
	} else if code != 0 {
		return "", ErrIndexNotClean
	}
	first, rest, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	args := []string{"commit", "--allow-empty", "--no-verify", "-m", "gm(run): " + cleanText(first, subjectLimit)}
	if rest = strings.TrimSpace(rest); rest != "" {
		args = append(args, "-m", rest)
	}
	args = append(args, "-m", c.runTrailer())
	if _, err := c.run(args...); err != nil {
		return "", err
	}
	_, code, err := c.runCode("merge-base", "--is-ancestor", c.base, c.work)
	if err != nil {
		return "", err
	}
	if code == 1 {
		return "", ErrLandingBlocked
	}
	if code != 0 {
		return "", &gitErr{sub: "merge-base", code: code}
	}
	if _, err := c.run("switch", c.base); err != nil {
		return "", fmt.Errorf("%w: cannot switch to the base branch", ErrDirtyTree)
	}
	if _, err := c.run("merge", "--ff-only", c.work); err != nil {
		_, _ = c.run("switch", c.work)
		return "", ErrLandingBlocked
	}
	return c.short(c.base)
}

func (c *CLI) Restore(paths []string) error {
	if err := validPaths(paths); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := filepath.EvalSymlinks(c.dir)
	if err != nil {
		return errors.New("gitland: cannot resolve repo")
	}
	for _, p := range paths {
		if st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p))); err == nil && st.IsDir() {
			return errors.New("gitland: refusing to restore a directory")
		}
		_, code, err := c.runCode("ls-files", "--error-unmatch", "--", p)
		if err != nil {
			return err
		}
		if code == 0 {
			if _, err := c.run("restore", "--source=HEAD", "--staged", "--worktree", "--", p); err != nil {
				return err
			}
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(p))
		st, err := os.Lstat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return errors.New("gitland: cannot inspect path")
		}
		if !st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 {
			return errors.New("gitland: refusing to remove a non-file path")
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil || (parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator))) {
			return errors.New("gitland: path resolves outside the repo")
		}
		target := filepath.Join(parent, filepath.Base(abs))
		if target == "" || target == root {
			return errors.New("gitland: invalid delete target")
		}
		if err := os.Remove(target); err != nil {
			return errors.New("gitland: cannot remove untracked file")
		}
	}
	return nil
}

func (c *CLI) Diff(base string) ([]byte, error) {
	if !validBranch(base) {
		return nil, errors.New("gitland: invalid branch name")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out, err := c.run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range splitNul(out) {
		if f == ".gophermind" || strings.HasPrefix(f, ".gophermind/") {
			continue
		}
		files = append(files, f)
	}
	if len(files) > 0 {
		list := []byte(strings.Join(files, "\x00") + "\x00")
		if _, err := c.runStdin(list, "add", "--intent-to-add", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return nil, err
		}
		// put the index back as found once the patch is read
		defer func() {
			_, _ = c.runStdin(list, "restore", "--staged", "--pathspec-from-file=-", "--pathspec-file-nul")
		}()
	}
	return c.run("diff", "--no-color", "--binary", "--no-ext-diff", "--no-textconv", "--no-renames", base, "--")
}

func (c *CLI) LeafCommit(nodeID string) (string, bool, error) {
	if !idRe.MatchString(nodeID) {
		return "", false, nil
	}
	const f = "%H%x1f%s%x1f%(trailers:key=GopherMind-Node,valueonly,unfold)%x1f%(trailers:key=GopherMind-Repair,valueonly,unfold)%x1e"
	out, err := c.run("log", "--format="+f)
	if err != nil {
		return "", false, err
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		parts := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(parts) != 4 {
			continue
		}
		if strings.HasPrefix(parts[1], "gm("+nodeID+"): ") && strings.TrimSpace(parts[2]) == nodeID && strings.TrimSpace(parts[3]) == "" {
			h, err := c.short(strings.TrimSpace(parts[0]))
			return h, err == nil, err
		}
	}
	return "", false, nil
}
