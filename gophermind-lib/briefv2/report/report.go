// Package report builds the run report: a pure aggregation of ledger rows and
// blackboard rows, so a hand count matches it. It holds ids, counts, hashes
// and fixed text only, never prompt text, reply text, command output or a
// secret.
package report

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/planner"
)

const (
	FileName = "report.json"
	// SchemaVersion is bumped on any change to the JSON shape.
	SchemaVersion = 1
	maxReadBytes  = 8 << 20
)

type Coverage struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

type Passed struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

type NodeCounts struct {
	Total     int `json:"total"`
	Verified  int `json:"verified"`
	Failed    int `json:"failed"`
	Escalated int `json:"escalated"`
	Blocked   int `json:"blocked"`
}

type TaskModel struct {
	TaskType            string `json:"task_type"`
	Model               string `json:"model"`
	Calls               int    `json:"calls"`
	OK                  int    `json:"ok"`
	Malformed           int    `json:"malformed"`
	Retries             int    `json:"retries"`
	ModelEscalations    int    `json:"model_escalations"`
	RevisionEscalations int    `json:"revision_escalations"`
	HumanEscalations    int    `json:"human_escalations"`
	PromptTokens        int64  `json:"prompt_tokens"`
	CompletionTokens    int64  `json:"completion_tokens"`
	AvgDurationMS       int64  `json:"avg_duration_ms"`
}

type LeafModel struct {
	Model        string  `json:"model"`
	Attempts     int     `json:"attempts"`
	Passes       int     `json:"passes"`
	PassRate     float64 `json:"pass_rate"`
	FirstTryWins int     `json:"first_try_wins"`
}

type Landing struct {
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	MergedInto string `json:"merged_into"`
}

// Escalation is one recorded escalation. The executor appends one to
// <run>/_state/escalations.json when it happens so the count survives a resume.
type Escalation struct {
	Kind     string `json:"kind"`
	TaskType string `json:"task_type"`
	Model    string `json:"model"`
	NodeID   string `json:"node_id"`
}

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	RunID         string      `json:"run_id"`
	StartedAt     string      `json:"started_at"`
	FinishedAt    string      `json:"finished_at"`
	Status        string      `json:"status"`
	Resumed       bool        `json:"resumed"`
	Sandbox       string      `json:"sandbox"`
	StopReason    string      `json:"stop_reason"`
	ExitCode      int         `json:"exit_code"`
	RepoBrief     string      `json:"repo_brief,omitempty"`
	RepoUsed      string      `json:"repo_used,omitempty"`
	Requirements  Coverage    `json:"requirements_covered"`
	Acceptance    Passed      `json:"acceptance"`
	Constraints   Passed      `json:"constraints_checked"`
	Waves         int         `json:"waves"`
	Nodes         NodeCounts  `json:"nodes"`
	ByTaskType    []TaskModel `json:"by_task_type"`
	Leaves        []LeafModel `json:"leaves"`
	WeakTests     int         `json:"weak_tests"`
	Repairs       int         `json:"repairs"`
	Incomplete    bool        `json:"incomplete,omitempty"`
	Failures      []string    `json:"failures,omitempty"`
	Landing       *Landing    `json:"landing,omitempty"`
}

// Input is everything Build aggregates. The caller builds Failures as
// "<node-id>: <class>[: <test names>]", "<id>: blocked by <dep>",
// "<id>: not_run: <reason>" or "<requirement-id>: exit <n>", never output text.
type Input struct {
	RunID                 string
	StartedAt, FinishedAt time.Time
	Status, StopReason    string
	Resumed               bool
	Sandbox               string
	RepoBrief, RepoUsed   string
	Calls                 []ledger.Call
	Rows                  []blackboard.Row
	Blocked               []string
	Requirements          []planner.Requirement
	Coverage              planner.CoverageFile
	AcceptancePassed      int
	AcceptanceTotal       int
	ConstraintsPassed     int
	ConstraintsTotal      int
	Waves                 int
	Escalations           []Escalation
	WeakTests, Repairs    int
	LedgerErrors          int
	Failures              []string
	Landing               *Landing
	// PlanLeaves is every leaf id of the plan. When non-nil, Build guarantees
	// each is either verified or named in Failures. nil means rows only.
	PlanLeaves []string
	// LeafDeps maps a leaf id to its direct dependencies, used to word the
	// reason for a leaf that never ran.
	LeafDeps map[string][]string
}

var taskOrder = map[string]int{"clarify": 0, "contract": 1, "decompose": 2, "coverage": 3, "testwrite": 4, "implement": 5, "revise": 6}

