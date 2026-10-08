package projectrun

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
)

// BinaryInfo stamps the report with the binary that produced it.
type BinaryInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// RepoInfo is the repository the run built in.
type RepoInfo struct {
	Path        string `json:"path"`
	BriefRepo   string `json:"brief_repo"`
	BaseBranch  string `json:"base_branch"`
	HeadAtStart string `json:"head_at_start"`
}

// ProviderInfo names a provider and the host that answered, host only.
type ProviderInfo struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Fallback bool   `json:"fallback"`
}

// ClarifyDefault is a question settled by its recommendation. It appears in
// _state/project.json only, never in the printed report.
type ClarifyDefault struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// AmbiguityInfo records how open questions were settled.
type AmbiguityInfo struct {
	BriefSetting            string           `json:"brief_setting"`
	Effective               string           `json:"effective"`
	ClarifyDefaulted        []ClarifyDefault `json:"clarify_defaulted"`
	ByAnsweredBy            map[string]int   `json:"by_answered_by"`
	Rounds                  int              `json:"rounds"`
	ClarifyCalls            int              `json:"clarify_calls"`
	ConservativeAssumptions int              `json:"conservative_assumptions"`
	MilestoneApprovals      bool             `json:"milestone_approvals"`
}

// UnderstandingInfo is the confirmed understanding (planner.ReadUnderstanding).
type UnderstandingInfo struct {
	ConfirmedBy string `json:"confirmed_by"`
	Hash        string `json:"hash"`
}

// ApprovalInfo is the plan approval (planner.ReadApproval).
type ApprovalInfo struct {
	By                string `json:"by"`
	PlanHash          string `json:"plan_hash"`
	UnderstandingHash string `json:"understanding_hash"`
}

// PlanInfo sizes the plan. AcceptanceTotal is the number of acceptance
// requirements, for the proof lines when the executor did not run.
type PlanInfo struct {
	Functions           int             `json:"functions"`
	Waves               int             `json:"waves"`
	Warnings            int             `json:"warnings"`
	RequirementsCovered report.Coverage `json:"requirements_covered"`
	AcceptanceTotal     int             `json:"acceptance_total"`
}

// StageInfo is one planner stage and how it ended.
type StageInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ProjectReport is the whole account of one /project run.
type ProjectReport struct {
	RunID               string            `json:"run_id"`
	RunDir              string            `json:"run_dir"` // as shown to the reader, for the pointer to _state/project.json
	Title               string            `json:"title"`
	Binary              BinaryInfo        `json:"binary"`
	Mode                string            `json:"mode"` // unattended | attended
	Graded              bool              `json:"graded"`
	Resumed             bool              `json:"resumed"`
	Repo                RepoInfo          `json:"repo"`
	Preflight           []Check           `json:"preflight"`
	Providers           []ProviderInfo    `json:"providers"`
	Secrets             []Provisioned     `json:"secrets"`
	Ambiguity           AmbiguityInfo     `json:"ambiguity"`
	Understanding       UnderstandingInfo `json:"understanding"`
	Approval            ApprovalInfo      `json:"approval"`
	Plan                PlanInfo          `json:"plan"`
	Warnings            Counts            `json:"planner_warnings"`
	PlannerWarningLines []string          `json:"planner_warning_lines"`
	Stages              []StageInfo       `json:"stages"`
	ByNodeClass         []ClassStat       `json:"by_node_class"`
	Executor            *report.Report    `json:"executor,omitempty"`
	Status              string            `json:"status"`
	StopReason          string            `json:"stop_reason"`
	ExitCode            int               `json:"exit_code"`
	StartedAt           string            `json:"started_at"`
	FinishedAt          string            `json:"finished_at"`
}

// maxFieldRunes bounds every free-text string written to project.json.
const maxFieldRunes = 200

// capText keeps the first line of s, cut to maxFieldRunes runes without
// splitting a rune.
func capText(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) > maxFieldRunes {
		s = string([]rune(s)[:maxFieldRunes])
	}
	return strings.ToValidUTF8(s, "?")
}

// hostOnly reduces whatever a caller holds (a URL, host:port, a bare host) to
// the host name: no scheme, credentials, port, path or query.
func hostOnly(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "//" + s
	}
	if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
		return oneLine(u.Hostname())
	}
	return "unknown"
}

