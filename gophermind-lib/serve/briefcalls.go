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
	At               string `json:"at"`
	Provider         string `json:"provider"`
	ModelRequested   string `json:"model_requested"`
	ErrorKind        string `json:"error_kind"`
	ErrorDetail      string `json:"error_detail"`
}

// maxCallErrors is how many failed calls the run page lists, newest first.
const maxCallErrors = 20

// BriefCallError is one model call that did not succeed, and what went wrong.
// Detail is the provider's own message or a fixed description; it never holds
// prompt or reply text.
type BriefCallError struct {
	At         string `json:"at"`
	Stage      string `json:"stage"`
	Node       string `json:"node,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Outcome    string `json:"outcome"`
	Kind       string `json:"kind,omitempty"`
	Detail     string `json:"detail"`
	DurationMS int64  `json:"duration_ms"`
}

// parseCalls reads _state/calls.jsonl of a run folder, applying each amend to
// its call. A missing file, or a line that is not JSON, is skipped.
func parseCalls(dir string) []callLine {
	b, err := readSmallFile(filepath.Join(dir, "_state", "calls.jsonl"), maxCallsBytes)
	if err != nil {
		return nil
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
				if c.ErrorKind != "" {
					calls[i].ErrorKind = c.ErrorKind
				}
			}
		}
	}
	return calls
}

// readCallErrors lists the calls whose outcome is not ok, newest first, at
// most maxCallErrors of them.
func readCallErrors(dir string) []BriefCallError {
	out := []BriefCallError{}
	calls := parseCalls(dir)
	for i := len(calls) - 1; i >= 0 && len(out) < maxCallErrors; i-- {
		c := calls[i]
		if c.Outcome == "ok" || c.Outcome == "" {
			continue
		}
		stage := c.Stage
		if stage == "" {
			stage = c.TaskType
		}
		detail := c.ErrorDetail
		if detail == "" {
			detail = describeKind(c.ErrorKind, c.Outcome)
		}
		out = append(out, BriefCallError{At: c.At, Stage: stage, Node: c.NodeID, Provider: c.Provider, Model: c.ModelRequested,
			Outcome: c.Outcome, Kind: c.ErrorKind, Detail: detail, DurationMS: c.DurationMS})
	}
	return out
}

// describeKind says in plain words what an error kind means, for calls whose
// record has no message of its own (older runs, and replies the planner
// refused, which are never stored because a parse error can quote them).
func describeKind(kind, outcome string) string {
	switch kind {
	case "malformed":
		return "the reply arrived but did not match the format the planner needs, so it asked again"
	case "truncated":
		return "the reply used its whole token budget before it finished"
	case "transient":
		return "the model server returned an error or could not be reached (this record did not keep its message)"
	case "timeout":
		return "the model did not answer in time"
	case "rate_limited":
		return "the provider asked for a pause"
	case "empty_reply":
		return "the model returned no text"
	case "too_long":
		return "the prompt was too long for the model"
	case "auth":
		return "the provider rejected the credentials"
	case "model_missing":
		return "the provider does not have this model"
	case "cancelled":
		return "the run was stopped before the model answered"
	}
	if kind != "" {
		return "the call did not succeed (" + kind + ")"
	}
	return "the call did not succeed (" + outcome + ")"
}

// readCalls folds _state/calls.jsonl of a run folder into per-stage totals and
// the slowest calls. A missing file, or a line that is not JSON, is skipped.
func readCalls(dir string) ([]BriefStage, []BriefSlowCall) {
	stages, slow := []BriefStage{}, []BriefSlowCall{}
	calls := parseCalls(dir)
	if calls == nil {
		return stages, slow
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
