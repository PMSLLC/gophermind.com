// Package diagnose reads a run folder and says where the run's problems are.
// It is read-only and prints only facts that are safe to show: ids, counts,
// durations, classes and reasons. It never reads a prompt, a reply or a source
// file.
package diagnose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/ledger"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/report"
)

// Diagnosis is everything `brief diagnose` prints; --json prints it as is.
type Diagnosis struct {
	RunID      string             `json:"run_id"`
	RunDir     string             `json:"run_dir"`
	Status     string             `json:"status"`
	StopReason string             `json:"stop_reason,omitempty"`
	Waiting    string             `json:"waiting,omitempty"`
	Started    string             `json:"started_at,omitempty"`
	Finished   string             `json:"finished_at,omitempty"`
	Nodes      *report.NodeCounts `json:"nodes,omitempty"`
	Phases     []Phase            `json:"phases"`
	Events     map[string]int     `json:"events,omitempty"`
	Slowest    []SlowCall         `json:"slowest_calls"`
	Tokens     []TokenTotal       `json:"tokens"`
	PlannerErr *PlannerErr        `json:"planner_error,omitempty"`
	Problems   []Problem          `json:"problems"`
	Causes     []string           `json:"likely_cause"`
	Notes      []string           `json:"notes,omitempty"`
}

// Phase is one planner stage or executor phase, summed over its model calls.
type Phase struct {
	Name      string  `json:"name"`
	Calls     int     `json:"calls"`
	OK        int     `json:"ok"`
	Malformed int     `json:"malformed"`
	Failed    int     `json:"other_failed"`
	Retries   int     `json:"retries"`
	ModelSecs float64 `json:"model_seconds"`
	SpanSecs  float64 `json:"span_seconds"`
	PromptTok int64   `json:"prompt_tokens"`
	ReplyTok  int64   `json:"completion_tokens"`
	first     time.Time
	end       time.Time
}

