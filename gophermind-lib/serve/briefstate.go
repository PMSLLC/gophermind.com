package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/runview"
)

// BriefStateVersion is the schema_version of GET /app/briefs/{id}/state.
const BriefStateVersion = 1

// BriefState is the JSON the run page polls. It is built from the run folder
// and the server's record of the run; nothing in it is a path or a secret.
type BriefState struct {
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   string           `json:"generated_at"`
	Run           BriefRun         `json:"run"`
	Understanding string           `json:"understanding"`
	Nodes         []BriefNode      `json:"nodes"`
	Questions     []BriefQuestion  `json:"questions"`
	Acceptance    *BriefAcceptance `json:"acceptance"`
	WaveChecks    []BriefWave      `json:"wave_checks"`
	Repairs       BriefRepairs     `json:"repairs"`
	LogTail       []string         `json:"log_tail"`
}

// BriefRun is the header: phase is the launcher's view (planning, running,
// done, failed, stopped, interrupted; empty for a run started elsewhere), status
// is the run folder's own (planning, waiting, planned, building, verified ...).
type BriefRun struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	Phase      string                `json:"phase"`
	Status     string                `json:"status"`
	StopReason string                `json:"stop_reason"`
	Active     bool                  `json:"active"`
	ExitCode   *int                  `json:"exit_code"`
	StartedAt  string                `json:"started_at"`
	FinishedAt string                `json:"finished_at"`
	Error      string                `json:"error"`
	Waves      int                   `json:"waves"`
	Counts     runview.Counts        `json:"counts"`
	Coverage   runview.CoverageCount `json:"coverage"`
}

// BriefNode is one node of the decomposition. Status is the normalized one
// (pending, running, verified, failed, blocked); RawStatus is the blackboard's.
// Origin is empty until a function is reused from elsewhere.
type BriefNode struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	Parent         string         `json:"parent"`
	Title          string         `json:"title"`
	Wave           int            `json:"wave"`
	Status         string         `json:"status"`
	RawStatus      string         `json:"raw_status"`
	DependsOn      []string       `json:"depends_on"`
	RequirementIDs []string       `json:"requirement_ids"`
	Origin         string         `json:"origin"`
	Attempts       []BriefAttempt `json:"attempts"`
}

// BriefAttempt is one model attempt at a leaf; Outcome is pass, fail or error.
type BriefAttempt struct {
	N             int    `json:"n"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Outcome       string `json:"outcome"`
	FailureReason string `json:"failure_reason"`
	// MalformedReason is the fixed-word class and rule that refused a malformed
	// reply, read from attempts/<leaf>/<n>/attempt.json; omitted when empty.
	MalformedReason string `json:"malformed_reason,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	TestsPassed     int    `json:"tests_passed"`
	TestsTotal      int    `json:"tests_total"`
}

// BriefQuestion is a clarify question and, once settled, its answer.
type BriefQuestion struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Kind       string `json:"kind"`
	Round      int    `json:"round"`
	Status     string `json:"status"`
	Answer     string `json:"answer"`
	AnsweredBy string `json:"answered_by"`
	// Why, Options and Recommended are the model's advice for an open question.
	Why            string   `json:"why"`
	Options        []string `json:"options"`
	Recommended    string   `json:"recommended"`
	RecommendedWhy string   `json:"recommended_why"`
	// Askable is true when the run is waiting at the file gate and this
	// question is one it is waiting for: POST /answer accepts it.
	Askable bool `json:"askable"`
}

// BriefAcceptance is acceptance.json reduced to counts and verdicts.
type BriefAcceptance struct {
	Complete    bool              `json:"complete"`
	Round       int               `json:"round"`
	Reason      string            `json:"reason"`
	Acceptance  runviewCount      `json:"acceptance"`
	Constraints runviewCount      `json:"constraints"`
	Bullets     []BriefAcceptItem `json:"bullets"`
}

type runviewCount struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// BriefAcceptItem is one acceptance bullet or constraint and its verdict.
type BriefAcceptItem struct {
	Requirement string `json:"requirement"`
	Text        string `json:"text"`
	Verdict     string `json:"verdict"`
	Kind        string `json:"kind"` // acceptance or constraint
}

const (
	maxUnderstandingBytes = 16 << 10
	maxLogTailLines       = 200
	maxLogTailBytes       = 64 << 10
)

