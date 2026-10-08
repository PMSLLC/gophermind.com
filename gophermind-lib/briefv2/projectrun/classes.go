package projectrun

import (
	"math"
	"sort"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
)

// classOrder is the fixed order of the node-class table.
var classOrder = []string{"pure", "validation", "handler", "client", "storage", "concurrency", "wiring", "other", "unclassified"}

// ClassStat is one row of the node-class table: how the leaves of one class
// fared. Ids never appear here, only counts and class names.
type ClassStat struct {
	Class            string  `json:"class"`
	Leaves           int     `json:"leaves"`
	Verified         int     `json:"verified"`
	Failed           int     `json:"failed"`
	Escalated        int     `json:"escalated"`
	NotRun           int     `json:"not_run"`
	Attempts         int     `json:"attempts"`
	Passes           int     `json:"passes"`
	FirstTryWins     int     `json:"first_try_wins"`
	FirstPassRate    float64 `json:"first_pass_rate"`
	Calls            int     `json:"calls"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
}

// ClassStats groups the blackboard rows by node class. classes maps a node id
// to its class (planner.ReadClasses); an id not in it, or a class not in the
// fixed list, counts as unclassified. calls are the implement calls of the run.
// Attempts, passes and first-try wins are counted as report.Build counts them:
// attempts by revision then order, error verdicts skipped, the first counted
// attempt of a row is its first try.
func ClassStats(rows []blackboard.Row, classes map[string]string, calls []ledger.Call) []ClassStat {
	known := map[string]bool{}
	for _, c := range classOrder {
		known[c] = true
	}
	norm := func(c string) string {
		if known[c] {
			return c
		}
		return "unclassified"
	}
	by := map[string]*ClassStat{}
	get := func(c string) *ClassStat {
		s := by[c]
		if s == nil {
			s = &ClassStat{Class: c}
			by[c] = s
		}
		return s
	}
	for _, row := range rows {
		s := get(norm(classes[row.NodeID]))
		s.Leaves++
		switch row.Status {
		case blackboard.StatusVerified:
			s.Verified++
		case blackboard.StatusFailed:
			s.Failed++
		case blackboard.StatusEscalated:
			s.Escalated++
		}
		atts := append([]blackboard.Attempt(nil), row.Attempts...)
		sort.SliceStable(atts, func(i, j int) bool {
			if atts[i].Revision != atts[j].Revision {
				return atts[i].Revision < atts[j].Revision
			}
			return atts[i].Order < atts[j].Order
		})
		first := true
		for _, a := range atts {
			if a.Verdict == blackboard.VerdictError {
				continue
			}
			s.Attempts++
			if a.Verdict == blackboard.VerdictPass {
				s.Passes++
				if first {
					s.FirstTryWins++
				}
			}
			first = false
		}
	}
	for _, c := range calls {
		s, ok := by[c.NodeClass]
		if !ok {
			s = get("unclassified")
		}
		s.Calls++
		s.PromptTokens += int64(c.PromptTokens)
		s.CompletionTokens += int64(c.CompletionTokens)
	}
	var out []ClassStat
	for _, c := range classOrder {
		s := by[c]
		if s == nil {
			continue
		}
		s.NotRun = s.Leaves - s.Verified - s.Failed - s.Escalated
		if s.Leaves > 0 {
			s.FirstPassRate = math.Round(float64(s.FirstTryWins)/float64(s.Leaves)*1e4) / 1e4
		}
		out = append(out, *s)
	}
	return out
}
