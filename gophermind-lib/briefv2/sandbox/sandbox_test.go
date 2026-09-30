package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// realDir returns a resolved temp directory.
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func baseProfile(t *testing.T) Profile {
	t.Helper()
	return Profile{Repo: realDir(t), Scratch: realDir(t), GoCache: realDir(t)}
}

func TestProfileEscapesAndRejectsPaths(t *testing.T) {
	good := realDir(t)
	cases := []struct {
		name string
		path string
	}{
		{"quote", good + `/a"b`},
		{"newline", good + "/a\nb"},
		{"backslash", good + `/a\b`},
		{"nul", good + "/a\x00b"},
		{"relative", "rel/secretpath"},
		{"unresolvable", good + "/does-not-exist-secretpath"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Profile{Repo: good, Scratch: good, GoCache: c.path}
			_, err := p.Text()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "GoCache") {
				t.Fatalf("error does not name the field: %v", err)
			}
			if strings.Contains(err.Error(), "secretpath") || strings.Contains(err.Error(), good) {
				t.Fatalf("error leaks path text: %v", err)
			}
		})
	}
	for _, field := range []string{"Repo", "Scratch", "GoCache"} {
		p := baseProfile(t)
		switch field {
		case "Repo":
			p.Repo = ""
		case "Scratch":
			p.Scratch = ""
		case "GoCache":
			p.GoCache = ""
		}
		if _, err := p.Text(); err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("missing %s: %v", field, err)
		}
	}
	// Symlinked temp dirs are rendered resolved.
	if runtime.GOOS == "darwin" {
		raw := t.TempDir()
		if !strings.HasPrefix(raw, "/var/") {
			t.Skipf("temp dir %q is not under /var", raw)
		}
		p := baseProfile(t)
		p.Scratch = raw
		txt, err := p.Text()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(txt, "/private/var") || strings.Contains(txt, `"`+raw+`"`) {
			t.Fatal("profile did not render the resolved path")
		}
	}
}

func TestProfileOrder(t *testing.T) {
	p := baseProfile(t)
	p.RunDir = filepath.Join(p.Repo, ".gophermind", "gm-x")
	if err := os.MkdirAll(p.RunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p.GoModCache = realDir(t)
	p.ReadOnly = []string{realDir(t)}
	p.Home = realDir(t)
	txt, err := p.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(txt, "unix-socket") {
		t.Fatal("profile holds a unix-socket rule")
	}
	gitDeny := fmt.Sprintf("(deny file-write* (subpath %q) (literal %q))\n", p.Repo+"/.git", p.Repo+"/.git")
	runDeny := fmt.Sprintf("(deny file-write* (subpath %q))\n", p.RunDir)
	writeAllow := strings.Index(txt, "(allow file-write* ")
	readAllow := strings.Index(txt, "(allow file-read* ")
	homeDeny := strings.Index(txt, fmt.Sprintf("(deny file-read* (subpath %q))", p.Home))
	if writeAllow < 0 || readAllow < 0 || homeDeny < 0 {
		t.Fatalf("missing rules: %d %d %d", writeAllow, readAllow, homeDeny)
	}
	if homeDeny > readAllow {
		t.Fatal("home deny must precede the reads allow")
	}
	for name, deny := range map[string]string{"git": gitDeny, "rundir": runDeny} {
		if strings.Count(txt, deny) != 2 {
			t.Fatalf("%s deny count = %d, want 2", name, strings.Count(txt, deny))
		}
		first := strings.Index(txt, deny)
		last := strings.LastIndex(txt, deny)
		if first < writeAllow {
			t.Fatalf("%s deny precedes the write allow", name)
		}
		if last < readAllow {
			t.Fatalf("%s deny is not repeated after the read allow", name)
		}
	}
	netDeny := strings.Index(txt, "(deny network*)")
	if netDeny < 0 {
		t.Fatal("no network deny")
	}
	for _, rule := range []string{"(allow network-outbound", "(allow network-inbound", "(allow network-bind"} {
		i := strings.Index(txt, rule)
		if i < 0 || i < netDeny {
			t.Fatalf("%s missing or before the network deny", rule)
		}
	}
	// Modcache: read-only unless ModCacheWritable.
	writeLine := func(s string) string {
		i := strings.Index(s, "(allow file-write* ")
		return s[i : i+strings.Index(s[i:], "\n")]
	}
	if strings.Contains(writeLine(txt), p.GoModCache) {
		t.Fatal("modcache is writable without ModCacheWritable")
	}
	if !strings.Contains(txt[readAllow:], p.GoModCache) {
		t.Fatal("modcache missing from the read allow")
	}
	p.ModCacheWritable = true
	txt2, err := p.Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writeLine(txt2), p.GoModCache) {
		t.Fatal("modcache not writable with ModCacheWritable")
	}
	// No home means no home rule and no reads allow.
	p.Home = ""
	txt3, _ := p.Text()
	if strings.Contains(txt3, "file-read*") {
		t.Fatal("read rules present without Home")
	}
}

