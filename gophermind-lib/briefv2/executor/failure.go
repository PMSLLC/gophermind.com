package executor

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/runner"
)

// Failure classes (spec 7.2). failure_reason holds "<class>" or
// "<class>: <names>", never text. The classes runner produces (build, vet,
// test_fail, test_panic, test_timeout, no_tests_ran, malformed) are used from
// runner (runner.ClassBuild and so on) and are not redefined here; the
// executor adds the rest. ClassTimeout is the same word the runner uses for a
// build or vet stage that outlived its limit.
const (
	ClassRateLimited      = "rate_limited"
	ClassTimeout          = "timeout"
	ClassContextTooLong   = "context_too_long"
	ClassImportNotAllowed = "import_not_allowed"
	ClassForbiddenWrite   = "forbidden_write"
	ClassContractProblem  = "contract_problem"
	ClassNetworkCritical  = "network_critical"
	ClassIdentical        = "identical_reply"
)

const (
	maxReasonNames = 8  // names shown in one failure_reason
	maxNameBytes   = 80 // one name is clipped to this
)

// cleanName makes one name safe for a one-line reason: control characters and
// white space become "_" and the name is clipped.
func cleanName(n string) string {
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(n))
	if len(n) > maxNameBytes {
		n = strings.ToValidUTF8(n[:maxNameBytes], "?")
	}
	return n
}

// failureReason is "<class>" or "<class>: <name>, <name>": names sorted, at
// most 8 and then "(+n more)", one line. For a runner.Verdict use its Reason.
func failureReason(class string, names []string) string {
	var clean []string
	for _, n := range names {
		if c := cleanName(n); c != "" {
			clean = append(clean, c)
		}
	}
	if len(clean) == 0 {
		return class
	}
	sort.Strings(clean)
	more := 0
	if len(clean) > maxReasonNames {
		more = len(clean) - maxReasonNames
		clean = clean[:maxReasonNames]
	}
	s := class + ": " + strings.Join(clean, ", ")
	if more > 0 {
		s += fmt.Sprintf(" (+%d more)", more)
	}
	return s
}

// isLeafFailure reports that a verdict counts against the leaf. A harness
// fault or a cancellation does not: the model did nothing wrong, so neither
// consumes a fix attempt nor marks a leaf failed. A pass is not a failure.
func isLeafFailure(v runner.Verdict) bool { return !v.Pass() && !v.Faulted() }

// settle applies the definition of done to a verdict: a pass with no test
// event ran nothing and is no_tests_ran (spec R4). The runner already says so;
// this is the executor's own check of the same rule.
func settle(v runner.Verdict) runner.Verdict {
	if v.Pass() && v.Events < 1 {
		v.Class = runner.ClassNoTestsRan
	}
	return v
}

// failureOf is the previous failure a verdict gives the next prompt: the failed
// test names and the runner's output, redacted and capped by the packer. It is
// memory only; nothing here is persisted.
func failureOf(v runner.Verdict, secrets []string) packer.Failure {
	return packer.NewFailure(v.Names, v.Out.Text(), secrets)
}

// mergeFailures joins failures: names are the union in first-seen order (at
// most 8), lines are concatenated, and the packer's caps of 30 lines and 2 KB
// apply again to the whole. Secrets were removed when each failure was made.
func mergeFailures(fs ...packer.Failure) packer.Failure {
	var names, lines []string
	seen := map[string]bool{}
	for _, f := range fs {
		for _, n := range f.Names {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		lines = append(lines, f.Lines...)
	}
	return packer.NewFailure(names, strings.Join(lines, "\n"), nil)
}

// historyLine is one line of the history the revise prompt receives:
// "attempt 3 mini/qwen fail test_fail: TestX/a". Class and names only.
func historyLine(n int, a blackboard.Attempt) string {
	s := fmt.Sprintf("attempt %d %s/%s %s", n, a.Provider, a.Model, a.Verdict)
	if a.FailureReason != "" {
		s += " " + a.FailureReason
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}