func rank(task string) int {
	if r, ok := taskOrder[task]; ok {
		return r
	}
	return len(taskOrder)
}

// ExitCode maps a run status to the process exit code.
func ExitCode(status, stopReason string) int {
	switch status {
	case "verified":
		return 0
	case "failed":
		return 1
	case "escalated":
		if stopReason == "waiting_on_human" {
			return 3
		}
		return 4
	case "interrupted":
		return 5
	}
	return 1
}

func modelKey(provider, served, requested string) string {
	if served == "" {
		served = requested
	}
	return provider + "/" + served
}

type tmKey struct{ task, model string }

type rowKey struct {
	node, stage string
	rev         int
	model       string
}

// Build aggregates in into a Report. It is deterministic: the same data in any
// order yields the same bytes.
func Build(in Input) (Report, error) {
	r := Report{
		SchemaVersion: SchemaVersion, RunID: in.RunID, Status: in.Status, Resumed: in.Resumed,
		Sandbox: in.Sandbox, StopReason: in.StopReason, ExitCode: ExitCode(in.Status, in.StopReason),
		RepoBrief: stripCredentials(in.RepoBrief), RepoUsed: stripCredentials(in.RepoUsed),
		Waves: in.Waves, WeakTests: in.WeakTests, Repairs: in.Repairs,
		Incomplete: in.LedgerErrors > 0, Landing: in.Landing,
		ByTaskType: []TaskModel{}, Leaves: []LeafModel{},
	}
	if !in.StartedAt.IsZero() {
		r.StartedAt = in.StartedAt.UTC().Format(time.RFC3339)
	}
	if !in.FinishedAt.IsZero() {
		r.FinishedAt = in.FinishedAt.UTC().Format(time.RFC3339)
	}
	for _, es := range in.Escalations {
		switch es.Kind {
		case "model", "revision", "human":
		default:
			return Report{}, fmt.Errorf("report: unknown escalation kind (%d bytes)", len(es.Kind))
		}
	}
	r.Failures = completeFailures(in)

	// Pass 1: ledger rows.
	entries := map[tmKey]*TaskModel{}
	totalMS := map[tmKey]int64{}
	get := func(task, model string) *TaskModel {
		k := tmKey{task, model}
		if e, ok := entries[k]; ok {
			return e
		}
		e := &TaskModel{TaskType: task, Model: model}
		entries[k] = e
		return e
	}
	seen := map[rowKey]bool{}
	for _, c := range in.Calls {
		m := modelKey(c.Provider, c.ModelServed, c.ModelRequested)
		e := get(c.TaskType, m)
		k := tmKey{c.TaskType, m}
		e.Calls++
		switch c.Outcome {
		case ledger.OutcomeOK:
			e.OK++
		case ledger.OutcomeMalformed:
			e.Malformed++
		}
		e.PromptTokens += int64(c.PromptTokens)
		e.CompletionTokens += int64(c.CompletionTokens)
		totalMS[k] += c.DurationMS
		rk := rowKey{c.NodeID, c.Stage, c.Revision, m}
		if seen[rk] {
			e.Retries++
		}
		seen[rk] = true
	}

	// Pass 2: blackboard attempts.
	type leafAgg struct{ attempts, passes, wins int }
	leaves := map[string]*leafAgg{}
	for _, row := range in.Rows {
		switch row.Status {
		case blackboard.StatusVerified:
			r.Nodes.Verified++
		case blackboard.StatusFailed:
			r.Nodes.Failed++
		case blackboard.StatusEscalated:
			r.Nodes.Escalated++
		}
		type ak struct {
			rev   int
			model string
		}
		seenAtt := map[ak]bool{}
		first := true
		attempts := append([]blackboard.Attempt(nil), row.Attempts...)
		sort.SliceStable(attempts, func(i, j int) bool {
			if attempts[i].Revision != attempts[j].Revision {
				return attempts[i].Revision < attempts[j].Revision
			}
			return attempts[i].Order < attempts[j].Order
		})
		for _, a := range attempts {
			if a.Verdict == blackboard.VerdictError {
				continue
			}
			m := a.Provider + "/" + a.Model
			l := leaves[m]
			if l == nil {
				l = &leafAgg{}
				leaves[m] = l
			}
			l.attempts++
			if a.Verdict == blackboard.VerdictPass {
				l.passes++
				if first {
					l.wins++
				}
			}
			first = false
			k := ak{a.Revision, m}
			if seenAtt[k] {
				get("implement", m).Retries++
			}
			seenAtt[k] = true
		}
	}
	r.Nodes.Total = len(in.Rows)
	r.Nodes.Blocked = len(in.Blocked)

	// Pass 3: escalations.
	for _, es := range in.Escalations {
		e := get(es.TaskType, es.Model)
		switch es.Kind {
		case "model":
			e.ModelEscalations++
		case "revision":
			e.RevisionEscalations++
		case "human":
			e.HumanEscalations++
		}
	}

	for k, e := range entries {
		if e.Calls > 0 {
			e.AvgDurationMS = totalMS[k] / int64(e.Calls)
		}
		r.ByTaskType = append(r.ByTaskType, *e)
	}
	sort.Slice(r.ByTaskType, func(i, j int) bool {
		a, b := r.ByTaskType[i], r.ByTaskType[j]
		if ra, rb := rank(a.TaskType), rank(b.TaskType); ra != rb {
			return ra < rb
		}
		if a.TaskType != b.TaskType {
			return a.TaskType < b.TaskType
		}
		return a.Model < b.Model
	})
	for m, l := range leaves {
		lm := LeafModel{Model: m, Attempts: l.attempts, Passes: l.passes, FirstTryWins: l.wins}
		if l.attempts > 0 {
			lm.PassRate = math.Round(float64(l.passes)/float64(l.attempts)*10000) / 10000
		}
		r.Leaves = append(r.Leaves, lm)
	}
	sort.Slice(r.Leaves, func(i, j int) bool { return r.Leaves[i].Model < r.Leaves[j].Model })

	// Proof counts, copied from the planner's files and the caller.
	r.Requirements = Coverage{Covered: len(in.Coverage.Covered), Total: len(in.Requirements)}
	kinds := map[string]planner.ReqKind{}
	accCount := 0
	for _, q := range in.Requirements {
		kinds[q.ID] = q.Kind
		if q.Kind == planner.ReqAcceptance {
			accCount++
		}
	}
	r.Acceptance = Passed{Passed: in.AcceptancePassed, Total: in.AcceptanceTotal}
	if in.AcceptanceTotal == 0 {
		r.Acceptance.Total = accCount
	}
	r.Constraints = Passed{Passed: in.ConstraintsPassed, Total: in.ConstraintsTotal}
	if in.ConstraintsTotal == 0 {
		for _, t := range in.Coverage.RootTests {
			if kinds[t.Requirement] == planner.ReqConstraint {
				r.Constraints.Total++
			}
		}
	}
	return r, nil
}

