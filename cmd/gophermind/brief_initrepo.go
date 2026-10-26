package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// briefInitRepo is `gophermind brief init-repo <dir>`: a new git repository on
// branch main with one empty commit, for a run that builds in a tree of its
// own. The web server runs it as the run user (through sudo) so the run owns
// the tree. It refuses a directory that already exists. Not listed in the
// usage text.
func briefInitRepo(args []string, errw io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errw, "usage: gophermind brief init-repo <dir>")
		return 1
	}
	dir := args[0]
	if _, err := os.Lstat(dir); err == nil {
		fmt.Fprintln(errw, "gophermind brief init-repo: the directory already exists")
		return 1
	}
	if err := os.MkdirAll(dir, 0o775); err != nil {
		fmt.Fprintln(errw, "gophermind brief init-repo:", err)
		return 1
	}
	for _, g := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=GopherMind", "-c", "user.email=gophermind@localhost", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, g...)...)
		if out, err := c.CombinedOutput(); err != nil {
			fmt.Fprintf(errw, "gophermind brief init-repo: git %s: %v: %s\n", g[len(g)-1], err, out)
			_ = os.RemoveAll(dir)
			return 1
		}
	}
	return 0
}
