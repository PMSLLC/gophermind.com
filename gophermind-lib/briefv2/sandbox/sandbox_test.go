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

// The target is a documentation-range address (RFC 5737) that no host answers, so
// the test needs no network: the sandbox denial is the "Operation not permitted"
// error from connect, distinct from the unsandboxed control's timeout or
// unreachable error. (An address of this machine's own interface counts as
// localhost to sandbox-exec and is deliberately not used.)
func TestSandboxDeniesNonLoopback(t *testing.T) {
	skipIfNoSandbox(t)
	const target, port = "192.0.2.1", "80"
	// nc's -w does not bound connect on macOS (the control can run ~75s), so cap it.
	cctx, ccancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer ccancel()
	ctl, _ := exec.CommandContext(cctx, "/usr/bin/nc", "-zv", "-w", "1", target, port).CombinedOutput()
	if strings.Contains(string(ctl), "Operation not permitted") {
		t.Skip("unsandboxed control is itself denied; the test cannot tell the sandbox apart")
	}
	cmd := exec.Command("/usr/bin/nc", "-zv", "-w", "1", target, port)
	w, err := Wrap(cmd, baseProfile(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.CombinedOutput()
	if err == nil {
		t.Fatal("sandboxed non-loopback connect succeeded")
	}
	if !strings.Contains(string(out), "Operation not permitted") {
		t.Fatalf("connect failed but not with a sandbox denial: %v", err)
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
	"net"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("bad")
	}
}

func TestNoExternal(t *testing.T) {
	c, err := net.DialTimeout("tcp", "192.0.2.1:80", time.Second)
	if err == nil {
		c.Close()
		t.Fatal("dial to a non-loopback address succeeded")
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
