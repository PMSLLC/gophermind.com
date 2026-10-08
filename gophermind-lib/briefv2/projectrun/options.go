// Package projectrun is the single entry point behind /project: one brief in,
// a planned and built repo out, with no second command.
package projectrun

import (
	"io"
	"os"

	"gophermind/gophermind-lib/briefv2/report"
)

// Process exit codes of /project (spec section 6).
const (
	ExitVerified, ExitFailed, ExitInvalid, ExitWaiting, ExitEscalated, ExitInterrupted, ExitPreflight, ExitFault = 0, 1, 2, 3, 4, 5, 6, 7
)

// ExitCode maps a final status and stop reason to a process exit code.
func ExitCode(status, stopReason string) int {
	switch status {
	case "preflight_failed":
		return ExitPreflight
	case "harness_fault":
		return ExitFault
	case "invalid_brief":
		return ExitInvalid
	}
	return report.ExitCode(status, stopReason)
}

// Options is what one /project invocation was asked to do.
type Options struct {
	BriefPath          string
	Repo               string // --repo
	Attended           bool
	Resume             bool
	Graded             bool // --graded, implied by ExpectHead
	RequirePrivate     bool
	ExpectHead         string
	ExpectBinaryCommit string
	Generate           map[string]string // secret name -> "hex32" | "placeholder"
	PreflightOnly      bool
	PrintStatePaths    bool
	In                 *os.File  // nil unless attended
	Out, Err           io.Writer // Out: final report; Err: progress and preflight
}

// Result is how a run ended.
type Result struct {
	ExitCode           int
	Status, StopReason string
}