func TestSandboxRequiredOffDarwin(t *testing.T) {
	cases := []struct {
		mode, goos string
		en         bool
		err        error
	}{
		{"on", "linux", false, ErrSandboxRequired},
		{"", "linux", false, ErrSandboxRequired},
		{"off", "linux", false, nil},
		{"on", "darwin", true, nil},
		{"", "darwin", true, nil},
		{"off", "darwin", false, nil},
	}
	for _, c := range cases {
		en, err := Required(c.mode, c.goos)
		if en != c.en || !errors.Is(err, c.err) && err != c.err {
			t.Errorf("Required(%q,%q) = %v,%v", c.mode, c.goos, en, err)
		}
	}
}

func TestWrapBuildsSandboxExecCommand(t *testing.T) {
	old := lookPath
	lookPath = func() (string, error) { return "/usr/bin/sandbox-exec", nil }
	defer func() { lookPath = old }()
	p := baseProfile(t)
	cmd := exec.Command("/bin/echo", "a", "b")
	cmd.Dir = "/tmp"
	cmd.Env = []string{"X=1"}
	out, err := Wrap(cmd, p)
	if err != nil {
		t.Fatal(err)
	}
	txt, _ := p.Text()
	if out.Path != "/usr/bin/sandbox-exec" {
		t.Fatalf("path %q", out.Path)
	}
	want := []string{"sandbox-exec", "-p", txt, "--", "/bin/echo", "a", "b"}
	if strings.Join(out.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args %q", out.Args)
	}
	if out.Dir != "/tmp" || len(out.Env) != 1 || out.Env[0] != "X=1" {
		t.Fatal("Dir or Env not preserved")
	}
}

func TestSandboxPreflightMissingBinary(t *testing.T) {
	old := lookPath
	lookPath = func() (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = old }()
	err := Preflight(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sandbox-exec") || !strings.Contains(err.Error(), "executor.sandbox: off") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Wrap(exec.Command("/bin/true"), baseProfile(t)); err == nil {
		t.Fatal("Wrap must fail when sandbox-exec is missing")
	}
}

// ---- containment tests: these really run sandbox-exec ----

func skipIfNoSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec not available: GOOS is " + runtime.GOOS)
	}
	if err := Preflight(context.Background()); err != nil {
		t.Skip("sandbox-exec not available: " + err.Error())
	}
}

func runWrapped(t *testing.T, p Profile, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	w, err := Wrap(cmd, p)
	if err != nil {
		t.Fatal(err)
	}
	return w.Run()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSandboxDeniesWriteOutside(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	other := realDir(t)
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", filepath.Join(p.Repo, "in.txt")); err != nil || !exists(filepath.Join(p.Repo, "in.txt")) {
		t.Fatalf("write inside repo failed: %v", err)
	}
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", filepath.Join(p.Scratch, "in.txt")); err != nil || !exists(filepath.Join(p.Scratch, "in.txt")) {
		t.Fatalf("write inside scratch failed: %v", err)
	}
	out := filepath.Join(other, "out.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", out); err == nil || exists(out) {
		t.Fatalf("write outside succeeded (err=%v)", err)
	}
	// control: unsandboxed the same write works
	if err := exec.Command("/bin/sh", "-c", "echo x > \"$1\"", "sh", out).Run(); err != nil || !exists(out) {
		t.Fatalf("control write failed: %v", err)
	}
}

func TestSandboxDeniesRunDirWrite(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	p.RunDir = filepath.Join(p.Repo, ".gophermind", "gm-x")
	if err := os.MkdirAll(p.RunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p.RunDir, "f.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", f); err == nil || exists(f) {
		t.Fatalf("run dir write succeeded (err=%v)", err)
	}
	ok := filepath.Join(p.Repo, "ok.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", ok); err != nil || !exists(ok) {
		t.Fatalf("repo write failed: %v", err)
	}
	// control
	if err := exec.Command("/bin/sh", "-c", "echo x > \"$1\"", "sh", f).Run(); err != nil || !exists(f) {
		t.Fatalf("control write failed: %v", err)
	}
}

