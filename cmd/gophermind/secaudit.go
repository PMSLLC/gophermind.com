package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gophermind/internal/llm"
	"gophermind/internal/secaudit"
)

// secAuditOptions is the parsed form of the secaudit subcommand's arguments.
type secAuditOptions struct {
	path       string
	staticOnly bool
	out        string
	failOn     secaudit.Severity
}

// parseSecAuditArgs parses "secaudit <path> [--static-only] [--out FILE]
// [--fail-on SEVERITY]". The path defaults to ".", the report to
// <path>/SECURITY-AUDIT.md, and the fail threshold to high.
func parseSecAuditArgs(args []string) (secAuditOptions, error) {
	o := secAuditOptions{path: ".", failOn: secaudit.High}
	pathSet := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--static-only":
			o.staticOnly = true
		case a == "--out":
			if i+1 >= len(args) {
				return o, fmt.Errorf("--out needs a file path")
			}
			i++
			o.out = args[i]
		case a == "--fail-on":
			if i+1 >= len(args) {
				return o, fmt.Errorf("--fail-on needs a severity")
			}
			i++
			sev, ok := secaudit.ParseSeverity(args[i])
			if !ok {
				return o, fmt.Errorf("unknown severity %q (want critical|high|medium|low|info)", args[i])
			}
			o.failOn = sev
		case strings.HasPrefix(a, "--"):
			return o, fmt.Errorf("unknown flag %q", a)
		default:
			if pathSet {
				return o, fmt.Errorf("unexpected argument %q", a)
			}
			o.path = a
			pathSet = true
		}
	}
	if o.out == "" {
		o.out = filepath.Join(o.path, secaudit.ReportFileName)
	}
	return o, nil
}

// runSecAudit executes the security bug-hunt. It returns an error when findings
// meet the fail threshold, so a shell/CI treats it as a failure.
func runSecAudit(ctx context.Context, client *llm.Client, args []string) error {
	o, err := parseSecAuditArgs(args)
	if err != nil {
		return err
	}

	opts := secaudit.Options{}
	if !o.staticOnly && client != nil {
		opts.Reviewer = secaudit.ClientReviewer{Client: client}
	}

	report, err := secaudit.Audit(ctx, o.path, opts)
	if err != nil {
		return fmt.Errorf("secaudit: %w", err)
	}
	if err := secaudit.WriteReport(report, o.out); err != nil {
		return fmt.Errorf("secaudit: write report: %w", err)
	}

	printSecAuditSummary(report, o)

	if report.MeetsThreshold(o.failOn) {
		return fmt.Errorf("secaudit: findings at or above %s severity — see %s", o.failOn, o.out)
	}
	return nil
}

// printSecAuditSummary writes a short human summary to stdout.
func printSecAuditSummary(report *secaudit.Report, o secAuditOptions) {
	counts := report.Counts()
	total := len(report.Findings)
	fmt.Printf("secaudit: %d finding(s) in %s", total, o.path)
	if total > 0 {
		parts := []string{}
		for _, sev := range []secaudit.Severity{secaudit.Critical, secaudit.High, secaudit.Medium, secaudit.Low, secaudit.Info} {
			if n := counts[sev]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, sev))
			}
		}
		fmt.Printf(" (%s)", strings.Join(parts, ", "))
	}
	fmt.Printf("\nreport: %s\n", o.out)
	if o.staticOnly {
		fmt.Println("(static-only: model verification skipped)")
	}
	fmt.Println("note: a clean run means nothing was found by these checks, not that the code is secure.")
}