func hashPrefix(h string) string {
	if len(h) > 12 {
		h = h[:12]
	}
	return oneLine(h)
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Text is the printed report. It carries ids, counts and names only: never a
// question, an answer, a secret value or model text. Its last two lines are
// the proof lines.
func (p *ProjectReport) Text() string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	line("gophermind %s (commit %s, built %s)", oneLine(p.Binary.Version), oneLine(p.Binary.Commit), oneLine(p.Binary.Date))
	line("binary: %s", oneLine(p.Binary.Path))
	title := oneLine(capText(p.Title))
	line("project: %s %s", oneLine(p.RunID), title)
	line("mode: %s, graded: %s, resumed: %s", oneLine(p.Mode), yn(p.Graded), yn(p.Resumed))
	line("repo: %s (brief repo: %s)", oneLine(p.Repo.Path), oneLine(p.Repo.BriefRepo))
	for _, pr := range p.Providers {
		fb := ""
		if pr.Fallback {
			fb = " (fallback)"
		}
		line("model server: %s answered on %s%s", oneLine(pr.Name), hostOnly(pr.Host), fb)
	}
	failed := len(Failed(p.Preflight))
	if failed == 0 {
		line("preflight: ok (%d checks)", len(p.Preflight))
	} else {
		line("preflight: %d of %d checks failed", failed, len(p.Preflight))
	}
	if len(p.Secrets) > 0 {
		parts := make([]string, 0, len(p.Secrets))
		for _, s := range p.Secrets {
			parts = append(parts, oneLine(s.Name)+" "+oneLine(string(s.Source)))
		}
		line("secrets: %s", strings.Join(parts, "; "))
	} else {
		line("secrets: none")
	}
	a := p.Ambiguity
	if p.Mode == "unattended" {
		ids := make([]string, 0, len(a.ClarifyDefaulted))
		for _, c := range a.ClarifyDefaulted {
			ids = append(ids, oneLine(capText(c.ID)))
		}
		idList := ""
		if len(ids) > 0 {
			idList = " (" + strings.Join(ids, ", ") + ")"
		}
		line("on_ambiguity=%s overridden by unattended policy: %d clarify question(s) answered by their recommendations%s in %d round(s), %d by the fact probe; %d conservative assumption(s); text in %s/_state/project.json",
			oneLine(a.BriefSetting), len(a.ClarifyDefaulted), idList, a.Rounds, a.ByAnsweredBy["probe"], a.ConservativeAssumptions, oneLine(p.RunDir))
	} else {
		line("on_ambiguity=%s, attended: %d question(s) answered by a person in %d round(s); text in %s/_state/project.json",
			oneLine(a.BriefSetting), a.ByAnsweredBy["human"]+a.ByAnsweredBy["accepted"], a.Rounds, oneLine(p.RunDir))
	}
	line("understanding: confirmed by %s, hash %s", oneLine(p.Understanding.ConfirmedBy), hashPrefix(p.Understanding.Hash))
	if a.MilestoneApprovals {
		line("milestone_approvals: declared; the executor has no milestone gate; covered by the unattended plan approval")
	}
	w := p.Warnings
	line("planner warnings: duplicates ignored %d, leaf_defaulted %d, doc_defaulted %d, leaf_normalized %d, outline_id_normalized %d",
		w.DuplicatesIgnored, w.LeafDefaulted, w.DocDefaulted, w.LeafNormalized, w.OutlineIDNormalized)
	line("approval: %s, plan %s, understanding %s", oneLine(p.Approval.By), hashPrefix(p.Approval.PlanHash), hashPrefix(p.Approval.UnderstandingHash))
	line("plan: %d functions in %d wave(s)", p.Plan.Functions, p.Plan.Waves)
	if len(p.ByNodeClass) == 0 {
		line("node classes: executor did not run")
	}
	for _, c := range p.ByNodeClass {
		line("node class %s: %d leaves, %d verified, %d first try, %d attempts", oneLine(c.Class), c.Leaves, c.Verified, c.FirstTryWins, c.Attempts)
	}
	if p.Executor != nil {
		if p.Graded && p.Executor.Resumed {
			line("graded: INVALID (the run resumed)")
		}
		b.WriteString(p.Executor.Summary())
		return b.String()
	}
	line("stopped: %s %s", oneLine(p.Status), oneLine(p.StopReason))
	line("Requirements covered: %d of %d", p.Plan.RequirementsCovered.Covered, p.Plan.RequirementsCovered.Total)
	line("Acceptance passed: 0 of %d", p.Plan.AcceptanceTotal)
	return b.String()
}

// WriteReport writes <runDir>/_state/project.json, mode 0600, through a
// temporary file and a rename. It sits under _state because tree.Store.Load
// would parse a root-level .json file as a node. Hosts are reduced to host
// names before they are written.
func WriteReport(runDir string, p *ProjectReport) error {
	cp := *p
	cp.Providers = append([]ProviderInfo(nil), p.Providers...)
	for i := range cp.Providers {
		cp.Providers[i].Host = hostOnly(cp.Providers[i].Host)
	}
	cp.Title = capText(p.Title)
	cp.PlannerWarningLines = make([]string, len(p.PlannerWarningLines))
	for i, l := range p.PlannerWarningLines {
		cp.PlannerWarningLines[i] = capText(l)
	}
	cp.Ambiguity.ClarifyDefaulted = make([]ClarifyDefault, len(p.Ambiguity.ClarifyDefaulted))
	for i, c := range p.Ambiguity.ClarifyDefaulted {
		cp.Ambiguity.ClarifyDefaulted[i] = ClarifyDefault{ID: capText(c.ID), Question: capText(c.Question), Answer: capText(c.Answer)}
	}
	raw, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return err
	}
	return runfs.WriteFileAtomic(filepath.Join(runDir, "_state", "project.json"), append(raw, '\n'))
}
