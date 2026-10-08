package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/planner"
)

// briefAnswer implements `gophermind brief answer <run-id> <question-id> <new answer...>`.
func briefAnswer(args []string, out, errw io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(errw, briefUsage)
		return 1
	}
	rep, err := planner.ChangeAnswer(args[0], args[1], strings.Join(args[2:], " "), time.Now())
	if err != nil {
		fmt.Fprintln(errw, "gophermind brief answer:", err)
		return 1
	}
	if rep.Reset == "contract" {
		fmt.Fprintf(out, "Answer changed. The plan restarts from the Contract stage (%d file(s) removed).\n", len(rep.Removed))
	} else {
		fmt.Fprintf(out, "Answer changed. Component %s is planned again (coverage and approval are removed).\n", rep.Reset)
	}
	fmt.Fprintf(out, "Run `gophermind brief resume %s` to continue.\n", args[0])
	return 0
}
