package serve

import (
	"context"
	"encoding/json"
	"fmt"
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
				fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", it.ID, it.Title, phase, it.StartedAt)
			}
			return c.clean(strings.TrimRight(b.String(), "\n")), nil
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
			if !validBriefID(a.ID) || !runview.ValidRunID(a.ID) {
				return "", fmt.Errorf("brief_state: %q is not a valid brief id", a.ID)
			}
			st, err := c.bm.buildState(a.ID)
			if err != nil {
				return "", fmt.Errorf("brief_state: no such brief %q", a.ID)
			}
			var b strings.Builder
			r := st.Run
			fmt.Fprintf(&b, "id: %s\ntitle: %s\nphase: %s\nstatus: %s\nstarted: %s\n", r.ID, r.Title, r.Phase, r.Status, r.StartedAt)
			if r.Error != "" {
				fmt.Fprintf(&b, "error: %s\n", r.Error)
			}
			if st.StopReason != "" {
				fmt.Fprintf(&b, "stop reason: %s\n", st.StopReason)
			}
			b.WriteString("nodes:\n")
			for _, n := range st.Nodes {
				fmt.Fprintf(&b, "  %s\t%s\t%s\n", n.ID, n.Status, n.Title)
			}
			b.WriteString("failing attempts:\n")
			for _, n := range st.Nodes {
				for _, at := range n.Attempts {
					if at.Outcome != "pass" {
						fmt.Fprintf(&b, "  %s #%d %s: %s\n", n.ID, at.N, at.Outcome, at.FailureReason)
					}
				}
			}
			b.WriteString("open questions:\n")
			for _, q := range st.Questions {
				if q.Status != "settled" {
					fmt.Fprintf(&b, "  %s: %s\n", q.ID, q.Question)
				}
			}
			b.WriteString("log tail:\n")
			b.WriteString(strings.Join(st.LogTail, "\n"))
			return c.clean(b.String()), nil
		},
	}
}

// questionsPending lists every unanswered question across all briefs. The
// harness holds no questions of its own: its runs ask on their own run
// folders, so those runs are found here and tagged with source harness.
func (c *chatTools) questionsPending() tools.Tool {
	return tools.Tool{
		Name:        "questions_pending",
		Description: "List every unanswered question across all brief runs: source (brief or harness), run id, question id, text, options, whether a default exists, and whether it can be answered through the chat.",
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
			var b strings.Builder
			for _, it := range items {
				if !validBriefID(it.ID) || !runview.ValidRunID(it.ID) {
					continue
				}
				var rec *briefRecord
				if r, ok := c.bm.readRecord(it.ID); ok {
					rec = &r
				}
				source := "brief"
				if owned[it.ID] {
					source = "harness"
				}
				for _, q := range questionsOf(c.bm.repoFor(it.ID), it.ID, rec) {
					if q.Status == "settled" {
						continue
					}
					def, ask := "no", "no"
					if q.Recommended != "" {
						def = "yes"
					}
					if q.Askable {
						ask = "yes"
					}
					fmt.Fprintf(&b, "source: %s\nbrief: %s\nquestion: %s\ntext: %s\noptions: %s\ndefault: %s\nanswerable in chat: %s\n\n",
						source, it.ID, q.ID, q.Question, strings.Join(q.Options, " | "), def, ask)
				}
			}
			if b.Len() == 0 {
				return "nothing is waiting", nil
			}
			return c.clean(strings.TrimRight(b.String(), "\n")), nil
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
				fmt.Fprintf(&b, "current run: %s (%s)\n", st.Current, st.CurrentFile)
			}
			if st.Note != "" {
				fmt.Fprintf(&b, "note: %s\n", st.Note)
			}
			fmt.Fprintf(&b, "updated: %s\nbriefs:\n", st.UpdatedAt)
			for _, hb := range st.Briefs {
				fmt.Fprintf(&b, "  %s\t%s\tattempts %d\tpasses %d\tlast %s %s\n", hb.File, hb.Title, hb.Attempts, hb.Passes, hb.LastRun, hb.LastPhase)
			}
			return c.clean(b.String()), nil
		},
	}
}
