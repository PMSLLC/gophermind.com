// Package gitenv builds git commands that cannot be redirected by an
// inherited git environment.
//
// exec.Command("git", ...) with only cmd.Dir set still inherits the parent
// process's environment. Git reads GIT_DIR (and GIT_INDEX_FILE) from that
// environment, and when either is set, git operates on the repository they
// name instead of the one found by walking up from cmd.Dir. Every git hook
// (pre-push, pre-commit, and so on) exports GIT_DIR for the invoking
// repository, so any process that shells out to git from inside a hook, a
// test suite, doctor fix, a tool call, inherits it whether it wants to or
// not.
//
// This is not a theoretical risk. On 2026-09-10, this repository's own test
// suite ran from a pre-push hook with GIT_DIR pointed at this repository.
// Code that built git commands with only cmd.Dir set ignored the temp
// directory it meant to operate on and instead:
//
//   - ran "git init" against the real repository (GIT_DIR set,
//     GIT_WORK_TREE unset), which reinitialized it as bare
//     (core.bare=true), after which every ordinary git command failed with
//     "this operation must be run in a work tree";
//   - ran "git commit", which wrote three test-fixture commits onto a real
//     branch, one of which deleted 803 files.
//
// Both were recovered by hand. Command and CommandContext are the fix: they
// strip every GIT_* variable from the child process's environment, so
// cmd.Dir is the only thing that decides which repository a git command
// touches. Do not remove this stripping; it is load-bearing, not paranoia.
package gitenv

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// SanitizedEnv returns os.Environ() with every GIT_* variable removed, for
// callers that must build the command themselves.
func SanitizedEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Command returns a git command rooted at dir with every GIT_* variable
// stripped from its environment, so an inherited GIT_DIR cannot redirect it
// away from dir.
func Command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = SanitizedEnv()
	return cmd
}

// CommandContext is Command with a context.
func CommandContext(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = SanitizedEnv()
	return cmd
}
