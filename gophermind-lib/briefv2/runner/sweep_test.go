package runner

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/sandbox"
)

// setsidSleeper is a shell command that starts a sleeper in its own session
// (so the process-group kill cannot reach it), prints its pid and then runs tail.
func setsidSleeper(tail string) string {
	return `/usr/bin/perl -MPOSIX -e 'POSIX::setsid(); exec "/bin/sleep", "61"' & echo $!; ` + tail
}

func pidFromOutput(t *testing.T, res Result) int {
	t.Helper()
	f := strings.Fields(res.Out.Text())
	if len(f) == 0 {
		t.Fatalf("no pid in output: %q", res.Out.Text())
	}
	pid, err := strconv.Atoi(f[0])
	if err != nil || pid < 2 {
		t.Fatalf("bad pid %q", f[0])
	}
	return pid
}

func sweepRunners(t *testing.T) map[string]*Runner {
	t.Helper()
	if _, err := os.Stat("/usr/bin/perl"); err != nil {
		t.Skip("perl is not installed")
	}
	rs := map[string]*Runner{"plain": New(Config{Grace: time.Second})}
	if runtime.GOOS == "darwin" && sandbox.Preflight(context.Background()) == nil {
		p := sandbox.Profile{Repo: realDir(t), Scratch: realDir(t), GoCache: realDir(t)}
		rs["sandboxed"] = New(Config{Sandbox: &p, Grace: time.Second})
	}
	return rs
}

func TestRunTimeoutKillsSetsidDescendant(t *testing.T) {
	for name, r := range sweepRunners(t) {
		t.Run(name, func(t *testing.T) {
			bystander := exec.Command("/bin/sleep", "61")
			bystander.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := bystander.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = bystander.Process.Kill(); _ = bystander.Wait() }()
			res := r.Run(context.Background(), Spec{Dir: os.TempDir(), Argv: Shell(setsidSleeper("sleep 60")),
				Env: []string{"PATH=/bin:/usr/bin"}, Timeout: 1500 * time.Millisecond})
			if !res.TimedOut {
				t.Fatalf("not timed out: %+v", res)
			}
			waitDead(t, pidFromOutput(t, res), 3*time.Second)
			if len(res.Warnings) != 0 {
				t.Fatalf("warnings: %v", res.Warnings)
			}
			if err := syscall.Kill(bystander.Process.Pid, 0); err != nil {
				t.Fatal("the sweep killed an unrelated process")
			}
		})
	}
}

func TestRunNormalExitKillsSetsidDescendant(t *testing.T) {
	for name, r := range sweepRunners(t) {
		t.Run(name, func(t *testing.T) {
			res := r.Run(context.Background(), Spec{Dir: os.TempDir(), Argv: Shell(setsidSleeper("sleep 1")),
				Env: []string{"PATH=/bin:/usr/bin"}})
			if res.ExitCode != 0 {
				t.Fatalf("exit %d", res.ExitCode)
			}
			waitDead(t, pidFromOutput(t, res), 3*time.Second)
		})
	}
}

func TestRunCancelKillsSetsidDescendant(t *testing.T) {
	r := sweepRunners(t)["plain"]
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(1500 * time.Millisecond); cancel() }()
	res := r.Run(ctx, Spec{Dir: os.TempDir(), Argv: Shell(setsidSleeper("sleep 60")), Env: []string{"PATH=/bin:/usr/bin"}})
	if !res.Canceled {
		t.Fatal("not canceled")
	}
	waitDead(t, pidFromOutput(t, res), 3*time.Second)
}

func TestRunSweepWarnsWhenADescendantSurvives(t *testing.T) {
	r := sweepRunners(t)["plain"]
	old := killProc
	killProc = func(pid int) error { return nil } // a process that cannot be killed
	defer func() { killProc = old }()
	var spared int
	res := r.Run(context.Background(), Spec{Dir: os.TempDir(), Argv: Shell(setsidSleeper("sleep 60")),
		Env: []string{"PATH=/bin:/usr/bin"}, Timeout: 1500 * time.Millisecond})
	spared = pidFromOutput(t, res)
	defer func() { _ = syscall.Kill(spared, syscall.SIGKILL) }()
	if len(res.Warnings) != 1 || strings.Contains(res.Warnings[0], strconv.Itoa(spared)) {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}
