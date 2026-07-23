package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/codeindex"
	"gophermind/internal/tuning"
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

// suppressStream reports whether the in-flight turn's streamed output should be
// kept out of the transcript. True only for a /project interview turn, whose
// reply is a JSON control message; every other turn -- ordinary chat and the
// /project generation turn -- streams normally.
func (m model) suppressStream() bool {
	return m.projTurn && m.proj == projInterview
}