func TestSandboxDeniesGitWrite(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	git := filepath.Join(p.Repo, ".git")
	if err := os.MkdirAll(filepath.Join(git, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	targets := []string{
		filepath.Join(git, "config"),
		filepath.Join(git, "hooks", "pre-commit"),
		filepath.Join(git, "index.lock"),
	}
	for _, f := range targets {
		if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", f); err == nil || exists(f) {
			t.Fatalf("sandboxed write to %s succeeded (err=%v)", filepath.Base(f), err)
		}
	}
	ok := filepath.Join(p.Repo, "ok.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", ok); err != nil || !exists(ok) {
		t.Fatalf("repo write failed: %v", err)
	}
	for _, f := range targets {
		if err := exec.Command("/bin/sh", "-c", "echo x > \"$1\"", "sh", f).Run(); err != nil || !exists(f) {
			t.Fatalf("control write to %s failed: %v", filepath.Base(f), err)
		}
	}

	// linked-worktree form: .git is a regular file
	q := baseProfile(t)
	gf := filepath.Join(q.Repo, ".git")
	const orig = "gitdir: /elsewhere\n"
	if err := os.WriteFile(gf, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runWrapped(t, q, "/bin/sh", "-c", "echo x > \"$1\"", "sh", gf); err == nil {
		t.Fatal("overwriting the .git file succeeded")
	}
	if b, _ := os.ReadFile(gf); string(b) != orig {
		t.Fatal(".git file changed")
	}
}

func TestSandboxDeniesHomeRead(t *testing.T) {
	skipIfNoSandbox(t)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		t.Skip("home does not resolve")
	}
	canary, err := os.MkdirTemp(home, "gm-sbtest-")
	if err != nil {
		t.Skip("cannot create a canary under home")
	}
	t.Cleanup(func() {
		target := canary
		if target != "" && filepath.Dir(target) == home && strings.HasPrefix(filepath.Base(target), "gm-sbtest-") {
			_ = os.RemoveAll(target)
		} else {
			t.Logf("canary delete refused")
		}
	})
	file := filepath.Join(canary, "secret.txt")
	if err := os.WriteFile(file, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := baseProfile(t)
	p.Home = home
	if err := runWrapped(t, p, "/bin/cat", file); err == nil {
		t.Fatal("sandboxed read of the home canary succeeded")
	}
	if err := exec.Command("/bin/cat", file).Run(); err != nil {
		t.Fatalf("control read failed: %v", err)
	}
	// reads of the allowed dirs still work with a home rule
	in := filepath.Join(p.Scratch, "r.txt")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWrapped(t, p, "/bin/cat", in); err != nil {
		t.Fatalf("read of scratch failed: %v", err)
	}
}

func portOf(l net.Listener) string {
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// The denied target is a documentation-range address (RFC 5737) that no host
// answers, so the test needs no network: the sandbox denial is the "Operation
// not permitted" error from connect. The positive control is a loopback
// listener reached under the very same profile. The unsandboxed run of the same
// connect is only logged: a VPN or firewall may itself return EPERM.
func TestSandboxDeniesNonLoopback(t *testing.T) {
	skipIfNoSandbox(t)
	const target, port = "192.0.2.1", "80"
	p := baseProfile(t)
	run := func(sandboxed bool, host, port string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/usr/bin/nc", "-zv", "-w", "1", host, port)
		if sandboxed {
			w, err := Wrap(cmd, p)
			if err != nil {
				t.Fatal(err)
			}
			cmd = w
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen: " + err.Error())
	}
	defer l.Close()
	if out, err := run(true, "127.0.0.1", portOf(l)); err != nil {
		t.Fatalf("sandboxed loopback connect failed (positive control): %v %s", err, out)
	}
	out, err := run(true, target, port)
	if err == nil {
		t.Fatal("sandboxed non-loopback connect succeeded")
	}
	if !strings.Contains(out, "Operation not permitted") {
		t.Fatalf("non-loopback connect failed but not with a sandbox denial (timeout?): %v", err)
	}
	if cout, _ := run(false, target, port); strings.Contains(cout, "Operation not permitted") {
		t.Log("note: the unsandboxed control also saw EPERM (VPN or firewall); the assertions above do not depend on it")
	}
}

func TestSandboxAllowsLoopback(t *testing.T) {
	skipIfNoSandbox(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen: " + err.Error())
	}
	defer l.Close()
	if err := runWrapped(t, baseProfile(t), "/usr/bin/nc", "-z", "-w", "2", "127.0.0.1", portOf(l)); err != nil {
		t.Fatalf("sandboxed loopback connect failed: %v", err)
	}
}

func TestSandboxGoBuildAndTest(t *testing.T) {
	skipIfNoSandbox(t)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	goBin, _ = filepath.EvalSymlinks(goBin)
	root, err := exec.Command(goBin, "env", "GOROOT").Output()
	if err != nil {
		t.Skip("go env GOROOT failed")
	}
	p := baseProfile(t)
	p.ReadOnly = []string{strings.TrimSpace(string(root)), filepath.Dir(goBin)}
	if h, err := os.UserHomeDir(); err == nil {
		p.Home = h
	}
	mod := p.Repo
	files := map[string]string{
		"go.mod": "module sbmod\n\ngo 1.21\n",
		"x.go":   "package sbmod\n\nfunc Add(a, b int) int { return a + b }\n",
		"x_test.go": `package sbmod

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("bad")
	}
}

// The sandbox alone turns this dial into EPERM; unsandboxed it times out or is
// unreachable, so a pass proves the deny rather than an unanswered address.
func TestNoExternal(t *testing.T) {
	c, err := net.DialTimeout("tcp", "192.0.2.1:80", time.Second)
	if err == nil {
		c.Close()
		t.Fatal("dial to a non-loopback address succeeded")
	}
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("dial error is not a permission error: %v", err)
	}
}
`,
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(mod, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "test", "-count=1", "./...")
	cmd.Dir = mod
	cmd.Env = []string{
		"PATH=" + filepath.Dir(goBin) + ":/usr/bin:/bin",
		"GOFLAGS=-mod=readonly -buildvcs=false", "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0",
		"HOME=" + p.Scratch, "TMPDIR=" + p.Scratch, "GOCACHE=" + p.GoCache,
	}
	w, err := Wrap(cmd, p)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var out []byte
	go func() { out, err = w.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		_ = w.Process.Kill()
		t.Fatal("go test timed out under the sandbox")
	}
	if err != nil {
		t.Fatalf("go test under sandbox failed: %v\n%s", err, out)
	}
}

func TestProfileRejectsBroadPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	good := realDir(t)
	broad := []string{"/", "/private", home, home + "/"}
	for _, field := range []string{"Repo", "Scratch", "GoCache", "RunDir", "GoModCache"} {
		for _, bp := range broad {
			p := Profile{Repo: good, Scratch: good, GoCache: good, ModCacheWritable: true}
			switch field {
			case "Repo":
				p.Repo = bp
			case "Scratch":
				p.Scratch = bp
			case "GoCache":
				p.GoCache = bp
			case "RunDir":
				p.RunDir = bp
			case "GoModCache":
				p.GoModCache = bp
			}
			_, err := p.Text()
			if err == nil {
				t.Fatalf("%s=%q accepted", field, bp)
			}
			if !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), home) {
				t.Fatalf("%s: bad error %v", field, err)
			}
		}
	}
	// Home itself may be broad-ish only as a read deny; "/" is still refused.
	if _, err := (Profile{Repo: good, Scratch: good, GoCache: good, Home: "/"}).Text(); err == nil {
		t.Fatal("Home=/ accepted")
	}
}

func TestProfileSkipsMissingReadOnlyAndCreatesNoRunDir(t *testing.T) {
	p := baseProfile(t)
	missing := filepath.Join(realDir(t), "gone-secretname")
	p.ReadOnly = []string{missing, realDir(t)}
	p.RunDir = filepath.Join(p.Repo, ".gophermind", "gm-new")
	txt, err := p.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(txt, "gone-secretname") {
		t.Fatal("missing ReadOnly entry rendered")
	}
	if !strings.Contains(txt, p.RunDir) {
		t.Fatal("RunDir not rendered")
	}
	if exists(p.RunDir) {
		t.Fatal("Text created the RunDir")
	}
}

// topLevelForms parses SBPL just enough to list the "head arg" of each top
// level form, honoring quoted strings.
func topLevelForms(t *testing.T, txt string) []string {
	t.Helper()
	var forms []string
	depth, inStr := 0, false
	start := -1
	for i := 0; i < len(txt); i++ {
		c := txt[i]
		if inStr {
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				f := strings.Fields(txt[start:i])
				if len(f) < 2 {
					t.Fatalf("short form %q", txt[start:i])
				}
				forms = append(forms, f[0]+" "+f[1])
			}
		}
	}
	if depth != 0 || inStr {
		t.Fatal("unbalanced profile")
	}
	return forms
}

func TestProfileHostilePathsInjectNothing(t *testing.T) {
	expected := []string{
		"version 1", "allow default", "deny file-write*", "allow file-write*",
		"deny file-write*", "deny file-write*", "deny file-read*", "allow file-read*",
		"deny file-write*", "deny file-write*", "deny network*",
		"allow network-outbound", "allow network-inbound", "allow network-bind",
	}
	hostile := []string{
		"x)y", "(allow default", "(allow default)", "a\"b", "a\\b", "a\nb", "a\tb", "a\rb",
		"a\xffb", "a\x00b", "a\u2028b", ")(allow file-write* (subpath \"/\"))",
	}
	fields := []string{"Repo", "RunDir", "Scratch", "GoCache", "GoModCache", "Home", "ReadOnly"}
	for _, field := range fields {
		for _, h := range hostile {
			base := realDir(t)
			path := filepath.Join(base, h)
			// Names the filesystem can hold are created so they resolve.
			if err := os.Mkdir(path, 0o755); err != nil && field != "RunDir" {
				// not creatable: the path must still be refused, not injected
				_ = err
			}
			good := realDir(t)
			p := Profile{Repo: good, Scratch: realDir(t), GoCache: realDir(t),
				RunDir: filepath.Join(good, "run"), GoModCache: realDir(t), Home: realDir(t),
				ReadOnly: []string{realDir(t)}}
			switch field {
			case "Repo":
				p.Repo = path
				p.RunDir = ""
			case "RunDir":
				p.RunDir = path
			case "Scratch":
				p.Scratch = path
			case "GoCache":
				p.GoCache = path
			case "GoModCache":
				p.GoModCache = path
			case "Home":
				p.Home = path
			case "ReadOnly":
				p.ReadOnly = []string{path}
			}
			txt, err := p.Text()
			if err != nil {
				if !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), h) {
					t.Fatalf("%s %q: unexpected error %q", field, h, err)
				}
				continue
			}
			forms := topLevelForms(t, txt)
			want := expected
			if field == "Repo" {
				// no RunDir: two run-dir denies drop out
				want = []string{"version 1", "allow default", "deny file-write*", "allow file-write*",
					"deny file-write*", "deny file-read*", "allow file-read*", "deny file-write*",
					"deny network*", "allow network-outbound", "allow network-inbound", "allow network-bind"}
			}
			if strings.Join(forms, "|") != strings.Join(want, "|") {
				t.Fatalf("%s %q: forms %q, want %q", field, h, forms, want)
			}
		}
	}
}

