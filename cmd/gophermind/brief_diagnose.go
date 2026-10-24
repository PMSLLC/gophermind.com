package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/build/diagnose"
	"gophermind/gophermind-lib/build/planner"
)

// briefDiagnose implements `gophermind brief diagnose <run-id> [--repo <path>] [--json]`.
// It reads the run folder and changes nothing. The folder comes from the run
// record, like status and report, or from <repo>/.gophermind/<run-id> when
// --repo is given (a run folder on a machine that has no record of the run).
func briefDiagnose(args []string, out, errw io.Writer) int {
	var asJSON bool
	var repo string
	// The run id may come first or last; the flags are parsed around it.
	// (parseRunArgs lives in brief_run.go, which Windows does not compile.)
	fs := flag.NewFlagSet("brief diagnose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&asJSON, "json", false, "print the diagnosis as one JSON object")
	fs.StringVar(&repo, "repo", "", "the repository holding .gophermind/<run-id>")
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		id = ""
	} else if id == "" && fs.NArg() == 1 {
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		id = ""
	}
	if id == "" {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}
	var dir string
	if repo != "" {
		if !planner.ValidRunID(id) {
			fmt.Fprintf(errw, "error: %q is not a run id (want gm-YYYY-MM-DD-NNN)\n", id)
			return exitError
		}
		abs, err := filepath.Abs(repo)
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		dir = filepath.Join(abs, ".gophermind", id)
	} else {
		rec, err := planner.LookupRun(id)
		if err != nil {
			fmt.Fprintf(errw, "error: %v (use --repo <path> to name the repository)\n", err)
			return exitError
		}
		dir = rec.RunDir
	}
	d, err := diagnose.Run(context.Background(), id, dir)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(d); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		return exitDone
	}
	fmt.Fprint(out, d.Text())
	return exitDone
}
