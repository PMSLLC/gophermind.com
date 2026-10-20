// Package sandbox builds the macOS sandbox-exec profile every child process of
// the executor runs under, and wraps an exec.Cmd to use it.
//
// Ruling: the plan mandates "(allow default)" followed by targeted denies, so
// fork and ordinary exec stay open and go test can run binaries it builds under
// the scratch directory. The targeted denies cover the escapes a reviewer
// verified with the real sandbox-exec: Launch Services (open, lsd, Apple
// events, osascript), the pasteboard, the security daemons, signals to
// processes outside the child's own process group, a short list of launcher
// and remote-access binaries, loopback ports that were not listed, secret-named
// files in the repo and writes under <repo>/.gophermind. SBPL has no port
// ranges and the compiler refuses a profile of several thousand port filters,
// so LoopbackRange is expanded into single ports and capped at maxRangePorts.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	LoopbackPorts    []int    // loopback TCP ports a child may connect to; none listed means no loopback at all
	LoopbackRange    [2]int   // inclusive range of further loopback ports (at most maxRangePorts); {0,0} means none
}

// maxRangePorts is the widest LoopbackRange the sandbox compiler accepts with margin.
const maxRangePorts = 4096

// deniedExec lists binaries a child may not execute. curl, go and git stay allowed.
var deniedExec = []string{
	"/usr/bin/open", "/usr/bin/osascript", "/usr/bin/security", "/bin/launchctl", "/usr/bin/sudo",
	"/usr/bin/ssh", "/usr/bin/scp", "/usr/bin/nc", "/usr/bin/su", "/usr/bin/login",
}

// secretRegex lists the file name patterns denied for reads inside the repo.
// They sit inside a require-all with the repo subpath, so the repo path itself is
// never placed in a regex and needs no regex escaping.
var secretRegex = []string{
	`/\.env(\.[^/]*)?$`,
	`\.pem$`,
	`\.key$`,
	`/id_rsa[^/]*$`,
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

// loopbackPorts validates LoopbackPorts and LoopbackRange and returns the sorted,
// de-duplicated port list. Errors name the field, never the value.
func (p Profile) loopbackPorts() ([]int, error) {
	seen := map[int]bool{}
	for _, n := range p.LoopbackPorts {
		if n < 1 || n > 65535 {
			return nil, errors.New("sandbox: LoopbackPorts holds a port outside 1-65535")
		}
		seen[n] = true
	}
	if lo, hi := p.LoopbackRange[0], p.LoopbackRange[1]; lo != 0 || hi != 0 {
		if lo < 1 || hi > 65535 || lo > hi {
			return nil, errors.New("sandbox: LoopbackRange is not a valid port range")
		}
		if hi-lo+1 > maxRangePorts {
			return nil, errors.New("sandbox: LoopbackRange is too wide")
		}
		for n := lo; n <= hi; n++ {
			seen[n] = true
		}
	}
	out := make([]int, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
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

	ports, err := p.loopbackPorts()
	if err != nil {
		return "", err
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
	// Writes under <repo>/.gophermind are denied (other briefs' run directories,
	// ledgers and caches); a scratch or cache directory placed inside it stays
	// writable, and the run's own RunDir is denied again after that.
	gmDir := repo + "/.gophermind"
	gmDeny := fmt.Sprintf("(deny file-write* (subpath %q))\n", gmDir)
	for _, w := range []string{scratch, goCache} {
		if strings.HasPrefix(w, gmDir+"/") {
			gmDeny += fmt.Sprintf("(allow file-write* (subpath %q))\n", w)
		}
	}
	runDeny := ""
	if runDir != "" {
		runDeny = fmt.Sprintf("(deny file-write* (subpath %q))\n", runDir)
	}
	writeDeny := gitDeny + gmDeny + runDeny

	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	// Escape routes that (allow default) leaves open: Launch Services and Apple
	// events, the pasteboard, the security daemons, signals to outsiders and
	// launcher binaries. Later rules win, so each deny is followed by its allow.
	b.WriteString("(deny mach-lookup (global-name-prefix \"com.apple.coreservices\") (global-name-prefix \"com.apple.lsd\"))\n")
	b.WriteString("(deny mach-lookup (global-name \"com.apple.pasteboard.1\"))\n")
	b.WriteString("(deny mach-lookup (global-name \"com.apple.SecurityServer\") (global-name \"com.apple.securityd\") (global-name \"com.apple.security.agent\"))\n")
	b.WriteString("(deny mach-lookup (global-name \"com.apple.coreservices.appleevents\"))\n")
	b.WriteString("(deny signal)\n")
	b.WriteString("(allow signal (target self) (target pgrp) (target children))\n")
	lits := make([]string, len(deniedExec))
	for i, e := range deniedExec {
		lits[i] = fmt.Sprintf("(literal %q)", e)
	}
	fmt.Fprintf(&b, "(deny process-exec %s)\n", strings.Join(lits, " "))
	b.WriteString("(deny file-write*)\n")
	fmt.Fprintf(&b, "(allow file-write* %s (literal \"/dev/null\") (literal \"/dev/tty\"))\n", subpaths(writes))
	b.WriteString(writeDeny)
	if home != "" {
		fmt.Fprintf(&b, "(deny file-read* (subpath %q))\n", home)
		fmt.Fprintf(&b, "(allow file-read* %s)\n", subpaths(reads))
		b.WriteString(writeDeny)
	}
	// Secret-named files in the repo are unreadable whether or not a home rule
	// exists; these come after the reads allow so they win.
	for _, re := range secretRegex {
		fmt.Fprintf(&b, "(deny file-read* (require-all (subpath %q) (regex #\"%s\")))\n", repo, re)
	}
	fmt.Fprintf(&b, "(deny file-read* (literal %q))\n", repo+"/.git/config")
	b.WriteString("(deny network*)\n")
	if len(ports) > 0 {
		pf := make([]string, len(ports))
		for i, n := range ports {
			pf[i] = fmt.Sprintf("(remote ip \"localhost:%d\")", n)
		}
		fmt.Fprintf(&b, "(allow network-outbound %s)\n", strings.Join(pf, " "))
	}
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
