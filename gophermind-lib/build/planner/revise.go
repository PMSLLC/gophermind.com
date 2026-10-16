package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/packer"
	"gophermind/gophermind-lib/build/router"
	"gophermind/gophermind-lib/build/tree"
)

// ReviseRequest is everything the executor hands the revise stage. It is built
// by code from files and memory, never from a model reply, and every text field
// has been scrubbed of secret values by the caller.
type ReviseRequest struct {
	RunID, RunDir, Repo, NodeID string
	Trigger                     string
	Revision, Remaining         int
	Problem                     string
	History                     []string
	LastFailure                 string
	TestSource, TestSHA256      string
	Refused                     []string
	Secrets                     []string
}

// ReviseOutcome is what a revise call came to.
type ReviseOutcome string

const (
	OutcomeRewritten ReviseOutcome = "rewritten" // Body (and maybe TestSource) are valid and ready to apply
	OutcomeFlagged   ReviseOutcome = "flagged"   // contract_flagged, split_required or tests_locked: a person decides
	OutcomeUnusable  ReviseOutcome = "unusable"  // the reply was refused after the router's repair, or is a no-op
)

// ReviseProposal is the result of Revise. A rewritten proposal is complete: the
// executor applies it without asking the planner anything else.
type ReviseProposal struct {
	Outcome     ReviseOutcome
	Class       string
	Paragraph   string
	Body        []byte
	BodySHA256  string
	Changed     []string
	Assumptions []string
	TestSource  string
	TestSHA256  string
	TestChanged bool
	Model       string
}

// Reviser is the planner's revise stage. The executor calls it through its
// Reviser seam; it makes model calls and writes only saved replies.
type Reviser struct{ p *Planner }

// NewReviser returns a Reviser over the same dependencies the planner has.
func NewReviser(d Deps) *Reviser { return &Reviser{p: New(d)} }

const (
	maxReviseInput      = 2048     // bytes of one text input of the revise prompt (failure, problem, one history line)
	maxReviseTestSource = 64 << 10 // the test file is shown whole up to this size
	removedLine         = "[line removed: secret value]"
)

// scrubInput is the stage's own defense in depth for a text input: the caller
// scrubs too, but a caller bug must not send a secret value to a provider. A
// line that holds one (any form packer.HasSecret knows) is replaced by a
// marker; if the text still spells one across lines it is withheld whole. The
// result is cut to max bytes after the scrub.
func scrubInput(s string, secrets []string, max int) string {
	if packer.HasSecret(s, secrets) {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if packer.HasSecret(l, secrets) {
				lines[i] = removedLine
			}
		}
		s = strings.Join(lines, "\n")
		if packer.HasSecret(s, secrets) {
			s = removedLine
		}
	}
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "\n[cut]"
	}
	return s
}

var reviseIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// openReviseRun is the run view the planner helpers need, read from the folder.
func openReviseRun(req ReviseRequest) (*run, error) {
	if !reviseIDRE.MatchString(req.NodeID) {
		return nil, errors.New("planner: the node id is not a plain id")
	}
	if !filepath.IsAbs(req.RunDir) || req.Repo == "" {
		return nil, errors.New("planner: the run folder and the repository must be absolute")
	}
	src, err := os.ReadFile(filepath.Join(req.RunDir, filepath.FromSlash(fileBrief)))
	if err != nil {
		return nil, errors.New("planner: brief.md is not readable")
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, errors.New("planner: brief.md is not a valid brief")
	}
	return &run{id: req.RunID, dir: req.RunDir, repo: req.Repo, src: src, brief: b, reqs: ParseRequirements(src)}, nil
}

// reviseView is the node as the revise model sees it: no reasoning group ever;
// the component variant adds rationale and assumptions only.
func reviseView(d map[string]any, component bool) map[string]any {
	cp, err := copyDoc(d)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for _, k := range []string{"id", "title", "description", "node_class", "model_tier", "profile_hooks", "construction", "security", "observability", "performance", "portability"} {
		if v, ok := cp[k]; ok {
			out[k] = v
		}
	}
	pick := func(src any, keys ...string) map[string]any {
		m, _ := src.(map[string]any)
		o := map[string]any{}
		for _, k := range keys {
			if v, ok := m[k]; ok {
				o[k] = v
			}
		}
		return o
	}
	out["contract"] = pick(cp["contract"], "package", "file", "signature", "inputs", "outputs", "errors", "side_effects")
	out["context"] = pick(cp["context"], "dependency_signatures", "constraints", "notes")
	var tests []any
	for _, t := range objects(cp["tests"]) {
		tests = append(tests, pick(t, "name", "given", "expect", "polarity", "covers"))
	}
	if tests != nil {
		out["tests"] = tests
	}
	if component {
		out["rationale"] = cp["rationale"]
		out["assumptions"] = cp["assumptions"]
	}
	return out
}

