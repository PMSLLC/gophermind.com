// This file backs the File menu's "Examples" item: a handful of sample
// brief files (gophermind-osx/examples/briefs/), the format the Pipeline
// panel's "Pick Brief..." reads, for someone who has never used the
// pipeline feature and doesn't know what to feed it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// findExamplesDir locates the examples/briefs directory, same search
// order and same bundle-vs-dev-layout reasoning as findServerBinary:
// GOPHERMIND_EXAMPLES override, then the .app bundle's Resources
// directory (build-app.sh copies examples/ there, sibling to
// gophermind-server -- see that script's own copy step), then the bare
// dev-checkout path relative to the current directory (running
// `./gophermind-osx` or `go run .` from within this module, the same way
// findServerBinary's dev-layout fallback assumes for its own sibling
// module).
func findExamplesDir() string {
	if p := os.Getenv("GOPHERMIND_EXAMPLES"); p != "" {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Join(dir, "examples", "briefs"),
			filepath.Join(dir, "..", "Resources", "examples", "briefs"),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	if info, err := os.Stat("examples/briefs"); err == nil && info.IsDir() {
		return "examples/briefs"
	}
	return ""
}

// openExamplesDir reveals the examples directory in Finder (the "links to
// a directory" behavior asked for), or reports where it looked if the
// directory can't be found -- e.g. a dev build run from an unexpected
// working directory.
func openExamplesDir(notify func(string)) {
	dir := findExamplesDir()
	if dir == "" {
		if notify != nil {
			notify("Examples folder not found (set GOPHERMIND_EXAMPLES, or run from the gophermind-osx checkout).")
		}
		return
	}
	if err := exec.Command("open", dir).Start(); err != nil {
		if notify != nil {
			notify(fmt.Sprintf("Could not open %s: %s", dir, err))
		}
	}
}
