package tui

import (
	"strconv"
	"strings"

	"gophermind/internal/config"
)

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