type SlowCall struct {
	ID         int64  `json:"id"`
	Stage      string `json:"stage"`
	NodeID     string `json:"node_id,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Tokens     int    `json:"tokens"`
	Outcome    string `json:"outcome"`
}

type TokenTotal struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Calls     int    `json:"calls"`
	PromptTok int64  `json:"prompt_tokens"`
	ReplyTok  int64  `json:"completion_tokens"`
}

type PlannerErr struct {
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

// Attempt is one executor attempt at a node.
type Attempt struct {
	N               int    `json:"n"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Revision        int    `json:"revision"`
	Verdict         string `json:"verdict"`
	Class           string `json:"class"`
	Reason          string `json:"reason,omitempty"`
	MalformedReason string `json:"malformed_reason,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
}

// Problem is a node that failed, was skipped or is blocked.
type Problem struct {
	NodeID         string    `json:"node_id"`
	Status         string    `json:"status"`
	Reason         string    `json:"reason,omitempty"`
	Revisions      int       `json:"revisions"`
	Attempts       []Attempt `json:"attempts"`
	LastRefusal    string    `json:"last_refusal,omitempty"`
	RefusalStage   string    `json:"last_refusal_stage,omitempty"`
	RefusalRound   int       `json:"last_refusal_round,omitempty"`
	ReviseRefusals []string  `json:"revise_refusals,omitempty"`
	Hints          []string  `json:"hints,omitempty"`
}

const maxText = 300

var (
	malformedRE = regexp.MustCompile(`^[a-z_]{1,32}:[a-z_]{1,48}$`)
	nodeIDRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// clip makes s one printable ASCII line of at most maxText bytes.
func clip(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b.WriteRune(r)
		if b.Len() >= maxText {
			b.WriteString("...")
			break
		}
	}
	return b.String()
}

// Run builds the diagnosis of the run folder runDir.
func Run(ctx context.Context, runID, runDir string) (*Diagnosis, error) {
	if fi, err := os.Stat(runDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("no run folder at %s", runDir)
	}
	resolve := func(string) (string, error) { return runDir, nil }
	d := &Diagnosis{RunID: runID, RunDir: runDir, Status: "unknown", Phases: []Phase{}, Slowest: []SlowCall{}, Tokens: []TokenTotal{}, Problems: []Problem{}, Causes: []string{}}

	var rep *report.Report
	if r, err := report.Read(runDir); err == nil {
		rep = &r
		d.Status, d.StopReason = r.Status, r.StopReason
		d.Started, d.Finished = r.StartedAt, r.FinishedAt
		n := r.Nodes
		d.Nodes = &n
	} else {
		d.Notes = append(d.Notes, "no report.json: the executor has not finished a run here")
	}
	var st struct {
		Waiting string `json:"waiting"`
	}
	if raw, err := os.ReadFile(filepath.Join(runDir, "_state", "status.json")); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	d.Waiting = clip(st.Waiting)
	if rep == nil {
		d.Status = "planning"
		if st.Waiting != "" {
			d.Status = "waiting"
		}
	}

	calls, err := ledger.NewFS(resolve).List(ctx, runID, ledger.Filter{})
	if err != nil {
		d.Notes = append(d.Notes, "calls.jsonl cannot be read")
	}
	d.addCalls(calls)
	d.Events = eventCounts(runDir)

	if pe := planner.ReadPlannerError(runDir); pe != nil {
		d.PlannerErr = &PlannerErr{Stage: clip(pe.Stage), Text: clip(pe.Text)}
	}
	d.addProblems(ctx, runID, runDir, resolve, rep)
	d.Causes = d.causes(calls)
	return d, nil
}

func (d *Diagnosis) addCalls(calls []ledger.Call) {
	idx := map[string]int{}
	tokIdx := map[string]int{}
	seen := map[string]bool{}
	for _, c := range calls {
		name := c.TaskType
		if name == "" {
			name, _, _ = strings.Cut(c.Stage, ":")
		}
		i, ok := idx[name]
		if !ok {
			i = len(d.Phases)
			idx[name] = i
			d.Phases = append(d.Phases, Phase{Name: name})
		}
		p := &d.Phases[i]
		p.Calls++
		switch c.Outcome {
		case ledger.OutcomeOK:
			p.OK++
		case ledger.OutcomeMalformed:
			p.Malformed++
		default:
			p.Failed++
		}
		model := c.ModelServed
		if model == "" {
			model = c.ModelRequested
		}
		rk := fmt.Sprintf("%s|%s|%d|%s|%s", c.NodeID, c.Stage, c.Revision, c.Provider, model)
		if seen[rk] {
			p.Retries++
		}
		seen[rk] = true
		p.ModelSecs += float64(c.DurationMS) / 1000
		p.PromptTok += int64(c.PromptTokens)
		p.ReplyTok += int64(c.CompletionTokens)
		start := c.At.Add(-time.Duration(c.DurationMS) * time.Millisecond)
		if p.first.IsZero() || start.Before(p.first) {
			p.first = start
		}
		if c.At.After(p.end) {
			p.end = c.At
		}

		tk := c.Provider + "/" + model
		j, ok := tokIdx[tk]
		if !ok {
			j = len(d.Tokens)
			tokIdx[tk] = j
			d.Tokens = append(d.Tokens, TokenTotal{Provider: clip(c.Provider), Model: clip(model)})
		}
		d.Tokens[j].Calls++
		d.Tokens[j].PromptTok += int64(c.PromptTokens)
		d.Tokens[j].ReplyTok += int64(c.CompletionTokens)
	}
	for i := range d.Phases {
		p := &d.Phases[i]
		p.SpanSecs = p.end.Sub(p.first).Seconds()
		p.Name = clip(p.Name)
	}
	sorted := append([]ledger.Call(nil), calls...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].DurationMS > sorted[b].DurationMS })
	for i, c := range sorted {
		if i == 5 {
			break
		}
		d.Slowest = append(d.Slowest, SlowCall{ID: c.ID, Stage: clip(c.Stage), NodeID: clip(c.NodeID), DurationMS: c.DurationMS,
			Tokens: c.PromptTokens + c.CompletionTokens, Outcome: clip(string(c.Outcome))})
	}
}

// eventCounts counts the node events in _state/events.jsonl by kind.
func eventCounts(runDir string) map[string]int {
	raw, err := os.ReadFile(filepath.Join(runDir, "_state", "events.jsonl"))
	if err != nil {
		return nil
	}
	out := map[string]int{}
	for _, l := range strings.Split(string(raw), "\n") {
		var e struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(l), &e) == nil && e.Kind != "" {
			out[clip(e.Kind)]++
		}
	}
	return out
}

func (d *Diagnosis) addProblems(ctx context.Context, runID, runDir string, resolve func(string) (string, error), rep *report.Report) {
	status := map[string]string{}
	reason := map[string]string{}
	revisions := map[string]int{}
	if rows, err := blackboard.NewFS(resolve).List(ctx, runID, blackboard.Filter{}); err == nil {
		for _, r := range rows {
			status[r.NodeID] = string(r.Status)
			revisions[r.NodeID] = r.Revision
		}
	}
	bad := map[string]bool{}
	// _state/leaf-results.json is read here, not through the executor package,
	// which only builds on unix.
	var res map[string]struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if raw, err := os.ReadFile(filepath.Join(runDir, "_state", "leaf-results.json")); err == nil && json.Unmarshal(raw, &res) == nil {
		for id, r := range res {
			if r.Status != string(blackboard.StatusVerified) {
				bad[id] = true
				status[id], reason[id] = r.Status, r.Reason
			}
		}
	}
	if rep != nil {
		for _, f := range rep.Failures {
			id, why, ok := strings.Cut(f, ": ")
			if !ok || !nodeIDRE.MatchString(id) {
				continue
			}
			bad[id] = true
			if reason[id] == "" {
				reason[id] = why
			}
			if strings.HasPrefix(why, "blocked by") {
				status[id] = "blocked"
			}
		}
	}
	for id, s := range status {
		switch blackboard.Status(s) {
		case blackboard.StatusFailed, blackboard.StatusNeedsRevision, blackboard.StatusEscalated:
			bad[id] = true
		}
	}
	revised := map[string][]string{}
	if rep != nil {
		for _, rl := range rep.Revised {
			revised[rl.NodeID] = rl.Refusals
		}
	}
	lastRef := map[string]planner.Refusal{}
	for _, rf := range planner.ReadRefusals(runDir) {
		if rf.NodeID != "" {
			lastRef[rf.NodeID] = rf
		}
	}
	ids := make([]string, 0, len(bad))
	for id := range bad {
		if nodeIDRE.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := Problem{NodeID: id, Status: clip(status[id]), Reason: clip(reason[id]), Revisions: revisions[id], Attempts: readAttempts(runDir, id)}
		if rf, ok := lastRef[id]; ok {
			p.LastRefusal, p.RefusalStage, p.RefusalRound = clip(rf.Text), clip(rf.Stage), rf.Round
		}
		for _, s := range revised[id] {
			p.ReviseRefusals = append(p.ReviseRefusals, clip(s))
		}
		p.Hints = nodeHints(p)
		d.Problems = append(d.Problems, p)
	}
	// When planning failed there are no executor problems; the refusals are the lead.
	if len(d.Problems) == 0 && d.PlannerErr != nil {
		for id, rf := range lastRef {
			d.Problems = append(d.Problems, Problem{NodeID: clip(id), Status: "planning", Attempts: []Attempt{},
				LastRefusal: clip(rf.Text), RefusalStage: clip(rf.Stage), RefusalRound: rf.Round})
		}
		sort.Slice(d.Problems, func(i, j int) bool { return d.Problems[i].NodeID < d.Problems[j].NodeID })
	}
}

// readAttempts reads attempts/<node>/<n>/attempt.json in order of n.
func readAttempts(runDir, node string) []Attempt {
	out := []Attempt{}
	entries, err := os.ReadDir(filepath.Join(runDir, "attempts", node))
	if err != nil {
		return out
	}
	var ns []int
	for _, e := range entries {
		if n, err := strconv.Atoi(e.Name()); err == nil && n > 0 {
			ns = append(ns, n)
		}
	}
	sort.Ints(ns)
	for _, n := range ns {
		raw, err := os.ReadFile(filepath.Join(runDir, "attempts", node, strconv.Itoa(n), "attempt.json"))
		if err != nil || len(raw) > 1<<16 {
			continue
		}
		var w map[string]any
		if json.Unmarshal(raw, &w) != nil {
			continue
		}
		str := func(k string) string { s, _ := w[k].(string); return clip(s) }
		num := func(k string) float64 { f, _ := w[k].(float64); return f }
		a := Attempt{N: n, Provider: str("provider"), Model: str("model"), Revision: int(num("revision")), Verdict: str("verdict"),
			Class: str("class"), Reason: str("reason"), DurationMS: int64(num("duration_ms"))}
		if mr, _ := w["malformed_reason"].(string); malformedRE.MatchString(mr) {
			a.MalformedReason = mr
		}
		out = append(out, a)
	}
	return out
}

func allClass(as []Attempt, want ...string) bool {
	if len(as) == 0 {
		return false
	}
	for _, a := range as {
		ok := false
		for _, w := range want {
			if a.Class == w {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// nodeHints applies simple rules to one node's attempts and refusals.
func nodeHints(p Problem) []string {
	var h []string
	as := p.Attempts
	n := len(as)
	if strings.HasPrefix(p.Reason, "blocked by ") {
		h = append(h, "not tried: "+p.Reason+"; fix that node first")
	}
	if strings.Contains(p.Reason, "skipped by human") {
		h = append(h, fmt.Sprintf("a person skipped this node after %d attempt(s); see PROBLEMS for the attempts", n))
	}
	if n >= 3 {
		tally := map[string]int{}
		for _, a := range as {
			tally[orDash(a.Class, a.Verdict)]++
		}
		top, topN := "", 0
		for c, k := range tally {
			if k > topN || (k == topN && c < top) {
				top, topN = c, k
			}
		}
		if topN*2 > n && topN < n {
			h = append(h, fmt.Sprintf("most attempts (%d of %d) were %s", topN, n, top))
		}
	}
	if n >= 2 && allClass(as, "identical_reply") {
		h = append(h, fmt.Sprintf("all %d attempts were identical_reply: the model gave the same answer each time; the prompt carries nothing new for it to act on", n))
	}
	if n >= 1 && allClass(as, "truncated") {
		h = append(h, fmt.Sprintf("all %d attempts were truncated: the reply limit is smaller than the code asked for", n))
	}
	if n >= 2 && allClass(as, "build") {
		h = append(h, fmt.Sprintf("all %d attempts failed to build (same compiler class): see attempts/%s/<n>/check-output.txt", n, p.NodeID))
	}
	if n >= 2 && allClass(as, "test_fail") {
		if a, b := as[n-1].Reason, as[n-2].Reason; a != "" && a == b {
			h = append(h, "the last two attempts failed the same tests: the code compiles, so the tests and the contract may disagree")
		} else {
			h = append(h, fmt.Sprintf("all %d attempts compiled but failed tests", n))
		}
	}
	if n >= 1 {
		mal := true
		for _, a := range as {
			if a.MalformedReason == "" {
				mal = false
			}
		}
		if mal {
			h = append(h, "every attempt was refused as malformed (see malformed_reason)")
		}
	}
	if n >= 1 && allClass(as, "rate_limited", "timeout", "context_too_long", "auth", "model_missing") {
		h = append(h, "no model answered usefully: every attempt ended in a provider-side class")
	}
	if p.RefusalRound >= 3 {
		h = append(h, fmt.Sprintf("the planner refused %d replies for this node at stage %s", p.RefusalRound, p.RefusalStage))
	}
	return h
}

// causes is the run-level LIKELY CAUSE list.
func (d *Diagnosis) causes(calls []ledger.Call) []string {
	var out []string
	byID := map[string]Problem{}
	for _, p := range d.Problems {
		byID[p.NodeID] = p
	}
	// Follow "blocked by" to the node that is not itself blocked.
	rootsSeen := map[string]bool{}
	for _, p := range d.Problems {
		cur := p
		for i := 0; i < 20 && strings.HasPrefix(cur.Reason, "blocked by "); i++ {
			next, ok := byID[strings.TrimPrefix(cur.Reason, "blocked by ")]
			if !ok {
				break
			}
			cur = next
		}
		if cur.NodeID != p.NodeID && !rootsSeen[cur.NodeID] {
			rootsSeen[cur.NodeID] = true
			out = append(out, fmt.Sprintf("%s is where the blocking starts (%s)", cur.NodeID, orDash(cur.Reason, cur.Status)))
		}
	}
	for _, p := range d.Problems {
		for _, h := range p.Hints {
			if !strings.HasPrefix(h, "not tried") {
				out = append(out, p.NodeID+": "+h)
			}
		}
	}
	if len(calls) == 0 {
		out = append(out, "no model call is recorded in this run")
	} else {
		tail, bad := 0, ""
		for i := len(calls) - 1; i >= 0 && calls[i].Outcome != ledger.OutcomeOK; i-- {
			tail++
			bad = string(calls[i].Outcome)
		}
		if tail >= 3 {
			out = append(out, fmt.Sprintf("the last %d model calls all failed (latest outcome %s): check the provider", tail, bad))
		}
	}
	failed := map[string]int{}
	failedMS := map[string]int64{}
	failedStage := map[string][]string{}
	for _, c := range calls {
		if c.Outcome == ledger.OutcomeOK || c.Outcome == ledger.OutcomeMalformed {
			continue
		}
		o := string(c.Outcome)
		failed[o]++
		failedMS[o] += c.DurationMS
		if len(failedStage[o]) < 3 && !containsStr(failedStage[o], c.Stage) {
			failedStage[o] = append(failedStage[o], clip(c.Stage))
		}
	}
	outcomes := make([]string, 0, len(failed))
	for o := range failed {
		outcomes = append(outcomes, o)
	}
	sort.Strings(outcomes)
	for _, o := range outcomes {
		if failed[o] >= 2 {
			out = append(out, fmt.Sprintf("%d model calls ended in %s after %.0f seconds in total (stages: %s): no usable answer came back, check the provider and its timeout", failed[o], clip(o), float64(failedMS[o])/1000, strings.Join(failedStage[o], ", ")))
		}
	}
	if d.PlannerErr != nil {
		out = append(out, "planning stopped in stage "+d.PlannerErr.Stage+"; see PLANNER ERROR")
	}
	if len(out) == 0 && d.Status == "failed" {
		out = append(out, "no rule matched: read the attempts above and attempts/<node>/<n>/check-output.txt")
	}
	return out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func orDash(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Text is the report a person reads.
func (d *Diagnosis) Text() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("run %s", d.RunID)
	w("folder: %s", d.RunDir)
	line := "status: " + d.Status
	if d.StopReason != "" {
		line += ", stop reason: " + d.StopReason
	}
	if d.Waiting != "" {
		line += ", waiting on a human at stage " + d.Waiting
	}
	w("%s", line)
	if d.Started != "" {
		w("started %s, finished %s", d.Started, orDash(d.Finished, "-"))
	}
	if d.Nodes != nil {
		w("nodes: %d total, %d verified, %d failed, %d escalated, %d blocked", d.Nodes.Total, d.Nodes.Verified, d.Nodes.Failed, d.Nodes.Escalated, d.Nodes.Blocked)
	}
	for _, n := range d.Notes {
		w("note: %s", n)
	}
	w("\nPHASES (span = first call start to last call end, so it includes waiting)")
	w("  %-12s %5s %5s %9s %6s %7s %9s %9s", "PHASE", "CALLS", "OK", "MALFORMED", "OTHER", "RETRIES", "MODEL S", "SPAN S")
	for _, p := range d.Phases {
		w("  %-12s %5d %5d %9d %6d %7d %9.1f %9.1f", p.Name, p.Calls, p.OK, p.Malformed, p.Failed, p.Retries, p.ModelSecs, p.SpanSecs)
	}
	if len(d.Events) > 0 {
		keys := make([]string, 0, len(d.Events))
		for k := range d.Events {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, d.Events[k]))
		}
		w("  node events: %s", strings.Join(parts, ", "))
	}
	w("\nSLOWEST MODEL CALLS")
	for _, c := range d.Slowest {
		w("  #%d %s node %s %.1fs %d tokens %s", c.ID, c.Stage, orDash(c.NodeID, "-"), float64(c.DurationMS)/1000, c.Tokens, c.Outcome)
	}
	w("\nTOKENS BY PROVIDER AND MODEL")
	for _, t := range d.Tokens {
		w("  %s/%s: %d calls, %d prompt, %d reply", t.Provider, t.Model, t.Calls, t.PromptTok, t.ReplyTok)
	}
	if d.PlannerErr != nil {
		w("\nPLANNER ERROR (stage %s)\n  %s", d.PlannerErr.Stage, d.PlannerErr.Text)
	}
	w("\nPROBLEMS")
	if len(d.Problems) == 0 {
		w("  none")
	}
	for _, p := range d.Problems {
		w("  %s: %s%s, %d revision(s), %d attempt(s)", p.NodeID, p.Status, withParens(p.Reason), p.Revisions, len(p.Attempts))
		for _, a := range p.Attempts {
			line := fmt.Sprintf("    attempt %d: %s/%s rev %d %s %s", a.N, a.Provider, a.Model, a.Revision, a.Verdict, orDash(a.Class, "-"))
			if a.Reason != "" && a.Reason != a.Class {
				line += " (" + a.Reason + ")"
			}
			if a.MalformedReason != "" {
				line += " malformed_reason=" + a.MalformedReason
			}
			w("%s", line)
		}
		if p.LastRefusal != "" {
			w("    last planner refusal (%s, round %d): %s", p.RefusalStage, p.RefusalRound, p.LastRefusal)
		}
		for _, r := range p.ReviseRefusals {
			w("    revise refused: %s", r)
		}
	}
	w("\nLIKELY CAUSE")
	if len(d.Causes) == 0 {
		w("  none found")
	}
	for _, c := range d.Causes {
		w("  - %s", c)
	}
	return b.String()
}

func withParens(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
