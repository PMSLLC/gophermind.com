package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/runview"
)

// FormatRunHeader is the panel's run line: id, status, waves, nodes, coverage.
func FormatRunHeader(r *runview.RunSummary) string {
	if r == nil {
		return "No run yet"
	}
	status := r.Status
	if r.StopReason != "" {
		status += " (" + r.StopReason + ")"
	}
	return fmt.Sprintf("%s: %s\nwaves %d, nodes %d, coverage %d/%d", r.RunID, status, r.Waves, r.Counts.Nodes, r.Coverage.Covered, r.Coverage.Total)
}

// FormatNodeLine is one function node: status, id, wave, model tier, attempt
// count, and its last attempt with the failure reason when there is one.
func FormatNodeLine(n runview.NodeSummary) string {
	line := fmt.Sprintf("  [%s] %s (wave %d", n.Status, n.ID, n.Wave)
	if n.ModelTier != "" {
		line += ", " + n.ModelTier
	}
	if n.AttemptCount > 0 {
		line += fmt.Sprintf(", %d attempt(s)", n.AttemptCount)
	}
	line += ")"
	if a := n.LastAttempt; a != nil {
		line += fmt.Sprintf(" last: %s %s", a.Model, a.Verdict)
		if a.FailureReason != "" {
			line += " - " + a.FailureReason
		}
	}
	return line
}

// FormatNodeTree lists the function nodes grouped under their component, in
// the order the server sent them. Function nodes whose component is not in the
// list come last under "== (no component)". Root nodes are not rows.
func FormatNodeTree(nodes []runview.NodeSummary) []string {
	byParent := map[string][]runview.NodeSummary{}
	comps := map[string]bool{}
	for _, n := range nodes {
		switch n.Kind {
		case "component":
			comps[n.ID] = true
		case "function":
			byParent[n.Parent] = append(byParent[n.Parent], n)
		}
	}
	var out []string
	for _, c := range nodes {
		if c.Kind != "component" {
			continue
		}
		title := c.Title
		if title == "" {
			title = c.ID
		}
		out = append(out, "== "+title)
		for _, n := range byParent[c.ID] {
			out = append(out, FormatNodeLine(n))
		}
	}
	var orphans []string
	for _, n := range nodes {
		if n.Kind == "function" && !comps[n.Parent] {
			orphans = append(orphans, FormatNodeLine(n))
		}
	}
	if len(orphans) > 0 {
		out = append(out, "== (no component)")
		out = append(out, orphans...)
	}
	return out
}

// FormatReport renders a report: status, node counts, requirements, calls per
// task type and model, leaf pass rates, and the failure lines.
func FormatReport(r runview.ReportBody) string {
	var b strings.Builder
	state := r.Status
	if !r.IsComplete() {
		state += " (running)"
	}
	fmt.Fprintf(&b, "Run report: %s\n", state)
	fmt.Fprintf(&b, "nodes: %d verified of %d (%d failed, %d escalated, %d blocked)\n",
		r.Nodes.Verified, r.Nodes.Total, r.Nodes.Failed, r.Nodes.Escalated, r.Nodes.Blocked)
	fmt.Fprintf(&b, "requirements %d/%d\n", r.Requirements.Covered, r.Requirements.Total)
	if len(r.ByTaskType) > 0 {
		b.WriteString("by task type:\n")
		for _, m := range r.ByTaskType {
			fmt.Fprintf(&b, "  %s  %s: %d calls, %d ok, %d malformed, %d retries\n", m.TaskType, m.Model, m.Calls, m.OK, m.Malformed, m.Retries)
		}
	}
	if len(r.Leaves) > 0 {
		b.WriteString("leaf models:\n")
		for _, m := range r.Leaves {
			fmt.Fprintf(&b, "  %s: %d/%d passed\n", m.Model, m.Passes, m.Attempts)
		}
	}
	if len(r.Failures) > 0 {
		b.WriteString("failures:\n")
		for _, f := range r.Failures {
			b.WriteString("  " + f + "\n")
		}
	}
	return b.String()
}

// FormatNodeDetail renders a node for the detail box: title, description, the
// reasoning groups that exist, its decisions, its tests, its runtime state and
// attempts, the saved artifacts, and the check output of the newest saved
// attempt when the caller fetched it.
func FormatNodeDetail(n runview.NodeBody, checkOutput string) string {
	var doc map[string]any
	_ = json.Unmarshal(n.Node, &doc)
	s := func(k string) string { v, _ := doc[k].(string); return v }
	sub := func(k, f string) string {
		m, _ := doc[k].(map[string]any)
		v, _ := m[f].(string)
		return v
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", s("title"), s("id"))
	if d := s("description"); d != "" {
		b.WriteString(d + "\n")
	}
	for _, g := range [][2]string{{"why", s("rationale")}, {"how", sub("construction", "approach_chosen")}, {"trust boundary", sub("security", "trust_boundary")}, {"complexity", sub("performance", "complexity")}} {
		if g[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", g[0], g[1])
		}
	}
	if ds, _ := doc["decisions"].([]any); len(ds) > 0 {
		b.WriteString("decisions:\n")
		for _, d := range ds {
			m, _ := d.(map[string]any)
			fmt.Fprintf(&b, "  %v: %v -> %v\n", m["id"], m["question"], m["answer"])
		}
	}
	if ts, _ := doc["tests"].([]any); len(ts) > 0 {
		var names []string
		for _, t := range ts {
			m, _ := t.(map[string]any)
			names = append(names, fmt.Sprint(m["name"]))
		}
		b.WriteString("tests: " + strings.Join(names, ", ") + "\n")
	}
	if rt := n.Runtime; rt != nil {
		fmt.Fprintf(&b, "status: %s (revision %d)\n", rt.Status, rt.Revision)
		for i, a := range rt.Attempts {
			fmt.Fprintf(&b, "  %d. %s %s", i+1, a.Model, a.Verdict)
			if a.FailureReason != "" {
				b.WriteString(" - " + a.FailureReason)
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("status: not started\n")
	}
	for _, a := range n.Artifacts {
		fmt.Fprintf(&b, "saved: attempt %d: %s\n", a.Attempt, strings.Join(a.Files, ", "))
	}
	if checkOutput != "" {
		b.WriteString("check output of the newest saved attempt:\n" + checkOutput + "\n")
	}
	return b.String()
}

// NodeSource is the part of client.Client LoadNodeDetail needs.
type NodeSource interface {
	PipelineNode(ctx context.Context, run, id string) (runview.NodeBody, error)
	PipelineArtifact(ctx context.Context, run, id string, attempt int, file string) (string, error)
}

// LoadNodeDetail fetches a node and, when it has a saved check output, the
// newest one, and formats them. A failed artifact fetch leaves the check
// output out; a failed node fetch is the error.
func LoadNodeDetail(ctx context.Context, src NodeSource, run, id string) (string, error) {
	body, err := src.PipelineNode(ctx, run, id)
	if err != nil {
		return "", err
	}
	check := ""
	for i := len(body.Artifacts) - 1; i >= 0 && check == ""; i-- {
		for _, f := range body.Artifacts[i].Files {
			if f == "check-output.txt" {
				if t, err := src.PipelineArtifact(ctx, run, id, body.Artifacts[i].Attempt, f); err == nil {
					check = t
				}
				break
			}
		}
	}
	return FormatNodeDetail(body, check), nil
}
