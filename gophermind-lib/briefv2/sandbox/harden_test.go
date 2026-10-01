package sandbox

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// outWrapped runs name under p and returns its combined output and error.
func outWrapped(t *testing.T, p Profile, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	w, err := Wrap(cmd, p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.CombinedOutput()
	return string(out), err
}

func outPlain(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// The control for each denied launcher is the same binary run unsandboxed with
// a harmless argument; the sandboxed run must not produce the same output.
func TestSandboxDeniesLauncherBinaries(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	// osascript evaluating arithmetic is harmless and prints "2".
	cout, cerr := outPlain("/usr/bin/osascript", "-e", "1+1")
	if cerr != nil || !strings.Contains(cout, "2") {
		t.Skipf("osascript control does not run here: %v", cerr)
	}
	out, err := outWrapped(t, p, "/usr/bin/osascript", "-e", "1+1")
	if err == nil || strings.Contains(out, "2") {
		t.Fatalf("sandboxed osascript ran: %v %q", err, out)
	}
	// open with an invalid option prints usage and launches nothing.
	cout, _ = outPlain("/usr/bin/open", "--gm-sbtest-bogus-option")
	if !strings.Contains(strings.ToLower(cout), "usage") && !strings.Contains(strings.ToLower(cout), "unrecognized") && !strings.Contains(strings.ToLower(cout), "unknown") {
		t.Skipf("open control prints no usage text: %q", cout)
	}
	out, err = outWrapped(t, p, "/usr/bin/open", "--gm-sbtest-bogus-option")
	if err == nil || strings.Contains(strings.ToLower(out), "usage") {
		t.Fatalf("sandboxed open executed: %v %q", err, out)
	}
	for _, bin := range []string{"/usr/bin/security", "/bin/launchctl", "/usr/bin/sudo", "/usr/bin/ssh", "/usr/bin/scp", "/usr/bin/nc", "/usr/bin/su", "/usr/bin/login"} {
		if _, e := os.Stat(bin); e != nil {
			continue
		}
		out, err := outWrapped(t, p, bin, "--gm-sbtest")
		if err == nil || !strings.Contains(strings.ToLower(out), "not permitted") && !strings.Contains(strings.ToLower(out), "denied") {
			t.Errorf("%s was not denied by the profile: %v %q", bin, err, out)
		}
	}
	// curl, go and git stay executable.
	for _, bin := range []string{"/usr/bin/curl", "/usr/bin/git"} {
		if _, e := os.Stat(bin); e != nil {
			continue
		}
		if err := runWrapped(t, p, bin, "--version"); err != nil {
			t.Errorf("%s must stay allowed: %v", bin, err)
		}
	}
}

func TestSandboxDeniesPasteboardAndLaunchServices(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	if _, err := outPlain("/usr/bin/pbpaste"); err != nil {
		t.Skipf("pbpaste control fails here: %v", err)
	}
	out, err := outWrapped(t, p, "/usr/bin/pbpaste")
	if err == nil && strings.TrimSpace(out) != "" {
		t.Fatalf("sandboxed pbpaste read the clipboard")
	}
	txt, e := p.Text()
	if e != nil {
		t.Fatal(e)
	}
	for _, rule := range []string{
		`(deny mach-lookup (global-name-prefix "com.apple.coreservices") (global-name-prefix "com.apple.lsd"))`,
		`(deny mach-lookup (global-name "com.apple.pasteboard.1"))`,
		`(global-name "com.apple.SecurityServer")`, `(global-name "com.apple.securityd")`, `(global-name "com.apple.security.agent")`,
		`(global-name "com.apple.coreservices.appleevents")`,
	} {
		if !strings.Contains(txt, rule) {
			t.Errorf("profile lacks %s", rule)
		}
	}
}

func TestSandboxSignalScope(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	helper := func() *exec.Cmd {
		c := exec.Command("/bin/sleep", "60")
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	alive := func(c *exec.Cmd) bool { return syscall.Kill(c.Process.Pid, 0) == nil }
	// control: an unsandboxed kill of an outside process works
	ctl := helper()
	if err := exec.Command("/bin/kill", "-TERM", strconv.Itoa(ctl.Process.Pid)).Run(); err != nil {
		t.Fatalf("control kill failed: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = ctl.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("control process did not die")
	}
	victim := helper()
	out, err := outWrapped(t, p, "/bin/kill", "-TERM", strconv.Itoa(victim.Process.Pid))
	if err == nil {
		t.Fatalf("sandboxed kill of an outside process succeeded: %q", out)
	}
	time.Sleep(200 * time.Millisecond)
	if !alive(victim) {
		t.Fatal("the outside process died")
	}
	// the sandbox's own children and group stay killable
	if err := runWrapped(t, p, "/bin/sh", "-c", "sleep 30 & kill -TERM $!; wait $! 2>/dev/null; sleep 30 & kill -KILL $!; true"); err != nil {
		t.Fatalf("killing its own child failed: %v", err)
	}
}

func listener(t *testing.T) (net.Listener, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen: " + err.Error())
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	n, _ := strconv.Atoi(portOf(l))
	return l, n
}

func TestSandboxLoopbackPortList(t *testing.T) {
	skipIfNoSandbox(t)
	_, listed := listener(t)
	_, other := listener(t)
	_, inRange := listener(t)
	const dial = "exec 3<>/dev/tcp/127.0.0.1/$1"
	connect := func(p Profile, port int) (string, error) {
		return outWrapped(t, p, "/bin/bash", "-c", dial, "bash", strconv.Itoa(port))
	}
	if out, err := outPlain("/bin/bash", "-c", dial, "bash", strconv.Itoa(other)); err != nil {
		t.Fatalf("unsandboxed control connect failed: %v %s", err, out)
	}
	p := baseProfile(t)
	p.LoopbackPorts = []int{listed}
	if out, err := connect(p, listed); err != nil {
		t.Fatalf("listed port refused: %v %s", err, out)
	}
	if out, err := connect(p, other); err == nil || !strings.Contains(out, "not permitted") {
		t.Fatalf("unlisted loopback port reachable: %v %s", err, out)
	}
	// no list at all: no loopback
	q := baseProfile(t)
	if _, err := connect(q, listed); err == nil {
		t.Fatal("empty list reached a loopback port")
	}
	// a range admits the ports inside it only
	r := baseProfile(t)
	r.LoopbackRange = [2]int{inRange, inRange + 2}
	if out, err := connect(r, inRange); err != nil {
		t.Fatalf("range port refused: %v %s", err, out)
	}
	if _, err := connect(r, other); err == nil && other != inRange && (other < inRange || other > inRange+2) {
		t.Fatal("port outside the range reachable")
	}
}

func TestProfileLoopbackInputs(t *testing.T) {
	bad := []struct {
		name string
		mod  func(*Profile)
		want string
	}{
		{"zero", func(p *Profile) { p.LoopbackPorts = []int{0} }, "LoopbackPorts"},
		{"negative", func(p *Profile) { p.LoopbackPorts = []int{-5} }, "LoopbackPorts"},
		{"big", func(p *Profile) { p.LoopbackPorts = []int{65536} }, "LoopbackPorts"},
		{"reversed", func(p *Profile) { p.LoopbackRange = [2]int{50010, 50000} }, "LoopbackRange"},
		{"range out", func(p *Profile) { p.LoopbackRange = [2]int{0, 70000} }, "LoopbackRange"},
		{"too wide", func(p *Profile) { p.LoopbackRange = [2]int{49152, 65535} }, "LoopbackRange"},
	}
	for _, c := range bad {
		p := baseProfile(t)
		c.mod(&p)
		_, err := p.Text()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		for _, d := range []string{"65536", "70000", "-5", "50010"} {
			if strings.Contains(err.Error(), d) {
				t.Errorf("%s: error leaks the value: %v", c.name, err)
			}
		}
	}
	p := baseProfile(t)
	p.LoopbackPorts = []int{55432, 8080, 55432}
	p.LoopbackRange = [2]int{50000, 50003}
	txt, err := p.Text()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(txt, `"localhost:*"`) && strings.Contains(txt, "(remote ip \"localhost:*\")") {
		t.Fatal("outbound loopback is still open to every port")
	}
	for _, want := range []string{`"localhost:55432"`, `"localhost:8080"`, `"localhost:50000"`, `"localhost:50003"`} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(txt, `"localhost:55432"`) != 1 {
		t.Error("duplicate port was not collapsed")
	}
}

func TestSandboxDeniesSecretFileReads(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	p.Home = realDir(t)       // any home: repo reads are re-allowed, the denies must still win
	files := map[string]bool{ // true = must be unreadable
		".env": true, ".env.local": true, "sub/.env": true, "sub/.env.production": true,
		"cert.pem": true, "sub/server.key": true, "id_rsa": true, "id_rsa.pub": true, "sub/id_rsa_work": true,
		".git/config": true,
		"env.txt":     false, "environment.go": false, ".envrc": false, "monkey": false, "notes.pem.md": false,
		"keys.go": false, "id_ed25519": false, ".git/HEAD": false,
	}
	for name := range files {
		f := filepath.Join(p.Repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("canary"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, home := range []string{p.Home, ""} {
		q := p
		q.Home = home
		for name, denied := range files {
			f := filepath.Join(p.Repo, filepath.FromSlash(name))
			if out, err := outPlain("/bin/cat", f); err != nil {
				t.Fatalf("control read of %s failed: %v %s", name, err, out)
			}
			out, err := outWrapped(t, q, "/bin/cat", f)
			if denied && (err == nil || strings.Contains(out, "canary")) {
				t.Errorf("home=%q: read of %s was allowed", home, name)
			}
			if !denied && (err != nil || !strings.Contains(out, "canary")) {
				t.Errorf("home=%q: read of %s was refused: %v", home, name, err)
			}
		}
	}
	// a same-named file outside the repo is not touched by the rule
	other := filepath.Join(realDir(t), ".env")
	_ = os.WriteFile(other, []byte("canary"), 0o644)
	if err := runWrapped(t, p, "/bin/cat", other); err != nil {
		t.Errorf("a .env outside the repo is unreadable without a home rule hit: %v", err)
	}
}

func TestSandboxDeniesGophermindSiblingWrites(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	gm := filepath.Join(p.Repo, ".gophermind")
	p.RunDir = filepath.Join(gm, "gm-mine")
	other := filepath.Join(gm, "gm-other")
	for _, d := range []string{p.RunDir, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{filepath.Join(other, "x.txt"), filepath.Join(gm, "top.txt"), filepath.Join(p.RunDir, "y.txt")} {
		if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", target); err == nil || exists(target) {
			t.Errorf("sandboxed write to %s succeeded", filepath.Base(target))
		}
		if err := exec.Command("/bin/sh", "-c", "echo x > \"$1\"", "sh", target).Run(); err != nil {
			t.Errorf("control write failed: %v", err)
		}
		_ = os.Remove(target)
	}
	// a scratch directory inside .gophermind stays writable
	p.Scratch = filepath.Join(gm, "scratch-mine")
	if err := os.MkdirAll(p.Scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p.Scratch, "ok.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", f); err != nil || !exists(f) {
		t.Fatalf("scratch inside .gophermind is not writable: %v", err)
	}
	// and the normal repo is still writable
	g := filepath.Join(p.Repo, "fine.txt")
	if err := runWrapped(t, p, "/bin/sh", "-c", "echo x > \"$1\"", "sh", g); err != nil || !exists(g) {
		t.Fatalf("repo write refused: %v", err)
	}
}

func TestSandboxKillsStillWork(t *testing.T) {
	skipIfNoSandbox(t)
	p := baseProfile(t)
	cmd := exec.Command("/bin/sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	w, err := Wrap(cmd, p)
	if err != nil {
		t.Fatal(err)
	}
	w.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Wait() }()
	time.Sleep(300 * time.Millisecond)
	_ = syscall.Kill(-w.Process.Pid, syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the process group kill did not end the sandboxed child")
	}
}
