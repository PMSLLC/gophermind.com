// Package sandbox builds the macOS sandbox-exec profile every child process of
// the executor runs under, and wraps an exec.Cmd to use it.
//
// Ruling: the plan mandates "(allow default)" followed by targeted denies, so
// process exec, fork, signals and mach-lookup stay open. Only file writes,
// reads of the home directory and network access are contained. The residual
// risk is that model-written code can still reach mach services, exec any
// binary it can read, and signal other processes of the same user. A
// (deny process-exec) rule was tried in scope and left out: go test must run
// binaries it builds under the scratch directory, so a deny would be broad or
// would break the toolchain.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Profile names the paths a sandboxed child may touch. Writes to <Repo>/.git are
// always denied; there is no field for it because the rule is not optional.
type Profile struct {
	Repo             string   // read and write
	RunDir           string   // <repo>/.gophermind/<brief-id>: write denied, read allowed
	Scratch          string   // read and write; HOME and TMPDIR of every child
	GoCache          string   // read and write
	GoModCache       string   // read only, unless ModCacheWritable
	ModCacheWritable bool     // true only for the deps step
	ReadOnly         []string // toolchain roots: GOROOT, the directories of go and git, PATH entries
	Home             string   // the real home directory; empty means no home rule
}

// ErrSandboxRequired is returned when the platform has no sandbox and the
// settings did not explicitly turn it off.
var ErrSandboxRequired = errors.New("sandbox: this OS has no sandbox; set executor.sandbox: off to run without one")

// lookPath locates sandbox-exec. Tests replace it.
var lookPath = func() (string, error) {
	if p, err := exec.LookPath("sandbox-exec"); err == nil {
		return p, nil
	}
	const fallback = "/usr/bin/sandbox-exec"
	if _, err := exec.LookPath(fallback); err != nil {
		return "", err
	}
	return fallback, nil
}

// Required reports whether the sandbox is enabled. On darwin "on" is the
// default and "off" is honored; elsewhere only an explicit "off" is accepted.
func Required(mode, goos string) (bool, error) {
	if mode == "off" {
		return false, nil
	}
	if goos != "darwin" {
		return false, ErrSandboxRequired
	}
	return true, nil
}

// pathOpts tunes resolve per field.
type pathOpts struct {
	writable     bool // may not be "/" nor the user's home directory
	missingSkip  bool // a path that does not exist is skipped, not an error
	missingAsOK  bool // a missing tail is allowed if its nearest ancestor resolves
	allowShallow bool // permit fewer than two path components
}

func validChars(p string) bool {
	if !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r == '"' || r == '\\' || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func components(p string) int {
	t := strings.Trim(p, "/")
	if t == "" {
		return 0
	}
	return strings.Count(t, "/") + 1
}

// resolve validates one path and returns it with symlinks resolved. Errors name
// the field, never the path. A skipped path returns "" and a nil error.
func resolve(field, p string, o pathOpts) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("sandbox: %s is not an absolute path", field)
	}
	if !validChars(p) {
		return "", fmt.Errorf("sandbox: %s contains a character not allowed in a profile", field)
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		if o.missingSkip {
			return "", nil
		}
		if o.missingAsOK {
			cur, rest := filepath.Clean(p), ""
			for {
				parent := filepath.Dir(cur)
				rest = filepath.Join(filepath.Base(cur), rest)
				if parent == cur {
					break
				}
				cur = parent
				if base, e := filepath.EvalSymlinks(cur); e == nil {
					r, err = filepath.Join(base, rest), nil
					break
				}
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("sandbox: %s cannot be resolved", field)
	}
	if !filepath.IsAbs(r) || !validChars(r) {
		return "", fmt.Errorf("sandbox: %s contains a character not allowed in a profile", field)
	}
	if !o.allowShallow && components(r) < 2 {
		return "", fmt.Errorf("sandbox: %s is too broad to sandbox", field)
	}
	if o.writable {
		if h, e := os.UserHomeDir(); e == nil && h != "" {
			if hr, e := filepath.EvalSymlinks(h); e == nil && hr == r {
				return "", fmt.Errorf("sandbox: %s is too broad to sandbox", field)
			}
		}
	}
	return r, nil
}

func subpaths(ps []string) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("(subpath %q)", p)
	}
	return strings.Join(parts, " ")
}

