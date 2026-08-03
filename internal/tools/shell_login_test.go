package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// run_shell must not start a login shell: sourcing ~/.bash_profile on every
// call re-runs whatever the user put there (a conda init hook forks a Python
// interpreter), which costs seconds and several processes per tool call.
func TestRunShellIsNotALoginShell(t *testing.T) {
	sh := RunShell(t.TempDir(), 30*time.Second)
	// The result is reported as a status code rather than a word, so the
	// assertion cannot accidentally match the command echoed back above it.
	out, err := sh.Run(context.Background(), args(t, map[string]string{
		"command": `shopt -q login_shell; echo "loginflag=$?"`,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "loginflag=0") {
		t.Errorf("run_shell spawned a login shell; profile is sourced per call: %q", out)
	}
	if !strings.Contains(out, "loginflag=1") {
		t.Errorf("unexpected output: %q", out)
	}
}

// Dropping the login shell must not cost the user their profile PATH: conda,
// ~/.lmstudio/bin and anything else ~/.bash_profile adds still have to be
// reachable from tool calls.
func TestRunShellKeepsLoginPath(t *testing.T) {
	want := loginShellPath()
	if want == "" {
		t.Skip("no login shell PATH available in this environment")
	}

	sh := RunShell(t.TempDir(), 30*time.Second)
	out, err := sh.Run(context.Background(), args(t, map[string]string{
		"command": `printf %s "$PATH"`,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, dir := range strings.Split(want, ":") {
		if dir == "" {
			continue
		}
		if !strings.Contains(out, dir) {
			t.Errorf("run_shell PATH is missing login-shell entry %q\ngot: %s", dir, out)
		}
	}
}