// decisionsNamedBy is the settled decisions the node cites, as decisionsText prints them.
func decisionsNamedBy(qs qstore, ids []string) string {
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	sub := qstore{}
	for _, q := range qs.Questions {
		if keep[q.ID] {
			sub.Questions = append(sub.Questions, q)
		}
	}
	return decisionsText(sub)
}

func bulletList(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	return "- " + strings.Join(lines, "\n- ")
}

func touchesContractShape(changed []string) bool {
	for _, f := range changed {
		if f == "contract.inputs" || f == "contract.outputs" || f == "contract.errors" {
			return true
		}
	}
	return false
}

// revisePrompt builds the revise prompt from the node and the request.
func (p *Planner) revisePrompt(r *run, c *contract.Contracts, orig map[string]any, req ReviseRequest, testFile string) (string, error) {
	component := p.d.Settings.Revise.Context == "component"
	slice, err := c.Slice([]string{req.NodeID}, "")
	if err != nil {
		return "", errors.New("planner: the function is not in contracts.json")
	}
	data := map[string]string{
		"Trigger": req.Trigger, "Revision": strconv.Itoa(req.Revision), "Remaining": strconv.Itoa(req.Remaining),
		"Refused": strings.Join(req.Refused, ", "),
		"Node":    scrubInput(mustJSON(reviseView(orig, component)), req.Secrets, maxReviseTestSource), "Contract": scrubInput(strings.Join(slice, "\n\n"), req.Secrets, maxReviseTestSource),
		"TestFile": testFile, "TestSource": strings.TrimRight(req.TestSource, "\n"),
		"Attempts": bulletList(req.History), "Failure": req.LastFailure, "Problem": req.Problem,
		"Component": "", "BriefSection": "", "Decisions": "",
	}
	if component {
		data["Component"] = "yes"
		comp, _ := orig["parent"].(string)
		data["BriefSection"] = briefSection(r, comp)
		qs, err := loadQStore(r)
		if err != nil {
			return "", err
		}
		data["Decisions"] = decisionsNamedBy(qs, strList(orig["decision_ids"]))
	}
	return render("revise", data)
}

// parsedRevise is what the revise parse closure keeps of the last reply it saw.
type parsedRevise struct {
	rep       reviseReply
	merged    map[string]any
	changed   []string
	needTests bool
	flag      *reviseErr
	secret    bool // the reply held a secret value: refused without a repair request
}

func classOf(err error, fallback string) string {
	var re *reviseErr
	if errors.As(err, &re) {
		return re.class
	}
	return fallback
}

