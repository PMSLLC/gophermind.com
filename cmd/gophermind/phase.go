package main

import (
	"fmt"
	"io"

	"gophermind/gophermind-lib/tui"
)

// runPhaseRemoved is what `gophermind phase ...` does since PhaseFlow's roadmap
// workflow was removed: print the notice (shared with the TUI's /phase) and
// return exit code 2. The stub exists for one release.
func runPhaseRemoved(w io.Writer) int {
	fmt.Fprintln(w, tui.PhaseRemovedNotice())
	return 2
}