// completeFailures returns the caller's failure lines plus one line for every
// plan leaf that is neither verified nor named, sorted. A run never silently
// drops a leaf.
func completeFailures(in Input) []string {
	out := append([]string(nil), in.Failures...)
	if in.PlanLeaves != nil {
		status := map[string]blackboard.Status{}
		for _, row := range in.Rows {
			status[row.NodeID] = row.Status
		}
		named := func(id string) bool {
			for _, f := range in.Failures {
				if strings.HasPrefix(f, id+":") {
					return true
				}
			}
			return false
		}
		for _, id := range in.PlanLeaves {
			st, hasRow := status[id]
			if st == blackboard.StatusVerified || named(id) {
				continue
			}
			if hasRow {
				out = append(out, fmt.Sprintf("%s: %s: no failure recorded", id, st))
				continue
			}
			reason := "not_run: no record"
			deps := append([]string(nil), in.LeafDeps[id]...)
			sort.Strings(deps)
			for _, d := range deps {
				if status[d] != blackboard.StatusVerified {
					reason = "blocked by " + d
					break
				}
			}
			out = append(out, id+": "+reason)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// stripCredentials reduces a repository reference to scheme, host and path.
// It fails safe: after deleting whitespace and control characters and decoding
// percent-encoded "@", everything up to and including the LAST "@" in the whole
// string is discarded (the scheme is kept), whatever the user info contained.
// Only then are the query and fragment removed. When a "?" or "#" precedes that
// "@" and no path follows it, the string could be a password or a query, so
// only the scheme is kept and the host is shown as "[redacted]".
func stripCredentials(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.NewReplacer("%40", "@", "%2540", "@").Replace(s)
	if at := strings.LastIndex(s, "@"); at >= 0 {
		prefix := ""
		if i := strings.Index(s, "://"); i >= 0 && i < at && validScheme(s[:i]) {
			prefix = s[:i+3]
		}
		rest := s[at+1:]
		if strings.ContainsAny(s[:at], "?#") && !strings.Contains(rest, "/") {
			return prefix + "[redacted]"
		}
		s = prefix + rest
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func validScheme(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// Write stores r as indented JSON in runDir/report.json, mode 0600, by temp
// file and rename. runDir must be an existing directory that is not a symbolic
// link; an existing report.json that is a link is replaced, never followed.
// WriteOptions carries what Write cannot infer.
type WriteOptions struct {
	// RepoRoot is the repository root; every component of the run folder below
	// it must be a real directory.
	RepoRoot string
}

func Write(runDir string, r Report, o WriteOptions) error {
	if runDir == "" {
		return errors.New("report: no run folder")
	}
	if err := checkRunDir(runDir, o.RepoRoot); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return errors.New("report: cannot encode the report")
	}
	raw = append(raw, '\n')
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return errors.New("report: no random source for a temp file name")
	}
	tmp := filepath.Join(runDir, "."+FileName+".tmp"+hex.EncodeToString(suffix[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|pathsafe.NoFollow, 0o600)
	if err != nil {
		return errors.New("report: cannot create the temp file")
	}
	fail := func(msg string) error {
		f.Close()
		os.Remove(tmp)
		return errors.New(msg)
	}
	if err := f.Chmod(0o600); err != nil {
		return fail("report: cannot set the file mode")
	}
	if _, err := f.Write(raw); err != nil {
		return fail("report: cannot write the temp file")
	}
	if err := f.Sync(); err != nil {
		return fail("report: cannot sync the temp file")
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return errors.New("report: cannot close the temp file")
	}
	if err := os.Rename(tmp, filepath.Join(runDir, FileName)); err != nil {
		os.Remove(tmp)
		return errors.New("report: cannot move the report into place")
	}
	d, err := os.Open(runDir)
	if err != nil {
		return errors.New("report: cannot open the run folder to sync it")
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return errors.New("report: cannot sync the run folder")
	}
	return nil
}

// checkRunDir requires runDir to be RepoRoot or a path below it in which every
// component below RepoRoot is a real directory, not a symbolic link. RepoRoot
// itself may be reached through an OS alias (for example /var on macOS); only
// what lies under it is judged.
func checkRunDir(runDir, root string) error {
	bad := errors.New("report: the run folder is not a real directory under the repository root")
	if root == "" {
		return errors.New("report: no repository root")
	}
	root, runDir = filepath.Clean(root), filepath.Clean(runDir)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return bad
	}
	rel, err := filepath.Rel(root, runDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return bad
	}
	p := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			p = filepath.Join(p, part)
			fi, err := os.Lstat(p)
			if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
				return bad
			}
		}
	}
	return nil
}

// jsonDepthOK reports whether raw nests no deeper than max, ignoring brackets
// inside strings.
func jsonDepthOK(raw []byte, max int) bool {
	depth, inStr, esc := 0, false, false
	for _, b := range raw {
		switch {
		case inStr:
			if esc {
				esc = false
			} else if b == '\\' {
				esc = true
			} else if b == '"' {
				inStr = false
			}
		case b == '"':
			inStr = true
		case b == '{' || b == '[':
			depth++
			if depth > max {
				return false
			}
		case b == '}' || b == ']':
			depth--
		}
	}
	return true
}

// Read loads runDir/report.json. Errors describe the kind of problem, never the
// file's content.
func Read(runDir string) (Report, error) {
	var r Report
	p := filepath.Join(runDir, FileName)
	fi, err := os.Lstat(p)
	if err != nil {
		return r, errors.New("report: no report in the run folder")
	}
	if !fi.Mode().IsRegular() {
		return r, errors.New("report: report.json is not a regular file")
	}
	f, err := os.OpenFile(p, os.O_RDONLY|pathsafe.NoFollow, 0)
	if err != nil {
		return r, errors.New("report: cannot open report.json")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil || len(raw) > maxReadBytes {
		return r, errors.New("report: report.json cannot be read or is too large")
	}
	var v struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return r, fmt.Errorf("report: report.json is not valid JSON (%d bytes)", len(raw))
	}
	if v.SchemaVersion != SchemaVersion {
		return r, fmt.Errorf("report: unsupported schema version %d (want %d)", v.SchemaVersion, SchemaVersion)
	}
	if !jsonDepthOK(raw, 16) {
		return Report{}, errors.New("report: report.json nests too deeply")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Report{}, fmt.Errorf("report: report.json has the wrong shape (%d bytes)", len(raw))
	}
	if r.ExitCode != ExitCode(r.Status, r.StopReason) {
		return Report{}, errors.New("report: exit_code does not match status")
	}
	return r, nil
}
