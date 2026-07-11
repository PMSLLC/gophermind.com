package tui

import (
	"os"
	"strconv"
	"strings"

	"gophermind/internal/config"
	"gophermind/internal/phaseflow"
)

// handlePhaseCommand parses a "/phase ..." slash command. It returns exactly one
// non-empty result: reply is transcript text for a locally-served state command
// (or an error message), and agentTask is a state-seeded prompt for an agentic
// loop step that the caller should send to the agent. The project root is the
// current working directory, where the TUI was launched.
func (m *model) handlePhaseCommand(full string) (reply string, agentTask string) {
	root, err := os.Getwd()
	if err != nil {
		return "phase: cannot determine working directory: " + err.Error(), ""
	}
	e := phaseflow.New(root)

	fields := strings.Fields(full)
	sub := "help"
	if len(fields) > 1 {
		sub = strings.ToLower(fields[1])
	}
	rest := ""
	if len(fields) > 2 {
		rest = strings.TrimSpace(strings.Join(fields[2:], " "))
	}

	switch sub {
	case "init":
		if rest == "" {
			return "usage: /phase init <project-name>", ""
		}
		if err := e.Init(rest); err != nil {
			return "phase: " + err.Error(), ""
		}
		return "✓ initialized PhaseFlow project " + strconv.Quote(rest) + " under " + phaseflow.PlanningDirName + "/ — next: /phase roadmap", ""

	case "status", "progress":
		out, err := e.Status()
		if err != nil {
			return "phase: " + err.Error(), ""
		}
		return out, ""

	case "next":
		p, err := e.Next()
		if err != nil {
			return "phase: " + err.Error(), ""
		}
		if p == nil {
			return "All phases complete — /phase milestone to ship.", ""
		}
		return "Next: Phase " + p.Number.String() + " — " + p.Name, ""

	case "commands", "list":
		return strings.Join(phaseflow.CommandNames(), "  "), ""

	case "help", "":
		return phaseSlashHelp, ""

	case "roadmap", "plan", "execute", "verify", "milestone":
		prompt, err := e.BuildStepPrompt(sub, rest)
		if err != nil {
			return "phase: " + err.Error(), ""
		}
		return "", prompt

	default:
		// Any other embedded PhaseFlow command runs agentically by name.
		if _, ok := phaseflow.Command(sub); ok {
			prompt, err := e.BuildCommandPrompt(sub, rest)
			if err != nil {
				return "phase: " + err.Error(), ""
			}
			return "", prompt
		}
		return "unknown phase command " + strconv.Quote(sub) + "\n" + phaseSlashHelp, ""
	}
}

// phaseSlashHelp is shown for "/phase" and "/phase help".
const phaseSlashHelp = "PhaseFlow: /phase init <name> · status · next · commands · " +
	"roadmap · plan <n> · execute <n> · verify <n> · milestone"

// handleSamplingCommand parses and applies the /temp and /topp slash commands.
// The argument is untrusted user input: it is parsed as a float and validated
// against the configured range before it is applied. Any parse or range error
// is echoed to the transcript and leaves the current setting unchanged — it
// never reaches the API and never panics. The cmd argument is the already-split
// command token ("/temp" or "/topp"); full is the whole input line.
func (m *model) handleSamplingCommand(cmd, full string) {
	fields := strings.Fields(full)
	if len(fields) < 2 {
		// No argument: report the current value instead of erroring.
		m.appendLine(m.samplingStatusLine(cmd))
		return
	}
	if len(fields) > 2 {
		m.appendLine(cmd + ": expected a single numeric value, e.g. " + cmd + " 0.7")
		return
	}

	raw := fields[1]
	// ParseFloat accepts forms like "Inf"/"NaN"; the validators below reject
	// those explicitly, so no non-finite value can slip through.
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		m.appendLine(cmd + ": invalid number " + strconv.Quote(raw))
		return
	}

	switch cmd {
	case "/temp":
		if err := config.ValidateTemperature(v); err != nil {
			m.appendLine("error: " + err.Error())
			return
		}
		if m.agent != nil {
			m.agent.SetTemperature(v)
		}
		m.temperature = v
		m.appendLine("temperature set to " + formatFloat(v))
	case "/topp":
		if err := config.ValidateTopP(v); err != nil {
			m.appendLine("error: " + err.Error())
			return
		}
		if m.agent != nil {
			p := v
			m.agent.SetTopP(&p)
		}
		m.topP = &v
		m.appendLine("top_p set to " + formatFloat(v))
	}
}

// samplingStatusLine reports the current value of a sampling command's setting.
func (m *model) samplingStatusLine(cmd string) string {
	switch cmd {
	case "/temp":
		return "temperature is " + formatFloat(m.temperature)
	case "/topp":
		if m.topP == nil {
			return "top_p is unset"
		}
		return "top_p is " + formatFloat(*m.topP)
	}
	return ""
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