// leafStatus maps a blackboard status to the five the page shows.
func leafStatus(raw string) string {
	switch raw {
	case "verified":
		return "verified"
	case "failed", "escalated", "needs_revision":
		return "failed"
	case "claimed", "in_progress":
		return "running"
	}
	return "pending"
}

// markBlocked turns a pending node into blocked when something it depends on,
// directly or further up, has failed.
func markBlocked(nodes []BriefNode) {
	byID := make(map[string]*BriefNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	var dead func(id string, seen map[string]bool) bool
	dead = func(id string, seen map[string]bool) bool {
		n := byID[id]
		if n == nil || seen[id] {
			return false
		}
		seen[id] = true
		if n.Status == "failed" {
			return true
		}
		for _, d := range n.DependsOn {
			if dead(d, seen) {
				return true
			}
		}
		return false
	}
	for i := range nodes {
		if nodes[i].Status != "pending" {
			continue
		}
		for _, d := range nodes[i].DependsOn {
			if dead(d, map[string]bool{}) {
				nodes[i].Status = "blocked"
				break
			}
		}
	}
}

// tailLines returns the last lines of text, bounded in count and bytes.
func tailLines(text string) []string {
	if len(text) > maxLogTailBytes {
		text = text[len(text)-maxLogTailBytes:]
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLogTailLines {
		lines = lines[len(lines)-maxLogTailLines:]
	}
	return lines
}

// ErrBriefNotFound is returned when neither a record nor a run folder exists.
var ErrBriefNotFound = errors.New("serve: no such run")

type questionStoreFile struct {
	Questions []struct {
		ID             string   `json:"id"`
		Text           string   `json:"question"`
		Kind           string   `json:"kind"`
		Round          int      `json:"round"`
		Status         string   `json:"status"`
		Answer         string   `json:"answer"`
		AnsweredBy     string   `json:"answered_by"`
		Why            string   `json:"why_it_matters"`
		Options        []string `json:"options"`
		Recommended    string   `json:"recommended"`
		RecommendedWhy string   `json:"recommended_why"`
	} `json:"questions"`
}

type answersLegacyFile struct {
	Answers []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Assumed  bool   `json:"assumed"`
	} `json:"answers"`
}

type acceptanceDisk struct {
	Complete    bool         `json:"complete"`
	Reason      string       `json:"reason"`
	Round       int          `json:"round"`
	Acceptance  runviewCount `json:"acceptance"`
	Constraints runviewCount `json:"constraints_checked"`
	Bullets     []struct {
		Requirement string `json:"requirement"`
		Text        string `json:"text"`
		Verdict     string `json:"verdict"`
	} `json:"bullets"`
	ConstraintItems []struct {
		Requirement string `json:"requirement"`
		Text        string `json:"text"`
		Verdict     string `json:"verdict"`
	} `json:"constraints"`
}

// BuildBriefState reads the run folder <root>/.gophermind/<id> and the
// launcher's record (nil for a run started elsewhere) into a BriefState. It
// writes nothing and takes the clock and the log text as arguments, so the same
// folder gives the same JSON. ErrBriefNotFound when there is neither a record
// nor a folder.
func BuildBriefState(root, id string, rec *briefRecord, active bool, logText string, now time.Time) (BriefState, error) {
	st := BriefState{
		SchemaVersion: BriefStateVersion, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z"),
		Nodes: []BriefNode{}, Questions: []BriefQuestion{}, WaveChecks: []BriefWave{}, Repairs: BriefRepairs{Open: []BriefRepair{}}, LogTail: tailLines(logText),
	}
	st.Run = BriefRun{ID: id, Active: active, Counts: runview.Counts{ByStatus: map[string]int{}}}
	if rec != nil {
		st.Run.Title, st.Run.Phase, st.Run.ExitCode = rec.Title, rec.Phase, rec.ExitCode
		st.Run.StartedAt, st.Run.FinishedAt, st.Run.Error = rec.StartedAt, rec.FinishedAt, rec.Error
	}
	v, err := runview.Open(root, id, runview.WithClock(func() time.Time { return now }))
	if err != nil {
		if rec == nil {
			return BriefState{}, ErrBriefNotFound
		}
		// Launched, but the run folder is not (fully) made yet; the gate's
		// questions may still be there.
		st.Questions = questionsOf(root, id, rec)
		return st, nil
	}
	body := v.State()
	if r := body.Run; r != nil {
		if r.Title != "" {
			st.Run.Title = r.Title
		}
		st.Run.Status, st.Run.StopReason = r.Status, r.StopReason
		st.Run.Waves, st.Run.Counts, st.Run.Coverage = r.Waves, r.Counts, r.Coverage
		if st.Run.StartedAt == "" {
			st.Run.StartedAt = r.StartedAt
		}
	}
	dir := filepath.Join(root, ".gophermind", id)
	for _, ns := range body.Nodes {
		n := BriefNode{
			ID: ns.ID, Kind: ns.Kind, Parent: ns.Parent, Title: ns.Title, Wave: ns.Wave,
			RawStatus: ns.Status, Status: leafStatus(ns.Status), DependsOn: ns.DependsOn,
			RequirementIDs: ns.RequirementIDs, Attempts: []BriefAttempt{},
		}
		if n.DependsOn == nil {
			n.DependsOn = []string{}
		}
		if n.RequirementIDs == nil {
			n.RequirementIDs = []string{}
		}
		if ns.Kind == "function" {
			if nb, err := v.Node(ns.ID); err == nil {
				var doc struct {
					Origin string `json:"origin"`
				}
				_ = json.Unmarshal(nb.Node, &doc)
				n.Origin = doc.Origin
				if nb.Runtime != nil {
					for i, a := range nb.Runtime.Attempts {
						ba := BriefAttempt{
							N: i + 1, Model: a.Model, Provider: a.Provider, Outcome: a.Verdict,
							FailureReason: a.FailureReason, DurationMS: a.DurationMS,
							TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal,
						}
						if strings.HasPrefix(a.FailureReason, "malformed") {
							ba.MalformedReason = malformedReasonOf(dir, ns.ID, i+1)
						}
						n.Attempts = append(n.Attempts, ba)
					}
				}
			}
		}
		st.Nodes = append(st.Nodes, n)
	}
	markBlocked(st.Nodes)
	st.WaveChecks, st.Repairs = waveChecks(dir, st.Run, st.Nodes)

	if b, err := readSmallFile(filepath.Join(dir, "UNDERSTANDING.md"), maxUnderstandingBytes); err == nil {
		st.Understanding = string(b)
	}
	st.Questions = questionsOf(root, id, rec)
	st.Acceptance = readAcceptance(dir)
	return st, nil
}

// questionsOf is the run's questions, with the ones a waiting run is blocked on
// marked askable.
func questionsOf(root, id string, rec *briefRecord) []BriefQuestion {
	qs := readQuestions(filepath.Join(root, ".gophermind", id))
	if rec != nil && rec.Phase == phaseWaiting {
		qs = markAskable(qs, readGateQuestions(root, id))
	}
	return qs
}

func readQuestions(dir string) []BriefQuestion {
	out := []BriefQuestion{}
	if b, err := readSmallFile(filepath.Join(dir, "_state", "clarify", "questions.json"), 1<<20); err == nil {
		var f questionStoreFile
		if json.Unmarshal(b, &f) == nil {
			for _, q := range f.Questions {
				out = append(out, BriefQuestion{ID: q.ID, Question: q.Text, Kind: q.Kind, Round: q.Round,
					Status: q.Status, Answer: q.Answer, AnsweredBy: q.AnsweredBy,
					Why: q.Why, Options: nonNilStrings(q.Options), Recommended: q.Recommended, RecommendedWhy: q.RecommendedWhy})
			}
			return out
		}
	}
	if b, err := readSmallFile(filepath.Join(dir, "answers.json"), 1<<20); err == nil {
		var f answersLegacyFile
		if json.Unmarshal(b, &f) == nil {
			for _, a := range f.Answers {
				by := "human"
				if a.Assumed {
					by = "unattended-default"
				}
				out = append(out, BriefQuestion{ID: a.ID, Question: a.Question, Kind: "decision",
					Status: "settled", Answer: a.Answer, AnsweredBy: by})
			}
		}
	}
	return out
}

func readAcceptance(dir string) *BriefAcceptance {
	b, err := readSmallFile(filepath.Join(dir, "acceptance.json"), 4<<20)
	if err != nil {
		return nil
	}
	var f acceptanceDisk
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	a := &BriefAcceptance{Complete: f.Complete, Round: f.Round, Reason: f.Reason,
		Acceptance: f.Acceptance, Constraints: f.Constraints, Bullets: []BriefAcceptItem{}}
	for _, x := range f.Bullets {
		a.Bullets = append(a.Bullets, BriefAcceptItem{Requirement: x.Requirement, Text: x.Text, Verdict: x.Verdict, Kind: "acceptance"})
	}
	for _, x := range f.ConstraintItems {
		a.Bullets = append(a.Bullets, BriefAcceptItem{Requirement: x.Requirement, Text: x.Text, Verdict: x.Verdict, Kind: "constraint"})
	}
	return a
}

// buildState is the state of one run from the launcher's record, the run
// folder and the log.
func (m *briefManager) buildState(id string) (BriefState, error) {
	var rec *briefRecord
	logText := ""
	if x, ok := m.readRecord(id); ok {
		rec = &x
		if b, err := readSmallFile(filepath.Join(m.runDir(id), "run.log"), 8<<20); err == nil {
			logText = string(b)
		}
	}
	return BuildBriefState(m.root, id, rec, m.activeID() == id, logText, m.now())
}

// state handles GET /app/briefs/{id}/state.
func (m *briefManager) state(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runview.ValidRunID(id) {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	st, err := m.buildState(id)
	if err != nil {
		briefError(w, http.StatusNotFound, "no such run", nil)
		return
	}
	briefJSON(w, http.StatusOK, st)
}

// BriefListItem is one row of GET /app/api/briefs.
type BriefListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Phase     string `json:"phase"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
	Active    bool   `json:"active"`
}

// list handles GET /app/api/briefs: runs this server launched plus any other
// run folder under the root, newest first.
func (m *briefManager) list(w http.ResponseWriter, r *http.Request) {
	items := map[string]*BriefListItem{}
	if ents, err := readDirNames(m.dir); err == nil {
		for _, id := range ents {
			if rec, ok := m.readRecord(id); ok {
				items[id] = &BriefListItem{ID: id, Title: rec.Title, Phase: rec.Phase, StartedAt: rec.StartedAt}
			}
		}
	}
	if runs, err := runview.Runs(m.root); err == nil {
		for _, s := range runs {
			it := items[s.RunID]
			if it == nil {
				it = &BriefListItem{ID: s.RunID, Title: s.Title, StartedAt: s.StartedAt}
				if it.StartedAt == "" {
					it.StartedAt = s.PlannedAt
				}
				items[s.RunID] = it
			}
			it.Status = s.Status
			if it.Title == "" {
				it.Title = s.Title
			}
		}
	}
	active := m.activeID()
	out := make([]BriefListItem, 0, len(items))
	for id, it := range items {
		it.Active = id == active
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt > out[j].StartedAt
		}
		return out[i].ID > out[j].ID
	})
	briefJSON(w, http.StatusOK, map[string]any{"schema_version": BriefStateVersion, "active_run_id": active, "runs": out})
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// markAskable flags the questions the file gate is waiting on. A gate
// question the question store does not know (a stage asking on its own) is
// added from QUESTIONS.md, so it can still be answered.
func markAskable(qs []BriefQuestion, gate []gateQuestion) []BriefQuestion {
	for _, g := range gate {
		found := false
		for i := range qs {
			if qs[i].ID == g.ID {
				found = true
				if g.Filled {
					qs[i].Status, qs[i].Answer, qs[i].AnsweredBy = "settled", g.Answer, "human"
				} else {
					qs[i].Askable = true
				}
			}
		}
		if !found {
			q := BriefQuestion{ID: g.ID, Question: g.Text, Kind: "decision", Status: "open", Options: []string{}, Askable: !g.Filled}
			if g.Filled {
				q.Status, q.Answer, q.AnsweredBy = "settled", g.Answer, "human"
			}
			qs = append(qs, q)
		}
	}
	return qs
}

var malformedReasonRE = regexp.MustCompile(`^[a-z_]{1,32}:[a-z_]{1,48}$`)

// malformedReasonOf reads the malformed_reason of attempts/<leaf>/<n>/attempt.json.
// Only a value of the fixed "<class>:<rule>" shape is returned; anything else,
// or a missing file, is "".
func malformedReasonOf(runDir, leaf string, n int) string {
	if leaf == "" || strings.ContainsAny(leaf, `/\`) || strings.Contains(leaf, "..") {
		return ""
	}
	b, err := readSmallFile(filepath.Join(runDir, "attempts", leaf, strconv.Itoa(n), "attempt.json"), 1<<16)
	if err != nil {
		return ""
	}
	var m struct {
		MalformedReason string `json:"malformed_reason"`
	}
	if json.Unmarshal(b, &m) != nil || !malformedReasonRE.MatchString(m.MalformedReason) {
		return ""
	}
	return m.MalformedReason
}
