package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
)

// CoverageFile is coverage.json: what covers every requirement of the brief.
// It exists only when nothing is uncovered.
type CoverageFile struct {
	Rounds    int        `json:"rounds"` // fill rounds it took
	Covered   []Covered  `json:"covered"`
	RootTests []RootTest `json:"root_tests"`
	Warnings  []string   `json:"warnings"`
	// Serve is how the executor starts the server for the Go acceptance tests;
	// absent when the plan has none.
	Serve *Serve `json:"serve,omitempty"`
}

// ReadCoverage reads a run folder's coverage.json.
func ReadCoverage(runDir string) (CoverageFile, error) {
	var f CoverageFile
	r := &run{dir: runDir}
	found, err := readJSON(r.path(fileCoverage), &f)
	if err != nil {
		return f, err
	}
	if !found {
		return f, fmt.Errorf("planner: %s has no coverage.json; the coverage stage has not finished", runDir)
	}
	return f, nil
}

// CoverageError stops a run whose plan leaves requirements of the brief
// uncovered after every fill round. Nothing is approved in that state.
type CoverageError struct{ Gaps []Gap }

func (e *CoverageError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d requirement(s) of the brief are not covered by the plan:", len(e.Gaps))
	for _, g := range e.Gaps {
		fmt.Fprintf(&b, "\n  %s", gapLine(g))
	}
	return b.String()
}

func gapLine(g Gap) string {
	return fmt.Sprintf("%s (line %d): %s [%s]", g.Requirement, g.Line, oneLine(g.Text, 160), g.Reason)
}

// oneLine puts text on one line and cuts it to n characters.
func oneLine(text string, n int) string {
	return cut(strings.Join(strings.Fields(text), " "), n)
}

func coverageDone(r *run) bool { return exists(r.path(fileCoverage)) }

// coverage is the Coverage stage: the model proposes what covers each
// requirement, code checks it, gaps and root tests that fail the quality gate
// go back for up to max_coverage_rounds fill rounds, and a plan with a gap or a
// weak root test left never reaches approval.
func (p *Planner) coverage(ctx context.Context, r *run) error {
	// A fill round that died after it extended the contract left functions
	// without nodes; write those first.
	if err := p.refreshPlan(ctx, r); err != nil {
		return err
	}
	nodes, err := PlanNodes(r.dir)
	if err != nil {
		return err
	}
	prompt, err := render("coverage", map[string]string{"Requirements": requirementsText(r.reqs), "Nodes": nodesText(nodes), "Harness": harnessContract()})
	if err != nil {
		return err
	}
	var reply CoverageReply
	cs := callSpec{stage: "coverage", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensCoverage}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		cr, err := ParseCoverageReply(StripReply(text), r.reqs)
		if err != nil {
			return err
		}
		reply = cr
		return nil
	}); err != nil {
		return err
	}

	qo := qualityOptions(r.brief.Front)
	reply = reply.withGoAcceptance(r.reqs)
	covered, gaps := CheckCoverage(r.reqs, nodes, reply)
	weak := rootTestDefects(r.reqs, reply.RootTests, qo)
	rounds := 0
	for (len(gaps) > 0 || len(weak) > 0) && rounds < p.d.Settings.Defaults.MaxCoverageRounds {
		rounds++
		for _, g := range gaps {
			p.emit(events.KindCoverageGap, "coverage", "", fmt.Sprintf("round %d: %s", rounds, gapLine(g)))
		}
		for _, w := range weak {
			p.emit(events.KindCoverageGap, "coverage", "", fmt.Sprintf("round %d: %s root test quality [%s]", rounds, w.Requirement, strings.Join(w.Findings, ", ")))
		}
		fill, err := p.coverageFill(ctx, r, gaps, weak, nodes)
		if err != nil {
			return err
		}
		reply = mergeFill(reply, fill, weak).withGoAcceptance(r.reqs)
		if nodes, err = PlanNodes(r.dir); err != nil {
			return err
		}
		covered, gaps = CheckCoverage(r.reqs, nodes, reply)
		weak = rootTestDefects(r.reqs, reply.RootTests, qo)
	}
	if len(gaps) > 0 {
		return &CoverageError{Gaps: gaps}
	}
	if len(weak) > 0 {
		return &QualityError{Items: weak}
	}

	warnings := PathWarnings(r.src, nodes)
	warnings = append(warnings, StrayCommandWarnings(r.src, nodes)...)
	warnings = append(warnings, CommandWarnings(r.reqs, reply)...)
	for _, w := range warnings {
		p.emit(events.KindWarning, "coverage", "", w)
	}
	if reply.RootTests == nil {
		reply.RootTests = []RootTest{}
	}
	// The root node gets its acceptance tests before coverage.json, the file
	// that marks this stage done, is written.
	if err := p.rewriteSkeleton(r, reply.RootTests); err != nil {
		return err
	}
	return writeJSON(r.path(fileCoverage), CoverageFile{Rounds: rounds, Covered: covered, RootTests: reply.RootTests, Warnings: warnings, Serve: reply.Serve})
}