func TestSandboxDeniesGitUnlinkAndRename(t *testing.T) {
	skipIfNoSandbox(t)
	ops := map[string]func(repo string) []string{
		"unlink HEAD": func(r string) []string { return []string{"rm", filepath.Join(r, ".git", "HEAD")} },
		"rename HEAD": func(r string) []string {
			return []string{"mv", filepath.Join(r, ".git", "HEAD"), filepath.Join(r, ".git", "HEAD2")}
		},
		"rename .git": func(r string) []string { return []string{"mv", filepath.Join(r, ".git"), filepath.Join(r, "moved")} },
		"remove .git": func(r string) []string { return []string{"rm", "-rf", filepath.Join(r, ".git")} },
		"rename in": func(r string) []string {
			return []string{"mv", filepath.Join(r, "ok.txt"), filepath.Join(r, ".git", "ok.txt")}
		},
		"rename hooks": func(r string) []string {
			return []string{"mv", filepath.Join(r, ".git", "hooks"), filepath.Join(r, "hooks")}
		},
	}
	setup := func(t *testing.T) Profile {
		p := baseProfile(t)
		for _, d := range []string{".git/hooks"} {
			if err := os.MkdirAll(filepath.Join(p.Repo, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range []string{".git/HEAD", "ok.txt"} {
			if err := os.WriteFile(filepath.Join(p.Repo, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			p := setup(t)
			args := op(p.Repo)
			bin, _ := exec.LookPath(args[0])
			if err := runWrapped(t, p, bin, args[1:]...); err == nil {
				t.Fatal("sandboxed operation succeeded")
			}
			if !exists(filepath.Join(p.Repo, ".git", "HEAD")) || !exists(filepath.Join(p.Repo, ".git", "hooks")) {
				t.Fatal(".git was changed")
			}
			// control: the same operation on a fresh repo succeeds unsandboxed
			q := setup(t)
			args = op(q.Repo)
			if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
				t.Fatalf("control failed: %v", err)
			}
		})
	}
}
