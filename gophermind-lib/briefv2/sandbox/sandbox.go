// Package sandbox builds the macOS sandbox-exec profile every child process of
// the executor runs under, and wraps an exec.Cmd to use it.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

// resolve validates one path and returns it with symlinks resolved. Errors name
// the field and a byte count, never the path.
func resolve(field, p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("sandbox: %s (%d bytes) is not an absolute path", field, len(p))
	}
	if strings.ContainsAny(p, "\"\\\x00\n") {
		return "", fmt.Errorf("sandbox: %s (%d bytes) contains a character not allowed in a profile", field, len(p))
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("sandbox: %s (%d bytes) cannot be resolved", field, len(p))
	}
	if !filepath.IsAbs(r) || strings.ContainsAny(r, "\"\\\x00\n") {
		return "", fmt.Errorf("sandbox: %s (%d bytes) contains a character not allowed in a profile", field, len(p))
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
	}{
		{"Repo", p.Repo, &repo, true},
		{"Scratch", p.Scratch, &scratch, true},
		{"GoCache", p.GoCache, &goCache, true},
		{"RunDir", p.RunDir, &runDir, false},
		{"GoModCache", p.GoModCache, &modCache, false},
		{"Home", p.Home, &home, false},
	} {
		if req.in == "" {
			if req.must {
				return "", fmt.Errorf("sandbox: %s is required", req.field)
			}
			continue
		}
		if *req.out, err = resolve(req.field, req.in); err != nil {
			return "", err
		}
	}
	ro := make([]string, 0, len(p.ReadOnly))
	for i, r := range p.ReadOnly {
		v, err := resolve(fmt.Sprintf("ReadOnly[%d]", i), r)
		if err != nil {
			return "", err
		}
		ro = append(ro, v)
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