// coverageFill asks the model to close the gaps. Its reply may map gaps to
// nodes, add root tests, and declare contract functions and types that are
// missing; declarations are appended to contracts.json and decomposed before
// it returns.
func (p *Planner) coverageFill(ctx context.Context, r *run, gaps []Gap, weak []QualityItem, nodes []PlanNode) (CoverageReply, error) {
	var doc map[string]any
	if _, err := readJSON(r.path(fileContracts), &doc); err != nil {
		return CoverageReply{}, err
	}
	var gapText, compText strings.Builder
	for _, g := range gaps {
		fmt.Fprintf(&gapText, "%s (line %d): %s\n  reason: %s\n", g.Requirement, g.Line, oneLine(g.Text, 600), g.Reason)
	}
	if len(gaps) == 0 {
		gapText.WriteString("none\n")
	}
	for _, c := range objects(doc["components"]) {
		fmt.Fprintf(&compText, "%v (package %v)\n", c["id"], c["package"])
	}
	itemSchemas, err := contractItemSchemas("types", "functions")
	if err != nil {
		return CoverageReply{}, err
	}
	prompt, err := render("coverage_fill", map[string]string{
		"Gaps": strings.TrimRight(gapText.String(), "\n"), "Weak": weakText(weak), "Harness": harnessContract(), "Nodes": nodesText(nodes),
		"Components": strings.TrimRight(compText.String(), "\n"), "ItemSchemas": itemSchemas})
	if err != nil {
		return CoverageReply{}, err
	}

	var fill CoverageReply
	var next map[string]any // the contract with the reply's declarations added, or nil
	cs := callSpec{stage: "coverage_fill", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensFill}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		cr, cp, err := parseCoverageFill(StripReply(text), r.reqs, doc, r.id)
		if err != nil {
			return err
		}
		fill, next = cr, cp
		return nil
	}); err != nil {
		return CoverageReply{}, err
	}
	if next != nil {
		if err := writeJSON(r.path(fileContracts), next); err != nil {
			return CoverageReply{}, err
		}
		if err := p.refreshPlan(ctx, r); err != nil {
			return CoverageReply{}, err
		}
	}
	return fill, nil
}

// parseCoverageFill decodes a fill reply. Beside the mapping it returns the
// contract with the reply's declared types and functions added (revision plus
// one) after the same validation as the Contract stage, or nil when the reply
// declares nothing. No error quotes the reply.
func parseCoverageFill(raw string, reqs []Requirement, doc map[string]any, briefID string) (CoverageReply, map[string]any, error) {
	cr, err := ParseCoverageReply(raw, reqs)
	if err != nil {
		return CoverageReply{}, nil, err
	}
	var add struct {
		Types     []map[string]any `json:"types"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(raw), &add); err != nil {
		return CoverageReply{}, nil, fmt.Errorf("coverage fill reply is not usable (%s)", jsonErr(err))
	}
	if len(add.Types)+len(add.Functions) == 0 {
		return cr, nil, nil
	}
	cp, err := copyDoc(doc)
	if err != nil {
		return CoverageReply{}, nil, err
	}
	types, _ := cp["types"].([]any)
	for _, t := range add.Types {
		types = append(types, t)
	}
	fns, _ := cp["functions"].([]any)
	for _, f := range add.Functions {
		fns = append(fns, f)
	}
	rev, _ := cp["revision"].(float64)
	cp["types"], cp["functions"], cp["revision"] = types, fns, int(rev)+1
	if _, err := validateContractDoc(cp, briefID); err != nil {
		return CoverageReply{}, nil, fmt.Errorf("coverage fill reply: %w", err)
	}
	return cr, cp, nil
}

// refreshPlan brings the drafts and the skeleton in line with contracts.json:
// it decomposes every function that has no draft and rewrites the root and
// component nodes. With nothing missing it makes no model call.
func (p *Planner) refreshPlan(ctx context.Context, r *run) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	before := 0
	for _, d := range dec.Components {
		before += len(d)
	}
	if before == len(c.Functions) {
		return nil
	}
	if err := p.decomposeMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	if err := p.enrichMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	if _, err := planWaves(r.id, c, dec); err != nil {
		return err
	}
	return writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, nil)
}

// rewriteSkeleton writes the root and component nodes again, the root with
// the given acceptance tests.
func (p *Planner) rewriteSkeleton(r *run, rootTests []RootTest) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	return writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, rootTests)
}

// requirementsText lists the requirements for the coverage prompt, one per
// line. A feature is shown by its heading and the start of its text.
func requirementsText(reqs []Requirement) string {
	var b strings.Builder
	for _, q := range reqs {
		text := oneLine(q.Text, 2000)
		if q.Kind == ReqFeature {
			text = q.Name + ": " + oneLine(q.Text, 300)
		}
		fmt.Fprintf(&b, "%s (%s, line %d): %s\n", q.ID, q.Kind, q.Line, text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func nodesText(nodes []PlanNode) string {
	var b strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", n.ID, n.Kind, n.Parent, n.Title, n.File)
	}
	return strings.TrimRight(b.String(), "\n")
}