// Revise makes the revise call and, when the tests must follow, the
// Test-writer call, and validates both. It changes nothing in the run folder
// but the saved replies. See the type docs for the three outcomes.
func (v *Reviser) Revise(ctx context.Context, req ReviseRequest) (ReviseProposal, error) {
	p := v.p
	cfg := p.d.Settings.Revise
	r, err := openReviseRun(req)
	if err != nil {
		return ReviseProposal{}, err
	}
	t, err := tree.NewStore(r.dir).Load()
	if err != nil {
		return ReviseProposal{}, errors.New("planner: the task tree is not readable")
	}
	node, ok := t.Nodes[req.NodeID]
	if !ok || node.Kind != tree.KindFunction {
		return ReviseProposal{}, errors.New("planner: the node is not a function node of this run")
	}
	raw, err := node.Marshal()
	if err != nil {
		return ReviseProposal{}, errors.New("planner: the node is not readable")
	}
	var orig map[string]any
	if err := json.Unmarshal(raw, &orig); err != nil {
		return ReviseProposal{}, errors.New("planner: the node is not readable")
	}
	if rev, _ := orig["revision"].(float64); int(rev) != req.Revision {
		return ReviseProposal{}, errors.New("planner: the node is not at the revision the executor is at")
	}
	c, err := loadContracts(r)
	if err != nil {
		return ReviseProposal{}, err
	}
	classes, err := loadClasses(r)
	if err != nil {
		return ReviseProposal{}, err
	}
	class := classes[req.NodeID]
	env, err := p.groupEnvFor(r, c)
	if err != nil {
		return ReviseProposal{}, err
	}
	leafTests, err := ReadLeafTests(r.dir)
	if err != nil {
		return ReviseProposal{}, err
	}
	lt, ok := leafTests[req.NodeID]
	if !ok {
		return ReviseProposal{}, errors.New("planner: the node has no recorded test")
	}
	sreq := req
	sreq.Problem = scrubInput(req.Problem, req.Secrets, maxReviseInput)
	sreq.LastFailure = scrubInput(req.LastFailure, req.Secrets, maxReviseInput)
	sreq.TestSource = scrubInput(req.TestSource, req.Secrets, maxReviseTestSource)
	sreq.Trigger = scrubInput(req.Trigger, req.Secrets, maxReviseInput)
	sreq.History = make([]string, len(req.History))
	for i, h := range req.History {
		sreq.History[i] = scrubInput(h, req.Secrets, maxReviseInput)
	}
	sreq.Refused = make([]string, len(req.Refused))
	for i, h := range req.Refused {
		sreq.Refused[i] = scrubInput(h, req.Secrets, maxReviseInput)
	}
	prompt, err := p.revisePrompt(r, c, orig, sreq, lt.TestFile)
	if err != nil {
		return ReviseProposal{}, err
	}

	next := req.Revision + 1
	secrets := req.Secrets
	heldSecret := func(text string) bool { return replyHoldsSecret(text, secrets) }
	scope := router.ScopeNode
	if cfg.Context == "component" {
		scope = router.ScopeComponent
	}
	var got parsedRevise
	// lastClass is shared by the router's two attempts on purpose: the class of the
	// reply that ends the call is the one left in it, and only a call that ends
	// unusable reads it.
	lastClass := ClassReviseInvalid
	cs := callSpec{stage: "revise:" + req.NodeID, taskType: "revise", nodeID: req.NodeID, nodeClass: class,
		scope: scope, maxTokens: cfg.MaxTokens, tier: router.Tier(cfg.ModelTier), revision: req.Revision, skipSave: heldSecret}
	res, err := p.callRes(ctx, r, cs, prompt, func(text string) error {
		got = parsedRevise{}
		if heldSecret(text) {
			// No error back to the router: its repair request would send the
			// rejected reply, secret included, to the provider again.
			got.secret = true
			return nil
		}
		rep, perr := parseReviseReplyScanned(text, secrets)
		if perr != nil {
			if classOf(perr, "") == ClassSecretInReply {
				// No error back to the router: its repair request would send the
				// rejected reply, secret included, to the provider again.
				got.secret = true
				return nil
			}
			lastClass = classOf(perr, ClassReviseInvalid)
			return perr
		}
		got.rep = rep
		if rep.kind != kindPatch {
			return nil
		}
		merged, perr := mergePatch(orig, rep.patch, c)
		if perr != nil {
			lastClass = classOf(perr, ClassReviseInvalid)
			return perr
		}
		needTests := rep.patch.RewriteTests || touchesContractShape(changedFields(orig, merged))
		changed, perr := checkMerged(orig, merged, rep.patch, mergeEnv{Class: class, Env: env, Secrets: secrets, N: next, TestsRewrite: needTests})
		var re *reviseErr
		if errors.As(perr, &re) && re.class == ClassContractFlagged {
			got.flag = re
			return nil
		}
		if classOf(perr, "") == ClassSecretInReply {
			got.secret = true // as above: never handed to the router's repair
			return nil
		}
		if perr != nil {
			lastClass = classOf(perr, ClassReviseInvalid)
			return perr
		}
		if needTests && cfg.RewriteTests == "off" {
			got.flag = &reviseErr{ClassTestsLocked, "the change needs new tests and revise.rewrite_tests is off"}
			return nil
		}
		got.merged, got.changed, got.needTests = merged, changed, needTests
		return nil
	})
	if err != nil {
		var ce *router.ChainExhausted
		if errors.As(err, &ce) && ce.ParseErr != nil {
			return ReviseProposal{Outcome: OutcomeUnusable, Class: lastClass}, nil
		}
		return ReviseProposal{}, err
	}
	if got.secret {
		return ReviseProposal{Outcome: OutcomeUnusable, Class: ClassSecretInReply}, nil
	}
	flag := func(class, paragraph string) ReviseProposal {
		return ReviseProposal{Outcome: OutcomeFlagged, Class: class, Paragraph: paragraph, Model: res.Entry}
	}
	switch {
	case got.rep.kind == kindContractChange:
		return flag(ClassContractFlagged, got.rep.paragraph), nil
	case got.rep.kind == kindSplit:
		return flag(ClassSplitRequired, got.rep.paragraph), nil
	case got.flag != nil:
		return flag(got.flag.class, got.flag.detail), nil
	}

	testSource, testSHA, testChanged := "", req.TestSHA256, false
	if got.needTests {
		src, refusal, terr := p.reviseTests(ctx, r, c, got.merged, class, lt.TestFile, req, next)
		if terr != nil {
			return ReviseProposal{}, terr
		}
		if refusal != "" {
			return ReviseProposal{Outcome: OutcomeUnusable, Class: refusal}, nil
		}
		if h := hashHex([]byte(src)); h != req.TestSHA256 {
			testSource, testSHA, testChanged = src, h, true
		}
		if !sameJSON(orig["tests"], got.merged["tests"]) {
			got.changed = append(got.changed, "tests")
		}
	}
	if len(got.changed) == 0 && !testChanged {
		return ReviseProposal{Outcome: OutcomeUnusable, Class: ClassReviseNoop}, nil
	}
	rawMerged, err := json.Marshal(got.merged)
	if err != nil {
		return ReviseProposal{Outcome: OutcomeUnusable, Class: ClassReviseInvalid}, nil
	}
	final, err := tree.ParseNode(rawMerged)
	if err != nil {
		return ReviseProposal{Outcome: OutcomeUnusable, Class: ClassReviseInvalid}, nil
	}
	body, err := final.Marshal()
	if err != nil {
		return ReviseProposal{Outcome: OutcomeUnusable, Class: ClassReviseInvalid}, nil
	}
	body = append(body, '\n')
	return ReviseProposal{
		Outcome: OutcomeRewritten, Body: body, BodySHA256: hashHex(body), Changed: got.changed,
		Assumptions: got.rep.patch.Assumptions, TestSource: testSource, TestSHA256: testSHA, TestChanged: testChanged, Model: res.Entry,
	}, nil
}

