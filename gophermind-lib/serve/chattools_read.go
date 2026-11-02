package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/tools"
)

func idSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"id": map[string]any{"type": "string", "description": "the brief run id, as listed by briefs_list"}},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}

// briefsList lists every known brief run, newest first.
func (c *chatTools) briefsList() tools.Tool {
	return tools.Tool{
		Name:        "briefs_list",
		Description: "List the brief runs on this server, newest first: id, title, phase, started.",
		Schema:      emptySchema(),
		Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			items, _ := c.bm.listItems()
			if len(items) == 0 {
				return "no briefs", nil
			}
			var b strings.Builder
			for _, it := range items {
				phase := it.Phase
				if phase == "" {
					phase = it.Status
				}
				fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", it.ID, oneLine(it.Title), oneLine(phase), it.StartedAt)
			}
			return c.cleanWith(strings.TrimRight(b.String(), "\n"), "the briefs folder"), nil
		},
	}
}

// briefState summarizes one run: phase, error, nodes, failing attempts, open
// questions and the log tail.
func (c *chatTools) briefState() tools.Tool {
	return tools.Tool{
		Name:        "brief_state",
		Description: "Show one brief run: phase, error, nodes with status, failing attempts, open questions and the end of its log.",
		Schema:      idSchema(),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("brief_state: bad arguments: %w", err)
			}
			if !validBriefID(a.ID) {
				return "", fmt.Errorf("brief_state: %q is not a valid brief id", a.ID)
			}
			a.ID = c.bm.resolveID(a.ID)
			st, err := c.bm.buildState(a.ID)
			if err != nil {
				return "", fmt.Errorf("brief_state: no such brief %q", a.ID)
			}
			var b strings.Builder
			r := st.Run
			fmt.Fprintf(&b, "id: %s\ntitle: %s\nphase: %s\nstatus: %s\nstarted: %s\n", r.ID, oneLine(r.Title), r.Phase, r.Status, r.StartedAt)
			if r.Error != "" {
				fmt.Fprintf(&b, "error: %s\n", r.Error)
			}
			if st.StopReason != "" {
				fmt.Fprintf(&b, "stop reason: %s\n", st.StopReason)
			}
			b.WriteString("nodes:\n")
			for _, n := range st.Nodes {
				fmt.Fprintf(&b, "  %s\t%s\t%s\n", n.ID, n.Status, oneLine(n.Title))
			}
			b.WriteString("failing attempts:\n")
			for _, n := range st.Nodes {
				for _, at := range n.Attempts {
					if at.Outcome != "pass" {
						fmt.Fprintf(&b, "  %s #%d %s: %s\n", n.ID, at.N, at.Outcome, oneLine(at.FailureReason))
					}
				}
			}
			b.WriteString("open questions:\n")
			for _, q := range st.Questions {
				if q.Status != "settled" {
					fmt.Fprintf(&b, "  %s: %s\n", q.ID, oneLine(q.Question))
				}
			}
			b.WriteString("log tail:\n")
			b.WriteString(strings.Join(st.LogTail, "\n"))
			return c.cleanWith(b.String(), "the run's run.log"), nil
		},
	}
}

// questionsPending lists every unanswered question across all briefs. The
// harness holds no questions of its own: its runs ask on their own run
// folders, so those runs are found here and tagged with source harness.
func (c *chatTools) questionsPending() tools.Tool {
	return tools.Tool{
		Name:        "questions_pending",
		Description: "List every unanswered question across all brief runs: source (brief or harness), run id, question id, text, options, the run phase (waiting runs first), the recommended answer if any, and whether it can be answered through the chat.",
		Schema:      emptySchema(),
		Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			owned := map[string]bool{}
			if c.h != nil {
				st := c.h.snapshot()
				if st.Current != "" {
					owned[st.Current] = true
				}
				for _, hb := range st.Briefs {
					if hb.LastRun != "" {
						owned[hb.LastRun] = true
					}
				}
			}
			items, _ := c.bm.listItems()
			type entry struct {
				waiting bool
				text    string
			}
			var entries []entry
			for _, it := range items {
				if !validBriefID(it.ID) {
					continue
				}
				phase := it.Phase
				if phase == "" {
					phase = it.Status
				}
				var rec *briefRecord
				if r, ok := c.bm.readRecord(it.ID); ok {
					rec = &r
					if r.Phase != "" {
						phase = r.Phase
					}
				}
				source := "brief"
				if owned[it.ID] {
					source = "harness"
				}
				for _, q := range questionsOf(c.bm.repoFor(it.ID), it.ID, rec) {
					if q.Status == "settled" {
						continue
					}
					ask := "no"
					if q.Askable {
						ask = "yes"
					}
					def := "default: none"
					if q.Recommended != "" {
						def = "recommended: " + oneLine(q.Recommended)
					}
					entries = append(entries, entry{phase == phaseWaiting, fmt.Sprintf("source: %s\nbrief: %s\nrun phase: %s\nquestion: %s\ntext: %s\noptions: %s\n%s\nanswerable in chat: %s\n\n",
						source, it.ID, oneLine(phase), q.ID, oneLine(q.Question), oneLine(strings.Join(q.Options, " | ")), def, ask)})
				}
			}
			sort.SliceStable(entries, func(i, j int) bool { return entries[i].waiting && !entries[j].waiting })
			var b strings.Builder
			for _, en := range entries {
				b.WriteString(en.text)
			}
			if b.Len() == 0 {
				return "nothing is waiting", nil
			}
			return c.cleanWith(strings.TrimRight(b.String(), "\n"), "each run's QUESTIONS.md"), nil
		},
	}
}

