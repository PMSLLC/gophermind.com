package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// run_shell must not hand GIT_* variables to the command it runs.
//
// This is not hypothetical. An inherited GIT_DIR once redirected a `git`
// subprocess away from the directory it had been pointed at and onto a
// different repository entirely, producing commits that deleted 803 files
// and leaving core.bare set on a working repo. internal/gitenv fixed that
// for git commands this codebase builds itself, but run_shell is the path
// an agent actually runs `git init` and `git commit` through, and it was
// still passing the whole parent environment straight to bash.
//
// The variables below are the ones that redirect git at another repository
// or another index; each is asserted separately so a partial filter fails
// loudly rather than passing on the first name that happens to be covered.
func TestRunShellStripsGitEnv(t *testing.T) {
	vars := []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CEILING_DIRECTORIES",
		"GIT_CONFIG",
		"GIT_CONFIG_GLOBAL",
	}
	for _, v := range vars {
		t.Setenv(v, "/tmp/somewhere-else")
	}

	sh := RunShell(t.TempDir(), 30*time.Second)
	for _, v := range vars {
		out, err := sh.Run(context.Background(), args(t, map[string]string{
			"command": `printf '%s=[%s]\n' ` + v + ` "$` + v + `"`,
		}))
		if err != nil {
			t.Fatalf("run %s: %v", v, err)
		}
		if !strings.Contains(out, v+"=[]") {
			t.Errorf("%s reached the command; an inherited git variable can "+
				"redirect an agent's git call at the wrong repository: %q", v, out)
		}
	}
}

// RunShellEnhanced, not RunShell, is the tool actually registered in the CLI
// and the desktop app (cmd/gophermind/main.go and desktop/deps.go), so it is
// the one an agent's `git init` really travels through. It must strip the
// same variables.
func TestRunShellEnhancedStripsGitEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/somewhere-else")
	t.Setenv("GIT_WORK_TREE", "/tmp/somewhere-else")

	sh := RunShellEnhanced(t.TempDir(), 30*time.Second, ShellLimits{})
	out, err := sh.Run(context.Background(), args(t, map[string]string{
		"command": `printf 'dir=[%s] tree=[%s]\n' "$GIT_DIR" "$GIT_WORK_TREE"`,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "dir=[] tree=[]") {
		t.Errorf("git variables reached the enhanced shell, the one production "+
			"registers: %q", out)
	}
}

// env_allow_list is a model-supplied parameter. Naming a GIT_* variable in it
// must not put that variable back: an allow-list the caller controls is not a
// reason to re-enable the exact redirection the default path strips.
func TestRunShellEnhancedAllowListCannotReaddGitEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/somewhere-else")
	t.Setenv("GOPHERMIND_TEST_CANARY", "present")

	sh := RunShellEnhanced(t.TempDir(), 30*time.Second, ShellLimits{})
	out, err := sh.Run(context.Background(), []byte(
		`{"command":"printf 'dir=[%s] canary=[%s]\\n' \"$GIT_DIR\" \"$GOPHERMIND_TEST_CANARY\"",`+
			`"env_allow_list":["GIT_DIR","GOPHERMIND_TEST_CANARY"]}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "dir=[]") {
		t.Errorf("env_allow_list re-added GIT_DIR; a model-chosen list must not "+
			"reinstate a git redirection: %q", out)
	}
	if !strings.Contains(out, "canary=[present]") {
		t.Errorf("env_allow_list stopped working for ordinary variables: %q", out)
	}
}

// Stripping GIT_* must not strip anything else: the user's toolchain still
// has to be reachable, which is the whole reason run_shell passes an
// environment through in the first place.
func TestRunShellKeepsNonGitEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/somewhere-else")
	t.Setenv("GOPHERMIND_TEST_CANARY", "present")

	sh := RunShell(t.TempDir(), 30*time.Second)
	out, err := sh.Run(context.Background(), args(t, map[string]string{
		"command": `printf 'canary=[%s]\n' "$GOPHERMIND_TEST_CANARY"`,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "canary=[present]") {
		t.Errorf("a non-git variable was dropped along with the git ones: %q", out)
	}
}
