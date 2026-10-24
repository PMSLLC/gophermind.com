package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/build/events"
)

// maxRefusalBytes bounds the refusal text recorded for one refused reply.
const maxRefusalBytes = 400

// Refusal is one line of _state/refusals.jsonl: why the planner refused one
// model reply. Text is the planner's own refusal wording (the same text a
// repair prompt carries), never the reply.
type Refusal struct {
	At     string `json:"at"`
	Stage  string `json:"stage"`
	NodeID string `json:"node_id,omitempty"`
	Round  int    `json:"round"`
	Text   string `json:"text"`
}

// safeRefusal keeps printable ASCII, joins lines with "; " and bounds the size.
func safeRefusal(s string) string { return safeText(s, maxRefusalBytes) }

// safeText is safeRefusal with a caller-chosen bound.
func safeText(s string, max int) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		for _, r := range line {
			if r < 0x20 || r > 0x7e {
				r = '?'
			}
			b.WriteRune(r)
		}
		if b.Len() >= max {
			break
		}
	}
	out := b.String()
	if len(out) > max {
		out = out[:max] + "..."
	}
	return out
}

// recordRefusal emits a refusal event and appends it to _state/refusals.jsonl,
// so the run folder keeps why a reply was refused after the repair call is
// made. The round counts the refusals of one stage and node from 1. A write
// failure is ignored: observability must not fail a planning run.
func (p *Planner) recordRefusal(r *run, stage, nodeID string, cause error) {
	text := safeRefusal(cause.Error())
	p.refusalMu.Lock()
	if p.refusals == nil {
		p.refusals = map[string]int{}
	}
	key := stage + "\x00" + nodeID
	p.refusals[key]++
	round := p.refusals[key]
	p.refusalMu.Unlock()

	p.emit(events.KindRefusal, stage, nodeID, text)
	if r == nil || r.dir == "" {
		return
	}
	line, err := json.Marshal(Refusal{At: p.d.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Stage: stage, NodeID: nodeID, Round: round, Text: text})
	if err != nil {
		return
	}
	p.refusalMu.Lock()
	defer p.refusalMu.Unlock()
	path := r.path(stateRefusals)
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// ReadRefusals returns the refusals recorded in a run folder, oldest first.
// A missing file is no refusals; a malformed line is skipped.
func ReadRefusals(runDir string) []Refusal {
	raw, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(stateRefusals)))
	if err != nil {
		return nil
	}
	var out []Refusal
	for _, l := range strings.Split(string(raw), "\n") {
		var rf Refusal
		if strings.TrimSpace(l) != "" && json.Unmarshal([]byte(l), &rf) == nil {
			out = append(out, rf)
		}
	}
	return out
}

// PlannerError is _state/planner_error.json: the error that stopped the last
// planning attempt. Text is the error the command printed, bounded and made
// single-line; it is removed when a later attempt gets past that stage.
type PlannerError struct {
	At    string `json:"at"`
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

const maxPlannerErrorBytes = 1500

// recordPlannerError keeps the error that ended a planning attempt in the run
// folder, so a person does not need the terminal output of the failed run.
func (p *Planner) recordPlannerError(r *run, stage string, cause error) {
	_ = writeJSON(r.path(statePlannerError), PlannerError{At: p.d.Now().UTC().Format("2006-01-02T15:04:05Z"), Stage: stage, Text: safeText(cause.Error(), maxPlannerErrorBytes)})
}

// ReadPlannerError returns the recorded planner error, or nil when there is none.
func ReadPlannerError(runDir string) *PlannerError {
	var pe PlannerError
	if found, err := readJSON(filepath.Join(runDir, filepath.FromSlash(statePlannerError)), &pe); err != nil || !found {
		return nil
	}
	return &pe
}
