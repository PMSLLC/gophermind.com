package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dispatchBinPath is the compiled gophermind binary the tests in this file
// drive as a subprocess. run() is not safely callable in-process: it parses
// the package-level flag.CommandLine (which can only be parsed once per
// process) and calls os.Exit directly for "gophermind free ...", which would
// kill the test binary. The only seam that exercises the real
// argv -> flag.Parse -> cmd dispatch path inside run() -- rather than just
// runFree() in isolation, which cannot see a bug in how run() slices argv --
// is to build the actual binary and invoke it the way a user does.
var dispatchBinPath string

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}

	dir, err := os.MkdirTemp("", "gophermind-dispatch-test")
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "gophermind")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = wd
	if out, err := build.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	dispatchBinPath = bin

	os.Exit(m.Run())
}

// dispatchEnv builds a clean environment for the subprocess: PATH plus an
// isolated HOME, and no inherited GOPHERMIND_* vars, so the test's behavior
// does not depend on the developer's real config, profile, or API keys.
func dispatchEnv(t *testing.T) []string {
	t.Helper()
	env := []string{"HOME=" + t.TempDir()}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return env
}

func runDispatch(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(dispatchBinPath, args...)
	cmd.Env = dispatchEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gophermind %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestFreeDispatchSurvivesLeadingGlobalFlags is the regression test for the
// bug in run(): "gophermind free ..." used to dispatch on os.Args[2:]
// instead of the post-flag positional args every sibling subcommand uses, so
// a global flag preceding "free" (-q, --profile X, ...) shifted the
// subcommand off by one and free.go reported "free" (or the flag's value)
// as an unknown subcommand instead of running "list". runFree()'s own tests
// cannot catch this: they call runFree(args, out) directly and never go
// through flag parsing, so this drives the compiled binary instead.
func TestFreeDispatchSurvivesLeadingGlobalFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no leading flag", []string{"free", "list"}},
		{"leading -q", []string{"-q", "free", "list"}},
		{"leading --profile", []string{"--profile", "free-groq", "free", "list"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runDispatch(t, tc.args...)
			if strings.Contains(out, "unknown subcommand") {
				t.Errorf("gophermind %s misdispatched:\n%s", strings.Join(tc.args, " "), out)
			}
			if !strings.Contains(out, "PROFILE") {
				t.Errorf("gophermind %s did not print the provider table:\n%s", strings.Join(tc.args, " "), out)
			}
		})
	}
}
