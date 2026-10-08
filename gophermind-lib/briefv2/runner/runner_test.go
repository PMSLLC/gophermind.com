package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/sandbox"
)

func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func skipIfNoSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec not available: GOOS is " + runtime.GOOS)
	}
	if err := sandbox.Preflight(context.Background()); err != nil {
		t.Skip("sandbox-exec not available: " + err.Error())
	}
}

func skipIfNoGo(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	p, _ = filepath.EvalSymlinks(p)
	return p
}

func waitDead(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d is still alive after %v", pid, within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestRunCapturesCombinedOutput(t *testing.T) {
	r := New(Config{})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: Shell("echo a; echo b 1>&2"), Env: []string{}})
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("err=%v exit=%d", res.Err, res.ExitCode)
	}
	lines := res.Out.Lines(10)
	if len(lines) != 2 || (lines[0] != "a" && lines[0] != "b") {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(res.Out.Text(), "a\n") || !strings.Contains(res.Out.Text(), "b\n") {
		t.Fatalf("text = %q", res.Out.Text())
	}
}

func TestRunOutputCapKeepsHashOfEverything(t *testing.T) {
	r := New(Config{OutputCap: 10})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: Shell("i=0; while [ $i -lt 100 ]; do echo 123456789; i=$((i+1)); done"), Env: []string{}})
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("err=%v exit=%d", res.Err, res.ExitCode)
	}
	full := strings.Repeat("123456789\n", 100)
	sum := sha256.Sum256([]byte(full))
	if len(res.Out.Text()) != 10 || res.Out.Size() != 1000 || !res.Out.Truncated() || res.Out.SHA256() != hex.EncodeToString(sum[:]) {
		t.Fatalf("len=%d size=%d trunc=%v sha=%s", len(res.Out.Text()), res.Out.Size(), res.Out.Truncated(), res.Out.SHA256())
	}
}

func TestRunEnvIsExactlyGiven(t *testing.T) {
	t.Setenv("CANARY_PARENT_ENV", "leak")
	r := New(Config{})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: Shell("env"), Env: []string{"A=1"}})
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("err=%v exit=%d", res.Err, res.ExitCode)
	}
	allowed := map[string]bool{"PWD": true, "SHLVL": true, "_": true, "OLDPWD": true}
	seenA := false
	for _, l := range res.Out.Lines(100) {
		k, v, _ := strings.Cut(l, "=")
		if k == "A" && v == "1" {
			seenA = true
			continue
		}
		if !allowed[k] {
			t.Fatalf("unexpected variable %q in the child environment", k)
		}
	}
	if !seenA {
		t.Fatal("A=1 missing")
	}
	if strings.Contains(res.Out.Text(), "HOME=") || strings.Contains(res.Out.Text(), "PATH=") || strings.Contains(res.Out.Text(), "CANARY") {
		t.Fatal("parent environment leaked")
	}
}

func TestRunTimeoutKillsGroup(t *testing.T) {
	dir := realDir(t)
	r := New(Config{Grace: 300 * time.Millisecond})
	start := time.Now()
	res := r.Run(context.Background(), Spec{Dir: dir, Argv: Shell("sleep 60 & echo $! > pid; wait"), Env: []string{"PATH=/bin:/usr/bin"}, Timeout: 500 * time.Millisecond})
	if !res.TimedOut || res.Canceled {
		t.Fatalf("TimedOut=%v Canceled=%v", res.TimedOut, res.Canceled)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("run was not bounded by the timeout")
	}
	waitDead(t, readPid(t, filepath.Join(dir, "pid")), 2*time.Second)
}

func TestRunKillsLeftoversAfterCleanExit(t *testing.T) {
	dir := realDir(t)
	grace := 300 * time.Millisecond
	r := New(Config{Grace: grace})
	res := r.Run(context.Background(), Spec{Dir: dir, Argv: Shell("sleep 60 & echo $! > pid"), Env: []string{"PATH=/bin:/usr/bin"}})
	if res.Err != nil || res.ExitCode != 0 || res.TimedOut {
		t.Fatalf("err=%v exit=%d timedOut=%v", res.Err, res.ExitCode, res.TimedOut)
	}
	waitDead(t, readPid(t, filepath.Join(dir, "pid")), grace+time.Second)
}

func TestRunContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	r := New(Config{Grace: 300 * time.Millisecond})
	res := r.Run(ctx, Spec{Dir: realDir(t), Argv: Shell("sleep 60"), Env: []string{"PATH=/bin:/usr/bin"}, Timeout: time.Minute})
	if !res.Canceled || res.TimedOut {
		t.Fatalf("Canceled=%v TimedOut=%v", res.Canceled, res.TimedOut)
	}
}

