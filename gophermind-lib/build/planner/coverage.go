package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/router"
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

// ErrNoCoverage is wrapped by ReadCoverage when coverage.json does not exist,
// so a caller can tell a plan that has not been covered yet from a corrupt file.
var ErrNoCoverage = errors.New("no coverage.json")

// ReadCoverage reads a run folder's coverage.json.
func ReadCoverage(runDir string) (CoverageFile, error) {
	var f CoverageFile
	r := &run{dir: runDir}
	found, err := readJSON(r.path(fileCoverage), &f)
	if err != nil {
		return f, err
	}
	if !found {
		return f, fmt.Errorf("planner: %s has no coverage.json; the coverage stage has not finished: %w", runDir, ErrNoCoverage)
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
	cs := callSpec{stage: "coverage", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensCoverage, jsonObject: true, schema: coverageReplySchema}
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

	// The binaries come from the contract as it stands: a fill round may add
	// functions, so the options are read again each time.
	quality := func(tests []RootTest) ([]QualityItem, error) {
		c, err := loadContracts(r)
		if err != nil {
			return nil, err
		}
		return rootTestDefects(r.reqs, tests, qualityOptions(r.brief.Front, c)), nil
	}
	reply = reply.withGoAcceptance(r.reqs)
	covered, gaps := CheckCoverage(r.reqs, nodes, reply)
	// A mapping is not the behaviour: each feature's output literals must be
	// carried by a node mapped to it, or the fill round is asked for them.
	bins := func() []string {
		c, err := loadContracts(r)
		if err != nil {
			return nil
		}
		return contractBins(c)
	}
	gaps = append(gaps, literalGaps(r.reqs, covered, nodes, bins()...)...)
	weak, err := quality(reply.RootTests)
	if err != nil {
		return err
	}
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
		gaps = append(gaps, literalGaps(r.reqs, covered, nodes, bins()...)...)
		if weak, err = quality(reply.RootTests); err != nil {
			return err
		}
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
	var tests []string
	cs := callSpec{stage: "coverage_fill", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensFill}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		text, tests = dropTestFunctionsText(StripReply(text))
		cr, cp, err := parseCoverageFill(text, r.reqs, doc, r.rootID())
		if err != nil {
			return err
		}
		fill, next = cr, cp
		return nil
	}); err != nil {
		return CoverageReply{}, err
	}
	if len(tests) > 0 {
		p.emit(events.KindWarning, "coverage", "", testDroppedText("coverage_fill", tests))
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
		return CoverageReply{}, nil, fmt.Errorf("coverage fill reply is not usable (%s)", jsonErrIn(err, raw))
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
	if err := fillDuplicates(objects(doc["functions"]), add.Functions); err != nil {
		return CoverageReply{}, nil, err
	}
	fns, _ := cp["functions"].([]any)
	for _, f := range add.Functions {
		fns = append(fns, f)
	}
	rev, _ := cp["revision"].(float64)
	cp["types"], cp["functions"], cp["revision"] = types, fns, int(rev)+1
	if _, err := repairContractShape(cp); err != nil {
		return CoverageReply{}, nil, fmt.Errorf("coverage fill reply: %w", err)
	}
	if _, err := validateContractDoc(cp, briefID); err != nil {
		return CoverageReply{}, nil, fmt.Errorf("coverage fill reply: %w", err)
	}
	return cr, cp, nil
}

// fillDuplicates refuses a fill function whose name (with its receiver type,
// for a method) and package an existing function already has: the fill round
// of run hello-v1.1-20261106-r007 declared Greeting and Run a second time
// under new ids. The error names only ids, the name and the package.
func fillDuplicates(existing, added []map[string]any) error {
	key := func(f map[string]any) (string, string, bool) {
		m := funcNameRE.FindStringSubmatch(strings.TrimSpace(strField(f, "signature")))
		if m == nil {
			return "", "", false
		}
		recv := ""
		if _, r, ok := strings.Cut(m[1], "("); ok {
			r, _, _ = strings.Cut(r, ")")
			if fs := strings.Fields(r); len(fs) > 0 {
				recv = fs[len(fs)-1] + "."
			}
		}
		return strField(f, "package"), recv + m[2], true
	}
	have := map[[2]string]string{}
	for _, f := range existing {
		if pkg, name, ok := key(f); ok {
			have[[2]string{pkg, name}] = strField(f, "id")
		}
	}
	for _, f := range added {
		pkg, name, ok := key(f)
		if !ok {
			continue
		}
		if id, dup := have[[2]string{pkg, name}]; dup {
			return fmt.Errorf("coverage fill reply: function %s duplicates %s (%s in %s); map the requirement to %s instead of declaring it again", strField(f, "id"), id, name, pkg, id)
		}
	}
	return nil
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
	if _, err := planWaves(r.rootID(), c, dec); err != nil {
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
// line; kind-test requirements are the test writer's and are left out. A
// feature is shown by its heading and the start of its text.
func requirementsText(reqs []Requirement) string {
	var b strings.Builder
	for _, q := range CoverageRequirements(reqs) {
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