// reviseTests is the Test-writer call for a revised node: the same prompt and
// the same parser as planning, on the revised contract alone. It sets
// merged["tests"] and the error-to-test links. A reply that fails the parser
// after the router's repair comes back as a refusal class, not an error.
func (p *Planner) reviseTests(ctx context.Context, r *run, c *contract.Contracts, merged map[string]any, class, testFile string, req ReviseRequest, n int) (source, refusal string, err error) {
	id := req.NodeID
	ct, _ := merged["contract"].(map[string]any)
	file, _ := ct["file"].(string)
	pkg, _ := ct["package"].(string)
	funcName := testFuncName(id)
	prompt, err := render("testwriter", map[string]string{
		"Node": scrubInput(mustJSON(testwriterView(merged)), req.Secrets, maxReviseTestSource), "PackageDir": path.Dir(file), "TestFuncName": funcName, "TestFile": testFile})
	if err != nil {
		return "", "", err
	}
	var tests []map[string]any
	var src string
	last := ClassReviseInvalid
	secret := false
	cs := callSpec{stage: fmt.Sprintf("testwrite:%s-r%d", id, n), taskType: "testwrite", nodeID: id, nodeClass: class,
		tier: p.tierFor(strField(merged, "model_tier")), scope: router.ScopeNode, maxTokens: maxTokensTestwrite, revision: req.Revision,
		skipSave: func(text string) bool { return replyHoldsSecret(text, req.Secrets) }}
	err = p.call(ctx, r, cs, prompt, func(text string) error {
		if replyHoldsSecret(text, req.Secrets) {
			// Not an error: the router's repair request would send this reply back.
			secret = true
			return nil
		}
		last = ClassReviseInvalid
		ts, s, perr := parseTestwrite(StripReply(text), ct, pkg, funcName, c.Module, hookList(merged))
		if perr != nil {
			return perr
		}
		if lerr := linkErrorTests(ct, ts); lerr != nil {
			return lerr
		}
		tests, src = ts, s
		return nil
	})
	if err != nil {
		var ce *router.ChainExhausted
		if errors.As(err, &ce) && ce.ParseErr != nil {
			return "", last, nil
		}
		return "", "", err
	}
	if secret {
		return "", ClassSecretInReply, nil
	}
	merged["tests"] = anyObjects(tests)
	return src, "", nil
}

// replyHoldsSecret is the secret scan of a revise or Test-writer reply: the
// reply as sent, every string of its JSON once decoded (an escaped secret is
// still a secret), and the value of every Go string or rune literal inside
// those strings (a test source may spell a secret with Go escapes). It runs
// before the reply is saved, parsed or handed to the router's repair.
func replyHoldsSecret(text string, secrets []string) bool {
	if packer.HasSecret(text, secrets) {
		return true
	}
	var v any
	if json.Unmarshal([]byte(StripReply(strings.TrimSpace(text))), &v) != nil {
		return false
	}
	var parts []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			parts = append(parts, t)
			parts = append(parts, goLiterals(t)...)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for k, e := range t {
				parts = append(parts, k)
				walk(e)
			}
		}
	}
	walk(v)
	return packer.HasSecret(strings.Join(parts, "\n"), secrets)
}

// goLiterals is the unquoted value of every string and rune literal in src,
// read as Go tokens; text that is not Go yields what the scanner finds.
func goLiterals(src string) []string {
	fset := token.NewFileSet()
	f := fset.AddFile("", fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(f, []byte(src), func(token.Position, string) {}, 0)
	var out []string
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.STRING || tok == token.CHAR {
			if v, err := strconv.Unquote(lit); err == nil {
				out = append(out, v)
			}
		}
	}
	return out
}
