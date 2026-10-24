package serve

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxCallsBytes = 16 << 20
	slowCallCount = 5
)

// BriefStage is the model calls of one stage kind (the stage name before any
// colon, such as implement; the task type when there is no stage) of a run, summed
// from _state/calls.jsonl. Problems counts every call whose outcome is not ok.
type BriefStage struct {
	Stage      string `json:"stage"`
	Calls      int    `json:"calls"`
	OK         int    `json:"ok"`
	Problems   int    `json:"problems"`
	DurationMS int64  `json:"duration_ms"`
	Tokens     int    `json:"tokens"`
}

// BriefSlowCall is one of the slowest model calls. It carries no prompt or
// reply text; the ledger does not hold any.
type BriefSlowCall struct {
	Stage      string `json:"stage"`
	Node       string `json:"node"`
	DurationMS int64  `json:"duration_ms"`
	Tokens     int    `json:"tokens"`
	Outcome    string `json:"outcome"`
}

// BriefProblem is a failed leaf and why its last attempt failed.
type BriefProblem struct {
	Node            string `json:"node"`
	Title           string `json:"title"`
	Attempts        int    `json:"attempts"`
	Class           string `json:"class"`
	Reason          string `json:"reason"`
	MalformedReason string `json:"malformed_reason,omitempty"`
}

type callLine struct {
	Kind             string `json:"kind"`
	ID               int64  `json:"id"`
	Stage            string `json:"stage"`
	TaskType         string `json:"task_type"`
	NodeID           string `json:"node_id"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	DurationMS       int64  `json:"duration_ms"`
	Outcome          string `json:"outcome"`
}

// readCalls folds _state/calls.jsonl of a run folder into per-stage totals and
// the slowest calls. A missing file, or a line that is not JSON, is skipped.
func readCalls(dir string) ([]BriefStage, []BriefSlowCall) {
	stages, slow := []BriefStage{}, []BriefSlowCall{}
	b, err := readSmallFile(filepath.Join(dir, "_state", "calls.jsonl"), maxCallsBytes)
	if err != nil {
		return stages, slow
	}
	var calls []callLine
	index := map[int64]int{}
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var c callLine
		if json.Unmarshal(ln, &c) != nil {
			continue
		}
		switch c.Kind {
		case "", "call":
			index[c.ID] = len(calls)
			calls = append(calls, c)
		case "amend":
			if i, ok := index[c.ID]; ok {
				calls[i].Outcome = c.Outcome
			}
		}
	}
	byStage := map[string]*BriefStage{}
	for _, c := range calls {
		// Stage names carry the node ("implement:fn-x"); the table groups by
		// the part before the colon.
		name, _, _ := strings.Cut(c.Stage, ":")
		if name == "" {
			name = c.TaskType
		}
		if name == "" {
			name = "(unnamed)"
		}
		s := byStage[name]
		if s == nil {
			s = &BriefStage{Stage: name}
			byStage[name] = s
		}
		s.Calls++
		if c.Outcome == "ok" {
			s.OK++
		} else {
			s.Problems++
		}
		s.DurationMS += c.DurationMS
		s.Tokens += c.PromptTokens + c.CompletionTokens
	}
	for _, s := range byStage {
		stages = append(stages, *s)
	}
	sort.Slice(stages, func(i, j int) bool {
		if stages[i].DurationMS != stages[j].DurationMS {
			return stages[i].DurationMS > stages[j].DurationMS
		}
		return stages[i].Stage < stages[j].Stage
	})
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].DurationMS > calls[j].DurationMS })
	for i := 0; i < len(calls) && i < slowCallCount; i++ {
		c := calls[i]
		name := c.Stage
		if name == "" {
			name = c.TaskType
		}
		slow = append(slow, BriefSlowCall{Stage: name, Node: c.NodeID, DurationMS: c.DurationMS,
			Tokens: c.PromptTokens + c.CompletionTokens, Outcome: c.Outcome})
	}
	return stages, slow
}

// problemsOf names each failed leaf with the class and reason of its last attempt.
func problemsOf(nodes []BriefNode) []BriefProblem {
	out := []BriefProblem{}
	for _, n := range nodes {
		if n.Kind != "function" || n.Status != "failed" {
			continue
		}
		p := BriefProblem{Node: n.ID, Title: n.Title, Attempts: len(n.Attempts)}
		if k := len(n.Attempts); k > 0 {
			a := n.Attempts[k-1]
			p.Class, p.Reason, p.MalformedReason = a.Outcome, a.FailureReason, a.MalformedReason
		}
		out = append(out, p)
	}
	return out
}
