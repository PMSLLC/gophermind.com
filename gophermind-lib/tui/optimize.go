package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/codeindex"
	"gophermind/gophermind-lib/secaudit"
	"gophermind/gophermind-lib/tuning"
)

// handleIndexCommand implements "/index": rebuild INDEX.md on demand. The same
// refresh runs automatically at startup and after each executed task.
func (m model) handleIndexCommand() model {
	root, err := os.Getwd()
	if err != nil {
		m.appendLine("index: " + err.Error())
		m.sync()
		return m
	}
	n, err := codeindex.BuildAndWrite(root)
	if err != nil {
		m.appendLine("index: " + err.Error())
		m.sync()
		return m
	}
	m.appendLine(fmt.Sprintf("index: wrote %s (%d symbols)", codeindex.FileName, n))
	m.sync()
	return m
}

// handleOptimizeCommand implements "/optimize [profile]": write the tuned
// settings for a named profile into the project's .env.
//
// It deliberately does NOT mutate the running session's config. A .env is read
// at startup, so the honest thing is to write the file and say the settings
// apply next launch, rather than half-applying some of them now.
func (m model) handleOptimizeCommand(text string) model {
	name := "aggressive"
	if fields := strings.Fields(text); len(fields) > 1 {
		name = strings.ToLower(fields[1])
	}

	prof, ok := tuning.Lookup(name)
	if !ok {
		m.appendLine(fmt.Sprintf("optimize: unknown profile %q. Known profiles:\n%s", name, tuning.Describe()))
		m.sync()
		return m
	}

	root, err := os.Getwd()
	if err != nil {
		m.appendLine("optimize: " + err.Error())
		m.sync()
		return m
	}
	envPath := filepath.Join(root, ".env")
	n, err := tuning.MergeIntoEnvFile(envPath, tuning.Settings(prof, tuning.DefaultProbe()))
	if err != nil {
		m.appendLine("optimize: " + err.Error())
		m.sync()
		return m
	}

	m.appendLine(projectBannerStyle.Render(
		fmt.Sprintf("optimize: applied “%s” — %d setting(s) written to .env", prof.Name, n)))
	m.appendLine(prof.Description)
	m.appendLine("Existing values (endpoint, model, credentials) were preserved. Settings take effect on next start.")
	if prof.AutoApprove {
		m.appendLine("⚠ this profile sets GOPHERMIND_APPROVAL=auto — tools will run with no approval gate")
	}
	m.sync()
	return m
}

// handleSecAuditCommand implements "/secaudit [path]": a static-only security
// scan over the given path (default the working directory), writing
// SECURITY-AUDIT.md and printing a summary. It runs static-only so it returns
// promptly without a model round-trip; the `gophermind secaudit` CLI does the
// full scan-plus-verification.
func (m model) handleSecAuditCommand(text string) model {
	root, err := os.Getwd()
	if err != nil {
		m.appendLine("secaudit: " + err.Error())
		m.sync()
		return m
	}
	if fields := strings.Fields(text); len(fields) > 1 {
		root = fields[1]
	}

	report, err := secaudit.Audit(context.Background(), root, secaudit.Options{})
	if err != nil {
		m.appendLine("secaudit: " + err.Error())
		m.sync()
		return m
	}
	out := filepath.Join(root, secaudit.ReportFileName)
	if err := secaudit.WriteReport(report, out); err != nil {
		m.appendLine("secaudit: write report: " + err.Error())
		m.sync()
		return m
	}

	counts := report.Counts()
	m.appendLine(projectBannerStyle.Render(fmt.Sprintf(
		"secaudit: %d finding(s) — %d critical, %d high, %d medium, %d low",
		len(report.Findings), counts[secaudit.Critical], counts[secaudit.High],
		counts[secaudit.Medium], counts[secaudit.Low])))
	m.appendLine("report written to " + secaudit.ReportFileName + " (static-only; run `gophermind secaudit` for model verification)")
	m.appendLine("note: a clean run means nothing was found by these checks, not that the code is secure.")
	m.sync()
	return m
}