// Text returns the SBPL profile. Later rules win in SBPL, so the denies precede
// the specific allows and are repeated after the read allow.
func (p Profile) Text() (string, error) {
	var repo, scratch, goCache, runDir, modCache, home string
	var err error
	for _, req := range []struct {
		field string
		in    string
		out   *string
		must  bool
		opts  pathOpts
	}{
		{"Repo", p.Repo, &repo, true, pathOpts{writable: true}},
		{"Scratch", p.Scratch, &scratch, true, pathOpts{writable: true}},
		{"GoCache", p.GoCache, &goCache, true, pathOpts{writable: true}},
		{"RunDir", p.RunDir, &runDir, false, pathOpts{writable: true, missingAsOK: true}},
		{"GoModCache", p.GoModCache, &modCache, false, pathOpts{writable: p.ModCacheWritable}},
		{"Home", p.Home, &home, false, pathOpts{}},
	} {
		if req.in == "" {
			if req.must {
				return "", fmt.Errorf("sandbox: %s is required", req.field)
			}
			continue
		}
		if *req.out, err = resolve(req.field, req.in, req.opts); err != nil {
			return "", err
		}
	}
	ro := make([]string, 0, len(p.ReadOnly))
	for i, r := range p.ReadOnly {
		v, err := resolve(fmt.Sprintf("ReadOnly[%d]", i), r, pathOpts{missingSkip: true, allowShallow: true})
		if err != nil {
			return "", err
		}
		if v != "" {
			ro = append(ro, v)
		}
	}

	writes := []string{repo, scratch, goCache}
	reads := []string{repo, scratch, goCache}
	if modCache != "" {
		reads = append(reads, modCache)
		if p.ModCacheWritable {
			writes = append(writes, modCache)
		}
	}
	reads = append(reads, ro...)
	if runDir != "" {
		reads = append(reads, runDir)
	}

	gitDeny := fmt.Sprintf("(deny file-write* (subpath %q) (literal %q))\n", repo+"/.git", repo+"/.git")
	runDeny := ""
	if runDir != "" {
		runDeny = fmt.Sprintf("(deny file-write* (subpath %q))\n", runDir)
	}

	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	fmt.Fprintf(&b, "(allow file-write* %s (literal \"/dev/null\") (literal \"/dev/tty\"))\n", subpaths(writes))
	b.WriteString(gitDeny)
	b.WriteString(runDeny)
	if home != "" {
		fmt.Fprintf(&b, "(deny file-read* (subpath %q))\n", home)
		fmt.Fprintf(&b, "(allow file-read* %s)\n", subpaths(reads))
		b.WriteString(gitDeny)
		b.WriteString(runDeny)
	}
	b.WriteString("(deny network*)\n")
	b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
	b.WriteString("(allow network-inbound (local ip \"localhost:*\"))\n")
	b.WriteString("(allow network-bind (local ip \"localhost:*\"))\n")
	return b.String(), nil
}

// Wrap returns cmd rewritten to run under sandbox-exec. It fails, rather than
// returning an unsandboxed command, when sandbox-exec cannot be found.
func Wrap(cmd *exec.Cmd, p Profile) (*exec.Cmd, error) {
	bin, err := lookPath()
	if err != nil {
		return nil, errors.New("sandbox: sandbox-exec not found; set executor.sandbox: off to run without one")
	}
	text, err := p.Text()
	if err != nil {
		return nil, err
	}
	args := append([]string{"sandbox-exec", "-p", text, "--", cmd.Path}, cmd.Args[min(1, len(cmd.Args)):]...)
	out := *cmd
	out.Path = bin
	out.Args = args
	return &out, nil
}

// Preflight verifies sandbox-exec exists and that a minimal profile loads and
// runs /usr/bin/true.
func Preflight(ctx context.Context) error {
	bin, err := lookPath()
	if err != nil {
		return errors.New("sandbox: sandbox-exec not found; set executor.sandbox: off to run without one")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, bin, "-p", "(version 1)(allow default)", "/usr/bin/true").Run(); err != nil {
		return errors.New("sandbox: sandbox-exec did not run a minimal profile; set executor.sandbox: off to run without one")
	}
	return nil
}