func TestRunRefusesRelativeArgv(t *testing.T) {
	r := New(Config{})
	for _, argv := range [][]string{{"echo", "hi"}, {}, nil} {
		res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: argv, Env: []string{}})
		if res.Err == nil || res.ExitCode != -1 {
			t.Fatalf("argv %q: err=%v exit=%d", argv, res.Err, res.ExitCode)
		}
	}
}

func TestRunNoShellForArgv(t *testing.T) {
	r := New(Config{})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: []string{"/bin/echo", "$HOME;id"}, Env: []string{}})
	if res.Err != nil || strings.TrimSpace(res.Out.Text()) != "$HOME;id" {
		t.Fatalf("err=%v out=%q", res.Err, res.Out.Text())
	}
}

func TestRunMissingBinaryIsErrNotOutput(t *testing.T) {
	r := New(Config{})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: []string{"/nonexistent/CANARY-bin"}, Env: []string{}})
	if res.Err == nil || res.ExitCode != -1 || strings.Contains(res.Err.Error(), "CANARY") {
		t.Fatalf("err=%v exit=%d", res.Err, res.ExitCode)
	}
}

func TestOutputHasNoLeakingForms(t *testing.T) {
	r := New(Config{})
	res := r.Run(context.Background(), Spec{Dir: realDir(t), Argv: []string{"/bin/echo", "CANARY-secret"}, Env: []string{}})
	o := res.Out
	if !strings.Contains(o.Text(), "CANARY-secret") {
		t.Fatal("Text() must return the output")
	}
	s := fmt.Sprintf("%v %+v %#v %s %d", o, o, o, o, o)
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, _ := json.Marshal(struct{ Out Output }{o})
	for _, got := range []string{s, string(b), string(wrapped), fmt.Sprint(res.Out, &o)} {
		if strings.Contains(got, "CANARY") {
			t.Fatalf("leak in %q", got)
		}
	}
	if !strings.Contains(s, "14 bytes") || !strings.Contains(string(b), "14") {
		t.Fatalf("size missing: %q %q", s, b)
	}
}

func sandboxProfile(t *testing.T) sandbox.Profile {
	t.Helper()
	return sandbox.Profile{Repo: realDir(t), Scratch: realDir(t), GoCache: realDir(t)}
}

func TestRunSandboxedWriteDenied(t *testing.T) {
	skipIfNoSandbox(t)
	p := sandboxProfile(t)
	other := realDir(t)
	out := filepath.Join(other, "outside.txt")
	r := New(Config{Sandbox: &p})
	res := r.Run(context.Background(), Spec{Dir: p.Repo, Argv: Shell("echo x > " + out), Env: []string{"PATH=/bin:/usr/bin"}})
	if res.Err != nil || res.ExitCode == 0 {
		t.Fatalf("err=%v exit=%d", res.Err, res.ExitCode)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("sandboxed write outside the profile succeeded")
	}
	inside := filepath.Join(p.Repo, "inside.txt")
	res = r.Run(context.Background(), Spec{Dir: p.Repo, Argv: Shell("echo x > " + inside), Env: []string{"PATH=/bin:/usr/bin"}})
	if res.ExitCode != 0 {
		t.Fatalf("write inside the repo failed: %d", res.ExitCode)
	}
}

func TestRunSpecModCacheWritable(t *testing.T) {
	skipIfNoSandbox(t)
	p := sandboxProfile(t)
	p.GoModCache = realDir(t)
	f := filepath.Join(p.GoModCache, "f")
	r := New(Config{Sandbox: &p})
	spec := Spec{Dir: p.Repo, Argv: Shell("echo x > " + f), Env: []string{"PATH=/bin:/usr/bin"}}
	if res := r.Run(context.Background(), spec); res.Err != nil || res.ExitCode == 0 {
		t.Fatalf("read-only module cache accepted a write: err=%v exit=%d", res.Err, res.ExitCode)
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("file exists after a denied write")
	}
	spec.ModCacheWritable = true
	if res := r.Run(context.Background(), spec); res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("writable module cache refused a write: err=%v exit=%d", res.Err, res.ExitCode)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("file missing after a permitted write")
	}
	spec.ModCacheWritable = false
	_ = os.Remove(f)
	if res := r.Run(context.Background(), spec); res.ExitCode == 0 {
		t.Fatal("Config was changed by a ModCacheWritable call")
	}
	if p.ModCacheWritable || r.cfg.Sandbox.ModCacheWritable {
		t.Fatal("the caller's profile was mutated")
	}
}

func TestOutputWholeLinesDropsACutLine(t *testing.T) {
	w := &cappedWriter{limit: 12, h: sha256.New()}
	_, _ = w.Write([]byte("abc\ndefgh\nijklmnop\n"))
	o := w.output()
	if got := o.WholeLines(); got != "abc\ndefgh\n" || o.Text() != "abc\ndefgh\nij" {
		t.Errorf("WholeLines = %q, Text = %q", got, o.Text())
	}
}
