package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"gophermind/gophermind-lib/briefv2/projectrun"
)

const projectUsage = `  gophermind project <brief.md> [--repo <path>] [--generate NAME=hex32|placeholder]... [--require-private]
                      [--expect-head <rev>] [--graded] [--expect-binary-commit <sha>] [--resume] [--attended]
                      [--preflight-only] [--print-state-paths]
                                plan and build a v2 brief in one run (same as /project in the TUI)`

// projectEnv is nil in production; a test replaces it with an Env of fakes.
var projectEnv func() projectrun.Env

// runProject implements `gophermind project <brief-path> [flags]` and returns
// the process exit code (projectrun.Run's, or 1 for a bad command line).
func runProject(args []string, in *os.File, out, errw io.Writer) int {
	o, err := projectrun.ParseArgs(args, true)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\nusage:\n%s\n", err, projectUsage)
		return 1
	}
	if o.Attended {
		o.In = in
	}
	o.Out, o.Err = out, errw
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env := projectrun.DefaultEnv()
	if projectEnv != nil {
		env = projectEnv()
	}
	return projectrun.Run(ctx, o, env).ExitCode
}
