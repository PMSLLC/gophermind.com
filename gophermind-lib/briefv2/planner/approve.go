package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/human"
)

// approval is approval.json. PlanHash binds it to one exact plan.
type approval struct {
	ApprovedAt string `json:"approved_at"`
	ApprovedBy string `json:"approved_by"`
	PlanHash   string `json:"plan_hash"`
	// UnderstandingHash is the hash of the understanding that was confirmed.
	UnderstandingHash string `json:"understanding_hash,omitempty"`
}

func approveDone(r *run) bool { return exists(r.path(fileApproval)) }

// approve shows the plan to a person and records the decision. Nothing past
// this stage runs without an approval.json whose hash matches the plan.
func (p *Planner) approve(ctx context.Context, r *run) error {
	// A coverage.json that was edited after its stage, or written by an older
	// planner, is held to the same quality gate as the stage itself.
	cov, err := ReadCoverage(r.dir)
	if err != nil {
		return err
	}
	if weak := rootTestDefects(r.reqs, cov.RootTests, qualityOptions(r.brief.Front)); len(weak) > 0 {
		return &QualityError{Items: weak}
	}
	if !confirmDone(r) {
		return errNotConfirmed
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	if bad := openQuestionNodes(dec); len(bad) > 0 {
		return fmt.Errorf("%d node(s) still list open questions (first: %s); answer them with `gophermind brief answer` and resume", len(bad), strings.Join(bad[:min(len(bad), 5)], ", "))
	}
	if err := p.refreshDecisions(r); err != nil {
		return err
	}
	md, hash, err := RenderPlan(r.dir)
	if err != nil {
		return err
	}
	by := "flag"
	if !r.opts.Yes {
		if p.d.Gate == nil {
			return errors.New("the plan needs approval and no human gate is configured; pass --yes to approve it unseen")
		}
		d, err := p.d.Gate.Approve(ctx, human.PlanSummary{Markdown: md, Hash: hash})
		if err != nil {
			return err
		}
		if !d.Approved {
			if d.Note != "" {
				return fmt.Errorf("plan not approved: %s", d.Note)
			}
			return errors.New("plan not approved")
		}
		if by = d.By; by == "" {
			by = "gate"
		}
	}
	u, _ := readUnderstanding(r)
	return writeJSON(r.path(fileApproval), approval{ApprovedAt: p.d.Now().UTC().Format("2006-01-02T15:04:05Z"), ApprovedBy: by, PlanHash: hash, UnderstandingHash: u.Hash})
}

// RenderPlan is the plan as a person approves it, and its SHA-256. It is
// built only from files in the run folder. The hash covers the summary text
// and the bytes of contracts.json, the drafts and classes, coverage.json and
// answers.json, so changing a signature, a file path, a dependency, an
// answer or the coverage mapping after approval changes the hash.
func RenderPlan(runDir string) (markdown, hash string, err error) {
	r := &run{dir: runDir}
	src, err := os.ReadFile(r.path(fileBrief))
	if err != nil {
		return "", "", err
	}
	b, err := brief.Parse(src)
	if err != nil {
		return "", "", err
	}
	reqs := ParseRequirements(src)
	c, err := loadContracts(r)
	if err != nil {
		return "", "", err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return "", "", err
	}
	cov, err := ReadCoverage(runDir)
	if err != nil {
		return "", "", err
	}
	as, err := loadAnswers(r)
	if err != nil {
		return "", "", err
	}
	w, err := planWaves(c.BriefID, c, dec)
	if err != nil {
		return "", "", err
	}

	var s strings.Builder
	f := b.Front
	fmt.Fprintf(&s, "# Plan: %s\n\n", f.Title)
	fmt.Fprintf(&s, "- Run: %s\n- Repository: %s\n- Landing: %s onto %s\n- On ambiguity: %s\n- Contract revision: %d\n\n", f.ID, f.Repo, f.Landing, f.BaseBranch, f.OnAmbiguity, c.Revision)

	deps, err := ReadDependencies(runDir)
	if err != nil {
		return "", "", err
	}
	s.WriteString("## Dependencies\n\n")
	if len(deps) == 0 {
		s.WriteString("none\n")
	}
	for _, d := range deps {
		fmt.Fprintf(&s, "- %s@%s: %s\n", d.Module, d.Version, oneLine(d.Purpose, 300))
	}
	s.WriteString("\n## Components\n\n| Component | Functions | Waves |\n|---|---|---|\n")
	total, top := 0, -1
	for _, comp := range c.Components {
		drafts := dec.Components[comp.ID]
		lo, hi := -1, -1
		for _, d := range drafts {
			id, _ := d["id"].(string)
			wave := w[id]
			if lo < 0 || wave < lo {
				lo = wave
			}
			hi = max(hi, wave)
		}
		span := "none"
		switch {
		case lo >= 0 && lo == hi:
			span = fmt.Sprint(lo)
		case lo >= 0:
			span = fmt.Sprintf("%d to %d", lo, hi)
		}
		fmt.Fprintf(&s, "| %s | %d | %s |\n", comp.ID, len(drafts), span)
		total += len(drafts)
		top = max(top, hi)
	}
	fmt.Fprintf(&s, "\nFunctions: %d in %d wave(s).\n\n", total, top+1)

	s.WriteString("## Assumptions\n\n")
	n := 0
	for _, a := range as.Answers {
		if a.Assumed {
			fmt.Fprintf(&s, "- %s: %s Assumed: %s\n", a.Stage, oneLine(a.Question, 300), oneLine(a.Answer, 300))
			n++
		}
	}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			for _, text := range strList(d["assumptions"]) {
				fmt.Fprintf(&s, "- %v: %s\n", d["id"], oneLine(text, 300))
				n++
			}
		}
	}
	if n == 0 {
		s.WriteString("None.\n")
	}

	s.WriteString("\n## Secrets, environment and network\n\n")
	var secrets, env, hosts []string
	for _, x := range f.Secrets {
		secrets = append(secrets, x.Name)
	}
	for _, x := range f.Env {
		env = append(env, x.Name)
	}
	for _, x := range f.Network {
		h := x.Host
		if x.Critical {
			h += " (critical)"
		}
		hosts = append(hosts, h)
	}
	fmt.Fprintf(&s, "- Secrets (names only): %s\n- Environment: %s\n- Hosts: %s\n", orNone(secrets), orNone(env), orNone(hosts))

	quality := map[string][]string{}
	for _, it := range rootTestDefects(reqs, cov.RootTests, qualityOptions(f)) {
		quality[it.Requirement] = it.Findings
	}
	s.WriteString("\n## Coverage\n\n| Requirement | Text | Covered by | Root tests | Quality |\n|---|---|---|---|---|\n")
	text := map[string]string{}
	for _, q := range reqs {
		text[q.ID] = q.Text
		if q.Kind == ReqFeature {
			text[q.ID] = q.Name
		}
	}
	for _, cv := range cov.Covered {
		fmt.Fprintf(&s, "| %s | %s | %s | %s | %s |\n", cv.Requirement, cell(oneLine(text[cv.Requirement], 80)), cell(orNone(cv.Nodes)), cell(orNone(cv.RootTests)), cell(qualityCell(quality, cv.Requirement)))
	}
	fmt.Fprintf(&s, "\nRequirements covered: %d of %d\n", len(cov.Covered), len(reqs))
	if cov.Rounds > 0 {
		fmt.Fprintf(&s, "Coverage fill rounds: %d\n", cov.Rounds)
	}

	s.WriteString("\n## Root test commands\n\nThese commands were written by a model. Approving this plan approves running them when the build is verified.\n\n")
	if len(cov.RootTests) == 0 {
		s.WriteString("None.\n")
	}
	for _, t := range cov.RootTests {
		fmt.Fprintf(&s, "- %s: %s\n\n      %s\n", t.Requirement, t.Name, strings.ReplaceAll(t.Command, "\n", "\n      "))
	}

	s.WriteString("\n## Warnings\n\n")
	if len(cov.Warnings) == 0 {
		s.WriteString("None.\n")
	}
	warnings := append([]string(nil), cov.Warnings...)
	sort.Strings(warnings)
	for _, wn := range warnings {
		fmt.Fprintf(&s, "- %s\n", wn)
	}

	markdown = s.String()
	// The summary cannot show every signature, file path or dependency edge, so
	// the hash also folds in the files that decide what is written to the
	// repository. Changing any byte of them after approval invalidates it.
	h := sha256.New()
	h.Write([]byte(markdown))
	for _, name := range hashedFiles {
		raw, err := os.ReadFile(r.path(name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		fmt.Fprintf(h, "\x00%s\x00%d\x00", name, len(raw))
		h.Write(raw)
	}
	return markdown, hex.EncodeToString(h.Sum(nil)), nil
}

// hashedFiles are the run folder files whose bytes are part of the plan hash.
var hashedFiles = []string{fileContracts, stateDecomposed, stateClasses, fileCoverage, fileAnswers, fileDependencies, stateUnderstanding, stateEnriched}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

// cell makes text safe inside a markdown table cell.
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// openQuestionNodes lists the function nodes that still list an open question.
func openQuestionNodes(dec decomposed) []string {
	var out []string
	for _, drafts := range dec.Components {
		for _, d := range drafts {
			if len(openQuestionDefects(d)) > 0 {
				id, _ := d["id"].(string)
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// citedByFunctions maps a question id to the function nodes whose decision_ids
// name it.
func citedByFunctions(dec decomposed) map[string][]string {
	out := map[string][]string{}
	for _, drafts := range dec.Components {
		for _, d := range drafts {
			id, _ := d["id"].(string)
			for _, q := range strList(d["decision_ids"]) {
				out[q] = append(out[q], id)
			}
		}
	}
	for q := range out {
		sort.Strings(out[q])
	}
	return out
}

// refreshDecisions rewrites every decision record so it lists the nodes that
// cite it. Task 8 extends it with the component and root records.
func (p *Planner) refreshDecisions(r *run) error {
	s, err := loadQStore(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	cited := citedByFunctions(dec)
	for _, q := range s.Questions {
		if q.Status == qSettled {
			if err := writeDecision(r, q, cited[q.ID]); err != nil {
				return err
			}
		}
	}
	return nil
}
