package runview

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

const maxWarnings = 20

// View reads one run folder. Every method reads fresh from disk (there is no
// cache to go stale) and none writes.
type View struct {
	root, dir, runID string
	bb               *blackboard.FS
	poll             time.Duration
	now              func() time.Time

	mu       sync.Mutex
	warnings []string
}

// Option configures Open.
type Option func(*View)

// WithPoll sets how often Follow looks for new event lines (default 250 ms).
func WithPoll(d time.Duration) Option {
	return func(v *View) {
		if d > 0 {
			v.poll = d
		}
	}
}

// WithClock replaces the clock used for generated_at and the running report.
func WithClock(now func() time.Time) Option { return func(v *View) { v.now = now } }

// Open returns a view of the run runID under root (the repo root).
func Open(root, runID string, opts ...Option) (*View, error) {
	dir, err := runDirOf(root, runID)
	if err != nil {
		return nil, err
	}
	v := &View{
		root: root, dir: dir, runID: runID,
		bb:   blackboard.NewFS(runfs.Fixed(dir)),
		poll: 250 * time.Millisecond,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

// runStamp is the newest modification time of the files updatedAt() stats.
func runStamp(dir string) time.Time {
	var newest time.Time
	for _, rel := range updateFiles {
		if fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil && fi.Mode().IsRegular() && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

type stamped struct {
	id string
	at time.Time
}

// stampedRuns lists the valid run folders under root with their stamps, newest
// first: latest update, run id descending as the tiebreak. It builds no summary.
func stampedRuns(root string) ([]stamped, error) {
	base := filepath.Join(root, ".gophermind")
	if !realDir(base) {
		return nil, nil
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, errors.New("runview: the run folder cannot be read")
	}
	var out []stamped
	for _, e := range ents {
		if !ValidRunID(e.Name()) {
			continue
		}
		dir, err := runDirOf(root, e.Name())
		if err != nil {
			continue
		}
		out = append(out, stamped{e.Name(), runStamp(dir)})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].at.Equal(out[j].at) {
			return out[i].at.After(out[j].at)
		}
		return out[i].id > out[j].id
	})
	return out, nil
}

// newestRunID is the most recently updated run under root, or "" when there is
// none. It stats files only and builds no summary.
func newestRunID(root string) string {
	runs, _ := stampedRuns(root)
	if len(runs) == 0 {
		return ""
	}
	return runs[0].id
}

// Runs lists the runs under root/.gophermind, newest first: the most recently
// updated run first, run id descending as the tiebreak. A repo with none gives
// an empty, non-nil list. A folder that is not a run, or is a link, is skipped.
func Runs(root string) ([]RunSummary, error) {
	out := []RunSummary{}
	runs, err := stampedRuns(root)
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		v, err := Open(root, r.id)
		if err != nil {
			continue
		}
		out = append(out, v.Summary())
	}
	return out, nil
}

func (v *View) eventsPath() string { return filepath.Join(v.dir, "_state", "events.jsonl") }

// warn records something that was skipped, once, without echoing any content.
func (v *View) warn(msg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, w := range v.warnings {
		if w == msg {
			return
		}
	}
	if len(v.warnings) < maxWarnings {
		v.warnings = append(v.warnings, msg)
	}
}

// Warnings returns what this view skipped so far: a corrupt event line, an
// unreadable runtime file or plan. Messages carry ids and line numbers only.
func (v *View) Warnings() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.warnings...)
}

// EventSeq is the number of lines in _state/events.jsonl.
func (v *View) EventSeq() int64 {
	lines, _, err := runfs.ReadLines(v.eventsPath(), 0)
	if err != nil {
		return 0
	}
	return int64(len(lines))
}

type loaded struct {
	t    *tree.Tree
	rows map[string]blackboard.Row
}

// load reads the plan and every node's runtime file. A plan that cannot be
// read is an empty tree plus a warning; a runtime file that cannot be read
// leaves that node on its plan status plus a warning.
func (v *View) load(ctx context.Context) loaded {
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		v.warn("plan: the node files cannot be read")
		t = &tree.Tree{Nodes: map[string]tree.Node{}}
	}
	rows := make(map[string]blackboard.Row, len(t.Nodes))
	for id := range t.Nodes {
		r, err := v.bb.Get(ctx, v.runID, id)
		switch {
		case err == nil:
			rows[id] = r
		case errors.Is(err, blackboard.ErrNotFound):
		default:
			v.warn("node " + id + ": the runtime file cannot be read")
		}
	}
	return loaded{t: t, rows: rows}
}

