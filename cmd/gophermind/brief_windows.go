//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/report"
)

// The executor and `gophermind project` need process groups and a sandbox that
// only exist on unix (see gophermind-lib/build/runner), so on Windows they only
// say so. `brief plan` and the other brief subcommands still work.

const windowsUnsupported = "not available on Windows: the build runner needs a unix host"

// exit code 7 is exitFault in brief_run.go, which Windows does not compile.
const projectUsage = `  gophermind project <brief.md>   (not available on Windows)`

func briefRun(args []string, in *os.File, out, errw io.Writer) int {
	fmt.Fprintf(errw, "error: brief run is %s\n", windowsUnsupported)
	return 7
}

func briefReport(args []string, out, errw io.Writer) int {
	fmt.Fprintf(errw, "error: brief report is %s\n", windowsUnsupported)
	return 7
}

func runProject(args []string, in *os.File, out, errw io.Writer) int {
	fmt.Fprintf(errw, "error: project is %s\n", windowsUnsupported)
	return 7
}

func executorLines(ctx context.Context, board blackboard.Blackboard, rec planner.RunRecord, rep *report.Report) ([]string, error) {
	return nil, nil
}