// harnessState summarizes the example-brief harness.
func (c *chatTools) harnessState() tools.Tool {
	return tools.Tool{
		Name:        "harness_state",
		Description: "Show the example-brief harness: whether it is running, the run it waits on, its note and the score of each example brief.",
		Schema:      emptySchema(),
		Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			if c.h == nil {
				return "harness is off", nil
			}
			st := c.h.snapshot()
			var b strings.Builder
			fmt.Fprintf(&b, "running: %t\ndone: %t\n", st.Running, st.Done)
			if st.Current != "" {
				fmt.Fprintf(&b, "current run: %s (%s)\n", st.Current, oneLine(st.CurrentFile))
			}
			if st.Note != "" {
				fmt.Fprintf(&b, "note: %s\n", oneLine(st.Note))
			}
			fmt.Fprintf(&b, "updated: %s\nbriefs:\n", st.UpdatedAt)
			for _, hb := range st.Briefs {
				fmt.Fprintf(&b, "  %s\t%s\tattempts %d\tpasses %d\tlast %s %s\n", hb.File, oneLine(hb.Title), hb.Attempts, hb.Passes, hb.LastRun, hb.LastPhase)
			}
			return c.cleanWith(b.String(), "the harness state file"), nil
		},
	}
}

// briefArtifact lists a run's nodes, a node's saved attempt files, or returns
// one file's text.
func (c *chatTools) briefArtifact() tools.Tool {
	return tools.Tool{
		Name:        "brief_artifact",
		Description: "Read what a brief run saved. With only id: list its nodes. With node: list its attempts and files. With node, attempt and file: return that file (reply.go, reply.txt, check-output.txt or attempt.json).",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":      map[string]any{"type": "string", "description": "the brief run id, as listed by briefs_list"},
				"node":    map[string]any{"type": "string", "description": "a node id from the node list"},
				"attempt": map[string]any{"type": "integer", "description": "attempt number, 1 or more"},
				"file":    map[string]any{"type": "string", "description": "one of the file names listed for the attempt"},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				ID      string `json:"id"`
				Node    string `json:"node"`
				Attempt int    `json:"attempt"`
				File    string `json:"file"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("brief_artifact: bad arguments: %w", err)
			}
			if !validBriefID(a.ID) {
				return "", fmt.Errorf("brief_artifact: %q is not a valid brief id", a.ID)
			}
			a.ID = c.bm.resolveID(a.ID)
			if a.Node == "" {
				st, err := c.bm.buildState(a.ID)
				if err != nil {
					return "", fmt.Errorf("brief_artifact: no such brief %q", a.ID)
				}
				var b strings.Builder
				for _, n := range st.Nodes {
					fmt.Fprintf(&b, "%s\t%s\t%s\n", n.ID, n.Status, oneLine(n.Title))
				}
				return c.cleanWith(strings.TrimRight(b.String(), "\n"), "the run folder"), nil
			}
			if !runview.ValidNodeID(a.Node) {
				return "", fmt.Errorf("brief_artifact: %q is not a valid node id", a.Node)
			}
			v, err := runview.Open(runRoot(c.bm.repos, c.bm.root, a.ID), a.ID)
			if err != nil {
				return "", fmt.Errorf("brief_artifact: no such brief %q", a.ID)
			}
			if a.File == "" {
				ents := v.ArtifactNames(a.Node)
				if len(ents) == 0 {
					return "no saved attempts for that node", nil
				}
				var b strings.Builder
				for _, e := range ents {
					fmt.Fprintf(&b, "attempt %d: %s\n", e.Attempt, strings.Join(e.Files, " "))
				}
				return c.cleanWith(strings.TrimRight(b.String(), "\n"), "the run folder"), nil
			}
			if a.Attempt < 1 || !runview.ValidArtifactName(a.File) {
				return "", fmt.Errorf("brief_artifact: attempt must be 1 or more and file one of the listed names")
			}
			text, err := v.Artifact(a.Node, a.Attempt, a.File)
			if err != nil {
				return "", fmt.Errorf("brief_artifact: no such file")
			}
			return c.cleanWith(text, "the saved file "+a.File), nil
		},
	}
}

// failGroup is one failure class in the digest.
type failGroup struct {
	reason string // first unnormalized example, one line, capped
	runs   []string
	nodes  []string
	count  int
}

// endGroup is one error class among the runs that ended failed or interrupted.
type endGroup struct {
	reason string // first unnormalized error line
	phases string // space-separated distinct phases
	runs   []string
	count  int
}

// failureOf is the text a failed node is grouped by: the failure reason of its
// last attempt that did not pass (BriefAttempt.FailureReason), with the
// malformed class and rule (BriefAttempt.MalformedReason), returned separately
// so the caller can keep the malformed part out of the first-line cut.
func failureOf(n BriefNode) (reason, malformed string) {
	for i := len(n.Attempts) - 1; i >= 0; i-- {
		at := n.Attempts[i]
		if at.Outcome == "pass" {
			continue
		}
		if at.FailureReason != "" || at.MalformedReason != "" {
			return at.FailureReason, at.MalformedReason
		}
	}
	return "(no failure reason recorded)", ""
}

// malformedSuffix is the bracketed malformed class, normalized and capped, or "".
func malformedSuffix(m string) string {
	if m == "" {
		return ""
	}
	return " [" + normalizeReason(m) + "]"
}

var (
	runIDInReasonRE = regexp.MustCompile(`gm-\d{4}-\d{2}-\d{2}-\d+|\b[a-z0-9]+(?:-[a-z0-9]+)*-v[0-9]+[a-z]?(?:\.[0-9]+[a-z]?)*-[0-9]{8}-r[0-9]{3,}`)
	timestampRE     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
	digitsRE        = regexp.MustCompile(`\d+`)
	spacesRE        = regexp.MustCompile(`\s+`)
)

// maxReasonRunes caps a reason or error line in the digest.
const maxReasonRunes = 160

// firstLine returns the first non-empty line of s, trimmed and capped.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return capRunes(spacesRE.ReplaceAllString(l, " "), maxReasonRunes)
		}
	}
	return ""
}

// maxReasonBytes also bounds a reason, so multi-byte text cannot outgrow the
// output cap.
const maxReasonBytes = 400

// capRunes cuts s to n runes and maxReasonBytes bytes, on a rune boundary.
func capRunes(s string, n int) string {
	r := []rune(s)
	cut := false
	if len(r) > n {
		r, cut = r[:n], true
	}
	for len(string(r)) > maxReasonBytes {
		r, cut = r[:len(r)-1], true
	}
	if !cut {
		return s
	}
	return string(r) + "..."
}

// normalizeReason reduces a failure reason to its class: first line only,
// run ids and timestamps replaced, digit runs replaced with #, whitespace
// collapsed, length capped. Two failures that differ only in line numbers,
// ids or durations normalize to the same text.
func normalizeReason(s string) string {
	l := firstLine(s)
	l = runIDInReasonRE.ReplaceAllString(l, "<run>")
	l = timestampRE.ReplaceAllString(l, "<time>")
	l = digitsRE.ReplaceAllString(l, "#")
	return capRunes(spacesRE.ReplaceAllString(l, " "), maxReasonRunes)
}

// maxDigestGroups is how many failure classes the digest prints.
const maxDigestGroups = 15

// maxDigestEnded and maxDigestRunsCited keep the digest under the output cap.
const (
	maxDigestEnded     = 10
	maxDigestRunsCited = 4
)

// runsDigest groups failed nodes over the newest runs by failure reason.
func (c *chatTools) runsDigest() tools.Tool {
	return tools.Tool{
		Name:        "runs_digest",
		Description: "Digest of the newest brief runs: failed nodes grouped by normalized failure reason (count, run ids, example nodes), then the runs that ended failed or interrupted with their error. Optional limit: runs to cover, default 20, max 30.",
		Schema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"limit": map[string]any{"type": "integer", "description": "how many of the newest runs to cover (default 20, max 30)"}},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Limit int `json:"limit"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("runs_digest: bad arguments: %w", err)
				}
			}
			if a.Limit <= 0 {
				a.Limit = 20
			}
			if a.Limit > 30 {
				a.Limit = 30
			}
			items, _ := c.bm.listItems()
			if len(items) == 0 {
				return "no runs", nil
			}
			if len(items) > a.Limit {
				items = items[:a.Limit]
			}
			groups := map[string]*failGroup{}
			var order []string
			endedGroups := map[string]*endGroup{}
			var endedOrder []string
			covered := 0
			cancelled := false
			for _, it := range items {
				if ctx.Err() != nil {
					cancelled = true
					break
				}
				if !validBriefID(it.ID) {
					continue
				}
				st, err := c.bm.buildState(it.ID)
				if err != nil {
					continue
				}
				covered++
				for _, n := range st.Nodes {
					if n.Status != "failed" {
						continue
					}
					reason, mal := failureOf(n)
					suffix := malformedSuffix(mal)
					key := normalizeReason(reason) + suffix
					g := groups[key]
					if g == nil {
						g = &failGroup{reason: firstLine(reason) + suffix}
						groups[key] = g
						order = append(order, key)
					}
					g.count++
					if len(g.runs) == 0 || g.runs[len(g.runs)-1] != it.ID {
						g.runs = append(g.runs, it.ID)
					}
					if len(g.nodes) < 3 {
						g.nodes = append(g.nodes, n.ID)
					}
				}
				if p := st.Run.Phase; p == phaseFailed || p == phaseInterrupted {
					key := normalizeReason(st.Run.Error)
					g := endedGroups[key]
					if g == nil {
						g = &endGroup{reason: firstLine(st.Run.Error)}
						endedGroups[key] = g
						endedOrder = append(endedOrder, key)
					}
					g.count++
					g.runs = append(g.runs, it.ID)
					if !strings.Contains(" "+g.phases+" ", " "+p+" ") {
						g.phases = strings.TrimSpace(g.phases + " " + p)
					}
				}
			}
			sort.SliceStable(endedOrder, func(i, j int) bool { return endedGroups[endedOrder[i]].count > endedGroups[endedOrder[j]].count })
			sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].count > groups[order[j]].count })
			var b strings.Builder
			fmt.Fprintf(&b, "runs covered: %d\nfailure classes:\n", covered)
			if len(order) == 0 {
				b.WriteString("  none\n")
			}
			for i, k := range order {
				if i == maxDigestGroups {
					fmt.Fprintf(&b, "  %d more classes omitted\n", len(order)-maxDigestGroups)
					break
				}
				g := groups[k]
				runs := g.runs
				more := ""
				if len(runs) > maxDigestRunsCited {
					more = fmt.Sprintf(" (+%d more)", len(runs)-maxDigestRunsCited)
					runs = runs[:maxDigestRunsCited]
				}
				fmt.Fprintf(&b, "  %s\n    count %d\n    runs: %s%s\n    example nodes: %s\n", g.reason, g.count, strings.Join(runs, ", "), more, strings.Join(g.nodes, ", "))
			}
			b.WriteString("runs that ended failed or interrupted:\n")
			if len(endedOrder) == 0 {
				b.WriteString("  none\n")
			}
			for i, k := range endedOrder {
				if i == maxDigestEnded {
					fmt.Fprintf(&b, "  %d more error classes omitted\n", len(endedOrder)-maxDigestEnded)
					break
				}
				g := endedGroups[k]
				runs, more := g.runs, ""
				if len(runs) > maxDigestRunsCited {
					more = fmt.Sprintf(" (+%d more)", len(runs)-maxDigestRunsCited)
					runs = runs[:maxDigestRunsCited]
				}
				fmt.Fprintf(&b, "  %s\n    count %d (%s)\n    runs: %s%s\n", g.reason, g.count, strings.ReplaceAll(g.phases, " ", ", "), strings.Join(runs, ", "), more)
			}
			if cancelled {
				b.WriteString("cancelled before all runs were read\n")
			}
			return c.cleanWith(strings.TrimRight(b.String(), "\n"), "the run folders"), nil
		},
	}
}