// statusSeverity orders statuses for a group node: the worst child wins.
var statusSeverity = map[string]int{
	"pending": 1, "ready": 2, "claimed": 3, "in_progress": 3, "needs_revision": 4, "escalated": 5, "failed": 6,
}

// deriveGroupStatus sets the status of component and root nodes, which have no
// runtime file: verified when every child is verified, else the worst child.
// The plan file's own status is ignored for them.
func deriveGroupStatus(t *tree.Tree, out []NodeSummary) {
	idx := make(map[string]int, len(out))
	for i, n := range out {
		idx[n.ID] = i
	}
	done := map[string]bool{}
	var derive func(id string) string
	derive = func(id string) string {
		i := idx[id]
		n := t.Nodes[id]
		if n.Kind == tree.KindFunction || len(n.Children) == 0 || done[id] {
			return out[i].Status
		}
		done[id] = true // a cycle keeps the plan status
		all, worst, worstRank := true, "", 0
		for _, c := range n.Children {
			if _, ok := idx[c]; !ok {
				continue
			}
			s := derive(c)
			if s != "verified" {
				all = false
			}
			if r := statusSeverity[s]; r > worstRank {
				worst, worstRank = s, r
			}
		}
		switch {
		case all:
			out[i].Status = "verified"
		case worst != "":
			out[i].Status = worst
		}
		return out[i].Status
	}
	for _, n := range out {
		derive(n.ID)
	}
}

func kindRank(k tree.Kind) int {
	switch k {
	case tree.KindRoot:
		return 0
	case tree.KindComponent:
		return 1
	}
	return 2
}

func (v *View) summarize(l loaded) []NodeSummary {
	out := make([]NodeSummary, 0, len(l.t.Nodes))
	for id, n := range l.t.Nodes {
		doc := docOf(n)
		ns := NodeSummary{
			ID: id, Kind: string(n.Kind), Parent: n.Parent, Title: str(doc, "title"), Status: str(doc, "status"),
			ModelTier: str(doc, "model_tier"), NodeClass: str(doc, "node_class"),
			DependsOn: nonNil(n.DependsOn), RequirementIDs: strs(doc, "requirement_ids"),
			Revision: num(doc, "revision"), Tests: TestCount{Total: arrLen(doc, "tests")},
		}
		if n.Wave != nil {
			ns.Wave = *n.Wave
		}
		if r, ok := l.rows[id]; ok {
			ns.Status, ns.Revision = string(r.Status), r.Revision
			if r.Claim != nil {
				ns.ClaimedBy = r.Claim.Worker
			}
			ns.AttemptCount = len(r.Attempts)
			if len(r.Attempts) > 0 {
				a := wireAttempt(r.Attempts[len(r.Attempts)-1])
				ns.LastAttempt = &a
			}
			if r.Result != nil {
				res := wireResult(r.Result)
				ns.Result = &res
			}
		}
		out = append(out, ns)
	}
	deriveGroupStatus(l.t, out)
	rank := func(k string) int { return kindRank(tree.Kind(k)) }
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		if a.Wave != b.Wave {
			return a.Wave < b.Wave
		}
		return a.ID < b.ID
	})
	return out
}

type statusFile struct {
	Waiting   string `json:"waiting"`
	PlannedAt string `json:"planned_at"`
}

type execFile struct {
	StartedAt string `json:"started_at"`
}

// readJSON decodes a small run file; false when it is missing or invalid.
func (v *View) readJSON(rel string, out any) bool {
	data, _, err := readLimited(filepath.Join(v.dir, filepath.FromSlash(rel)), 1<<20)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

func (v *View) exists(rel string) bool {
	return realFile(filepath.Join(v.dir, filepath.FromSlash(rel)))
}

// status derives the run status from which files exist (spec section 5).
func (v *View) status() (status, reason string) {
	if v.exists(report.FileName) {
		rep, err := report.Read(v.dir)
		if err != nil {
			v.warn("report.json: it cannot be read")
			return StatusFailed, "report_unreadable"
		}
		if rep.Status == "failed" && rep.StopReason == "harness_fault" {
			return StatusHarnessFault, rep.StopReason
		}
		return rep.Status, rep.StopReason
	}
	// A /project run that stopped before the executor wrote only this file.
	// Decode just its two keys: it also holds run_dir and secret names. Once
	// the executor has started (a resumed run), it no longer applies.
	if !v.exists("_state/executor.json") {
		var pj struct {
			Status     string `json:"status"`
			StopReason string `json:"stop_reason"`
		}
		if v.readJSON("_state/project.json", &pj) && pj.Status != "" {
			return pj.Status, pj.StopReason
		}
	}
	if v.exists("_state/executor.json") {
		return StatusBuilding, ""
	}
	if v.exists("approval.json") {
		return StatusPlanned, ""
	}
	var st statusFile
	if v.readJSON("_state/status.json", &st) && st.Waiting != "" {
		return StatusWaiting, ""
	}
	return StatusPlanning, ""
}

func (v *View) title(l loaded) string {
	for _, n := range l.t.Nodes {
		if n.Kind == tree.KindRoot {
			if s := str(docOf(n), "title"); s != "" {
				return s
			}
		}
	}
	if data, _, err := readLimited(filepath.Join(v.dir, "brief.md"), 64<<10); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(ln, "# ") {
				return strings.TrimSpace(ln[2:])
			}
		}
	}
	return v.runID
}

func (v *View) repoName() string {
	abs, err := filepath.Abs(v.root)
	if err != nil {
		abs = v.root
	}
	return filepath.Base(abs)
}

var updateFiles = []string{"brief.md", "approval.json", "report.json", "_state/events.jsonl", "_state/status.json", "_state/executor.json", "_state/project.json"}

func (v *View) updatedAt() string { return ts(runStamp(v.dir)) }

func (v *View) summaryOf(l loaded, nodes []NodeSummary) RunSummary {
	s := RunSummary{RunID: v.runID, Title: v.title(l), RepoName: v.repoName(), Counts: Counts{ByStatus: map[string]int{}}}
	s.Status, s.StopReason = v.status()
	waves := map[int]bool{}
	for _, n := range nodes {
		if n.Kind != string(tree.KindFunction) {
			continue
		}
		s.Counts.Nodes++
		s.Counts.ByStatus[n.Status]++
		waves[n.Wave] = true
	}
	s.Waves = len(waves)
	var st statusFile
	if v.readJSON("_state/status.json", &st) {
		s.PlannedAt = st.PlannedAt
	}
	var ex execFile
	if v.readJSON("_state/executor.json", &ex) {
		s.StartedAt = ex.StartedAt
	}
	if s.StartedAt == "" && v.exists(report.FileName) {
		if rep, err := report.Read(v.dir); err == nil {
			s.StartedAt = rep.StartedAt
		}
	}
	if reqs, err := planner.ReadRequirements(v.dir); err == nil {
		s.Coverage.Total = len(reqs)
	}
	if cov, err := planner.ReadCoverage(v.dir); err == nil {
		s.Coverage.Covered = len(cov.Covered)
	}
	s.UpdatedAt = v.updatedAt()
	return s
}

// Summary is the run's header: status, times, waves, counts and coverage.
func (v *View) Summary() RunSummary {
	l := v.load(context.Background())
	return v.summaryOf(l, v.summarize(l))
}

// State is GET /pipeline/state: the summary plus every node. The event count
// is read first, so resuming the stream at EventSeq misses nothing.
func (v *View) State() StateBody {
	seq := v.EventSeq()
	l := v.load(context.Background())
	nodes := v.summarize(l)
	sum := v.summaryOf(l, nodes)
	return StateBody{
		SchemaVersion: SchemaVersion, GeneratedAt: v.now().UTC().Format(time.RFC3339Nano),
		EventSeq: seq, Run: &sum, Nodes: nodes, Warnings: v.Warnings(),
	}
}

func docOf(n tree.Node) map[string]any {
	raw, err := n.Marshal()
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func num(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }

func arrLen(m map[string]any, k string) int { a, _ := m[k].([]any); return len(a) }

func strs(m map[string]any, k string) []string {
	arr, _ := m[k].([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return runfs.TS(t)
}

func wireAttempt(a blackboard.Attempt) Attempt {
	return Attempt{
		Model: a.Model, Provider: a.Provider, Verdict: string(a.Verdict), FailureReason: a.FailureReason,
		DurationMS: a.DurationMS, StartedAt: ts(a.StartedAt), Order: a.Order, Revision: a.Revision,
		TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal, FailedTests: nonNil(a.FailedTests),
	}
}

func wireResult(r *blackboard.Result) NodeResult {
	return NodeResult{Commit: r.Commit, FilesChanged: nonNil(r.FilesChanged)}
}
