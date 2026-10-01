package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gophermind/gophermind-lib/briefv2/tree"
)

// decomposeBatch is how many functions one Decompose call writes nodes for.
// It bounds the size of one reply, not the size of the plan.
const decomposeBatch = 8

// decomposed is _state/decomposed.json: every function node as far as it can
// be written before its tests exist, by component, in the order written.
type decomposed struct {
	Components map[string][]map[string]any `json:"components"`
	Done       bool                        `json:"done"`
	// Pending are the nodes whose reply failed a check: kept with their
	// defects and the passes spent on them until they are repaired.
	Pending []pendingDraft `json:"pending,omitempty"`
}

func loadDecomposed(r *run) (decomposed, error) {
	d := decomposed{}
	_, err := readJSON(r.path(stateDecomposed), &d)
	if d.Components == nil {
		d.Components = map[string][]map[string]any{}
	}
	return d, err
}

func loadClasses(r *run) (map[string]string, error) {
	classes := map[string]string{}
	_, err := readJSON(r.path(stateClasses), &classes)
	return classes, err
}

func loadContracts(r *run) (*contract.Contracts, error) {
	raw, err := os.ReadFile(r.path(fileContracts))
	if err != nil {
		return nil, err
	}
	return contract.Load(raw)
}

func decomposeDone(r *run) bool {
	var d decomposed
	found, err := readJSON(r.path(stateDecomposed), &d)
	return err == nil && found && d.Done
}

// decompose is the Decompose stage: one function node draft per contract
// function, then the root and component nodes.
func (p *Planner) decompose(ctx context.Context, r *run) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	if err := p.decomposeMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	if _, err := planWaves(r.id, c, dec); err != nil {
		return err
	}
	if err := writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, nil); err != nil {
		return err
	}
	dec.Done = true
	return writeJSON(r.path(stateDecomposed), dec)
}

// decomposeMissing writes a draft for every contract function that has none
// yet, component by component, at most decomposeBatch functions per call.
// Progress is saved after every call, so a resume (and a coverage round that
// added functions to the contract) only asks for what is missing.
func (p *Planner) decomposeMissing(ctx context.Context, r *run, c *contract.Contracts, dec *decomposed) error {
	classes, err := loadClasses(r)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, drafts := range dec.Components {
		for _, d := range drafts {
			id, _ := d["id"].(string)
			have[id] = true
		}
	}
	for _, pd := range dec.Pending {
		have[pd.ID] = true
	}
	for _, comp := range c.Components {
		var missing []contract.Function
		for _, f := range c.Functions {
			if f.Component == comp.ID && !have[f.ID] {
				missing = append(missing, f)
			}
		}
		for len(missing) > 0 {
			n := min(decomposeBatch, len(missing))
			batch := missing[:n]
			missing = missing[n:]
			res, err := p.decomposeCall(ctx, r, c, comp, batch)
			if err != nil {
				return err
			}
			p.noteLeafNormalized(res.notes)
			// The nodes that passed are stored now; the others wait for their repair.
			dec.Components[comp.ID] = append(dec.Components[comp.ID], res.good...)
			dec.Pending = append(dec.Pending, res.bad...)
			for id, class := range res.classes {
				classes[id] = class
			}
			if err := writeJSON(r.path(stateClasses), classes); err != nil {
				return err
			}
			if err := writeJSON(r.path(stateDecomposed), dec); err != nil {
				return err
			}
		}
	}
	if err := p.repairDrafts(ctx, r, c, dec, classes); err != nil {
		return err
	}
	dropped := breakDependencyCycles(c, *dec)
	for _, w := range dropped {
		p.emit(events.KindWarning, "decompose", "", w)
	}
	if len(dropped) > 0 {
		return writeJSON(r.path(stateDecomposed), dec)
	}
	return nil
}

// breakDependencyCycles removes, deterministically, the back edges of the
// function dependency graph. Two functions may call each other (the contract
// allows it), but a wave plan cannot. Nodes are visited in contract order and
// each node's dependencies in sorted order; an edge into a node still being
// visited is the back edge and is dropped. Nothing is lost: the dropped
// function's signature still reaches the node through dependency_signatures.
// It returns one warning per dropped edge, naming ids only.
func breakDependencyCycles(c *contract.Contracts, dec decomposed) []string {
	drafts := map[string]map[string]any{}
	for _, list := range dec.Components {
		for _, d := range list {
			id, _ := d["id"].(string)
			drafts[id] = d
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var warnings []string
	var visit func(id string)
	visit = func(id string) {
		state[id] = visiting
		deps := strList(drafts[id]["depends_on"])
		sort.Strings(deps)
		kept := []string{}
		for _, dep := range deps {
			if drafts[dep] == nil {
				kept = append(kept, dep)
				continue
			}
			if state[dep] == visiting {
				warnings = append(warnings, fmt.Sprintf("functions %s and %s depend on each other; %s no longer waits for %s", id, dep, id, dep))
				continue
			}
			kept = append(kept, dep)
			if state[dep] == 0 {
				visit(dep)
			}
		}
		if len(kept) != len(deps) {
			drafts[id]["depends_on"] = kept
		}
		state[id] = done
	}
	for _, f := range c.Functions {
		if drafts[f.ID] != nil && state[f.ID] == 0 {
			visit(f.ID)
		}
	}
	return warnings
}

// decomposeCall asks for the nodes of one batch of one component's functions.
func (p *Planner) decomposeCall(ctx context.Context, r *run, c *contract.Contracts, comp contract.Component, batch []contract.Function) (draftResult, error) {
	var uses []string
	for _, f := range batch {
		uses = append(uses, f.Uses...)
	}
	slice, err := c.Slice(uses, "")
	if err != nil {
		return draftResult{}, err
	}
	sliceText := strings.Join(slice, "\n\n")
	if sliceText == "" {
		sliceText = "(these functions reference no other contract entry)"
	}
	prompt, err := render("decompose", map[string]string{
		"Component":     mustJSON(map[string]any{"component": comp, "functions": batch}),
		"ContractSlice": sliceText,
		"BriefSection":  briefSection(r, comp.ID),
	})
	if err != nil {
		return draftResult{}, err
	}
	var res draftResult
	cs := callSpec{stage: "decompose:" + comp.ID, taskType: "decompose", scope: router.ScopeComponent, maxTokens: maxTokensDecompose}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		d, err := splitDrafts(StripReply(text), comp.ID, batch, c, r)
		if err != nil {
			return err
		}
		res = d
		return nil
	})
	if err != nil {
		return draftResult{}, err
	}
	return res, nil
}

// nodeClasses is the closed list a draft's node_class must come from.
var nodeClasses = []string{"pure", "validation", "handler", "client", "storage", "concurrency", "wiring", "other"}

// draftDefect kinds, a fixed vocabulary: they name what is wrong with a node
// without quoting the reply, and say which fields to ask for again.
const (
	defMissingNode   = "missing_node"
	defNodeClass     = "node_class"
	defTitle         = "title"
	defDescription   = "description"
	defContract      = "contract_missing"
	defInputsMissing = "inputs_missing"
	defInputType     = "input_type"
	defOutputsCount  = "outputs_count"
	defOutputType    = "output_type"
	defErrorsMissing = "errors_missing"
	defErrorsPartial = "errors_incomplete"
	defDepends       = "depends_unknown"
	defSchema        = "schema"
)

// nodeEnv is what normalising one node needs besides the node.
type nodeEnv struct {
	component, ref string
	constraints    []any
	isFunction     map[string]bool
	known          map[string]bool
	c              *contract.Contracts
	// notes records, per node checked, the shape changes made before the checks
	// (field and kind only); details is the schema failure of the node last
	// checked, as field and keyword.
	notes   []string
	details []string
}

func newNodeEnv(component string, c *contract.Contracts, r *run) *nodeEnv {
	e := &nodeEnv{component: component, c: c, isFunction: map[string]bool{}, known: map[string]bool{}, ref: "#architecture"}
	for _, f := range c.Functions {
		e.isFunction[f.ID], e.known[f.ID] = true, true
	}
	for _, t := range c.Types {
		e.known[t.ID] = true
	}
	for _, q := range r.reqs {
		if q.Kind == ReqConstraint {
			e.constraints = append(e.constraints, q.Text)
		}
	}
	for _, f := range r.brief.Features {
		if slug(f.Name) == component {
			e.ref = "#features/" + component
		}
	}
	return e
}

// normalizeNode turns one node of a reply into a draft: every field the
// harness owns is set or overwritten by code and every leaf check applied. It
// returns the node_class the model gave (not part of the node document), the
// defects found (kinds, with a message for each that quotes nothing of the
// reply) and an error only when the contract itself is unusable. d is changed
// in place; on defects it is not a draft.
func (e *nodeEnv) normalizeNode(f contract.Function, d map[string]any) (class string, kinds, msgs []string, err error) {
	defect := func(kind, format string, a ...any) {
		for _, k := range kinds {
			if k == kind {
				return
			}
		}
		kinds = append(kinds, kind)
		msgs = append(msgs, fmt.Sprintf("node %s: "+format, append([]any{f.ID}, a...)...))
	}
	e.details = nil
	class, _ = d["node_class"].(string)
	if !contains(nodeClasses, class) {
		// A node class written into model_tier is a slip of the field, not a refusal.
		if t, _ := d["model_tier"].(string); contains(nodeClasses, strings.ToLower(strings.TrimSpace(t))) {
			class = strings.ToLower(strings.TrimSpace(t))
			e.notes = append(e.notes, f.ID+" node_class from model_tier")
		}
	}
	if !contains(nodeClasses, class) {
		defect(defNodeClass, "node_class (%d bytes) is not one of %s", len(class), strings.Join(nodeClasses, ", "))
	}
	for _, k := range []string{"node_class", "tests", "wave", "claim", "attempts", "result"} {
		delete(d, k)
	}
	fnName := ""
	if fd, perr := parseSignature(f.Signature); perr == nil {
		fnName = fd.Name.Name
	}
	normalizeShape(d, fnName, func(field, what string) { e.notes = append(e.notes, f.ID+" "+field+" "+what) })
	d["spec_version"], d["kind"], d["parent"] = "2.0", "function", e.component
	d["status"], d["revision"], d["brief_ref"] = "pending", 0, e.ref
	if t, _ := d["title"].(string); len([]rune(t)) > 120 {
		d["title"] = string([]rune(t)[:120])
	}
	if t, _ := d["title"].(string); strings.TrimSpace(t) == "" {
		defect(defTitle, "title is missing or empty")
	}
	if t, _ := d["description"].(string); strings.TrimSpace(t) == "" {
		defect(defDescription, "description is missing or empty")
	}
	if _, ok := d["model_tier"]; !ok {
		d["model_tier"] = "standard"
	}
	ct, ok := d["contract"].(map[string]any)
	if !ok {
		defect(defContract, "contract is missing")
	} else {
		ct["package"], ct["file"], ct["signature"] = f.Package, f.File, f.Signature
		fd, perr := parseSignature(f.Signature)
		if perr != nil {
			return class, kinds, msgs, fmt.Errorf("node %s: %w", f.ID, perr)
		}
		declared := map[string]bool{}
		if fd.Type.Params != nil {
			for _, field := range fd.Type.Params.List {
				for _, name := range field.Names {
					declared[name.Name] = true
				}
			}
		}
		for _, ld := range leafDefects(ct, fd, declared) {
			defect(ld.kind, "%s", ld.msg)
		}
	}

	// depends_on holds node ids only; types reach the node through its
	// dependency signatures.
	deps := append(strList(d["depends_on"]), f.Uses...)
	unknown := false
	for i, id := range deps {
		if !e.known[id] {
			defect(defDepends, "depends_on entry %d (%d bytes) is an unknown id", i+1, len(id))
			unknown = true
		}
	}
	if len(kinds) > 0 || unknown {
		return class, kinds, msgs, nil
	}
	sigs, serr := e.c.Slice(deps, f.ID)
	if serr != nil {
		defect(defDepends, "depends_on: %v", serr)
		return class, kinds, msgs, nil
	}
	fnDeps := []string{}
	for _, id := range deps {
		if e.isFunction[id] && id != f.ID && !contains(fnDeps, id) {
			fnDeps = append(fnDeps, id)
		}
	}
	sort.Strings(fnDeps)
	d["depends_on"] = fnDeps
	nctx, _ := d["context"].(map[string]any)
	if nctx == nil {
		nctx = map[string]any{}
	}
	nctx["dependency_signatures"] = sigs
	if _, ok := nctx["constraints"]; !ok && len(e.constraints) > 0 {
		nctx["constraints"] = e.constraints
	}
	d["context"] = nctx
	if verr := validateDraft(d); verr != nil {
		defect(defSchema, "%v", verr)
		if ve := draftSchemaFailure(d); ve != nil {
			e.details = schemaDetails(ve)
		}
	}
	return class, kinds, msgs, nil
}

// draftResult is what a Decompose reply held: the nodes that passed, the nodes
// that did not (to be asked for again), and the noise that was ignored.
type draftResult struct {
	good    []map[string]any
	classes map[string]string
	bad     []pendingDraft
	noise   []string
	notes   []string // shape changes made before the checks
}

// splitDrafts reads a Decompose reply for the functions asked for. A node that
// fails a check does not discard the others: it comes back in bad with its
// defects. A node nobody asked for, or a second node for the same function, is
// noise and ignored (the first is kept). Only a reply that is not a JSON array
// of objects is an error, which makes it an unusable reply.
func splitDrafts(text, component string, fns []contract.Function, c *contract.Contracts, r *run) (draftResult, error) {
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return draftResult{}, fmt.Errorf("decompose reply is not a JSON array of nodes (%s)", jsonErr(err))
	}
	res := draftResult{classes: map[string]string{}}
	want := map[string]bool{}
	for _, f := range fns {
		want[f.ID] = true
	}
	byID := map[string]map[string]any{}
	for i, d := range got {
		id, _ := d["id"].(string)
		switch {
		case !want[id]:
			res.noise = append(res.noise, fmt.Sprintf("decompose reply node %d (id of %d bytes) is not one of the functions asked for", i+1, len(id)))
		case byID[id] != nil:
			res.noise = append(res.noise, fmt.Sprintf("decompose reply has two nodes for %s", id))
		default:
			byID[id] = d
		}
	}
	env := newNodeEnv(component, c, r)
	for _, f := range fns {
		d := byID[f.ID]
		if d == nil {
			res.bad = append(res.bad, pendingDraft{Component: component, ID: f.ID, Defects: []string{defMissingNode},
				msg: fmt.Sprintf("decompose reply has no node for %s", f.ID)})
			continue
		}
		raw, err := copyDoc(d)
		if err != nil {
			return draftResult{}, err
		}
		class, kinds, msgs, err := env.normalizeNode(f, d)
		if err != nil {
			return draftResult{}, err
		}
		if len(kinds) > 0 {
			res.bad = append(res.bad, pendingDraft{Component: component, ID: f.ID, Raw: raw, Defects: kinds,
				Details: append([]string(nil), env.details...), msg: strings.Join(msgs, "; ")})
			continue
		}
		res.classes[f.ID] = class
		res.good = append(res.good, d)
	}
	res.notes = env.notes
	return res, nil
}

// leafDefect is one thing wrong with a leaf's contract, with its kind.
type leafDefect struct{ kind, msg string }

// checkLeaf is what every leaf must carry before it may enter the plan: an
// input for every parameter, an output for every result, and an error entry
// when the function can fail. It returns the first defect as an error.
func checkLeaf(ct map[string]any, fd *ast.FuncDecl, declared map[string]bool) error {
	if ds := leafDefects(ct, fd, declared); len(ds) > 0 {
		return errors.New(ds[0].msg)
	}
	return nil
}

// leafDefects lists every leaf check that fails, in the order of checkLeaf.
func leafDefects(ct map[string]any, fd *ast.FuncDecl, declared map[string]bool) []leafDefect {
	var out []leafDefect
	add := func(kind, format string, a ...any) { out = append(out, leafDefect{kind, fmt.Sprintf(format, a...)}) }
	inputs := objects(ct["inputs"])
	for i, in := range inputs {
		if s, _ := in["type"].(string); strings.TrimSpace(s) == "" {
			add(defInputType, "%s has no type", inputLabel(in, i, declared))
		}
	}
	params, unnamed := 0, false
	if fd.Type.Params != nil {
		for _, field := range fd.Type.Params.List {
			if len(field.Names) == 0 {
				params++
				unnamed = true
				continue
			}
			for _, name := range field.Names {
				params++
				if name.Name == "_" {
					unnamed = true
					continue
				}
				if !hasInput(inputs, name.Name) {
					add(defInputsMissing, "inputs has no entry for parameter %q", name.Name)
				}
			}
		}
	}
	if unnamed && len(inputs) < params {
		add(defInputsMissing, "inputs has %d entries for %d parameters", len(inputs), params)
	}

	results, returnsError := 0, false
	if fd.Type.Results != nil {
		for _, field := range fd.Type.Results.List {
			results += max(1, len(field.Names))
			if id, ok := field.Type.(*ast.Ident); ok && id.Name == "error" {
				returnsError = true
			}
		}
	}
	outputs := objects(ct["outputs"])
	if len(outputs) < results {
		add(defOutputsCount, "outputs has %d entries for %d results", len(outputs), results)
	}
	for i, o := range outputs {
		if s, _ := o["type"].(string); strings.TrimSpace(s) == "" {
			add(defOutputType, "output %d has no type", i+1)
		}
	}

	errs := objects(ct["errors"])
	for i, e := range errs {
		when, _ := e["when"].(string)
		returns, _ := e["returns"].(string)
		if strings.TrimSpace(when) == "" || strings.TrimSpace(returns) == "" {
			add(defErrorsPartial, "errors entry %d needs both when and returns", i+1)
		}
	}
	if returnsError && len(errs) == 0 {
		add(defErrorsMissing, "the function returns error but errors lists no condition")
	}
	return out
}

// inputLabel names an input in an error: by name when it is a parameter the
// contract's signature declares, otherwise by position, so that text from the
// reply is never quoted.
func inputLabel(in map[string]any, i int, declared map[string]bool) string {
	if name, _ := in["name"].(string); declared[name] {
		return fmt.Sprintf("input %q", name)
	}
	return fmt.Sprintf("input %d", i+1)
}

// hasInput reports whether inputs describes the parameter: an entry named
// exactly like it, or one naming a part of it (r.Body for r).
func hasInput(inputs []map[string]any, param string) bool {
	for _, in := range inputs {
		name, _ := in["name"].(string)
		if name == param || strings.HasPrefix(name, param+".") {
			return true
		}
	}
	return false
}

// draftSchemaFailure validates a draft against the node schema and returns the
// failure, or nil. A function node is only schema-valid once it has tests and a
// wave, which come later, so the check runs on a copy that has placeholders for
// both.
func draftSchemaFailure(d map[string]any) *jsonschema.ValidationError {
	cp, err := copyDoc(d)
	if err != nil {
		return nil
	}
	cp["wave"] = 0
	cp["tests"] = []any{map[string]any{"name": "placeholder", "level": "unit", "given": "", "expect": "", "command": "true"}}
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if errors.As(schema.Validate(schema.KindNode, raw), &ve) {
		return ve
	}
	return nil
}

// validateDraft checks a draft against the node schema (see draftSchemaFailure).
func validateDraft(d map[string]any) error {
	cp, err := copyDoc(d)
	if err != nil {
		return err
	}
	cp["wave"] = 0
	cp["tests"] = []any{map[string]any{"name": "placeholder", "level": "unit", "given": "", "expect": "", "command": "true"}}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	err = schema.Validate(schema.KindNode, raw)
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return errors.New(schemaErr(ve))
	}
	return err
}

// waves computes wave(n) = 0 with no dependencies, else 1 + the highest wave
// among them, the same rule as tree.ComputeWaves. The planner needs it before
// function nodes are valid tree nodes (decision L3).
func waves(deps map[string][]string) (map[string]int, error) {
	const visiting = -1
	out := map[string]int{}
	var visit func(id string, trail []string) (int, error)
	visit = func(id string, trail []string) (int, error) {
		if w, ok := out[id]; ok {
			if w == visiting {
				return 0, fmt.Errorf("dependency cycle: %s", strings.Join(append(trail, id), " -> "))
			}
			return w, nil
		}
		out[id] = visiting
		w := 0
		for _, d := range deps[id] {
			if _, ok := deps[d]; !ok {
				return 0, fmt.Errorf("node %q depends on unknown node %q", id, d)
			}
			dw, err := visit(d, append(trail, id))
			if err != nil {
				return 0, err
			}
			w = max(w, dw+1)
		}
		out[id] = w
		return w, nil
	}
	ids := make([]string, 0, len(deps))
	for id := range deps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := visit(id, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// planWaves is waves over the whole plan: the root and the components have
// no dependencies of their own, a function depends on other functions.
func planWaves(rootID string, c *contract.Contracts, dec decomposed) (map[string]int, error) {
	deps := map[string][]string{rootID: nil}
	for _, comp := range c.Components {
		deps[comp.ID] = nil
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			deps[id] = strList(d["depends_on"])
		}
	}
	return waves(deps)
}

// writeSkeleton writes root.json and every <component>/component.json. It is
// called again whenever they change: when coverage adds root tests or
// functions.
func writeSkeleton(r *run, maxContext, maxRevisions int, c *contract.Contracts, dec decomposed, rootTests []RootTest) error {
	root, comps, err := buildSkeleton(r, maxContext, maxRevisions, c, dec, rootTests)
	if err != nil {
		return err
	}
	store := tree.NewStore(r.dir)
	for _, doc := range append([]map[string]any{root}, comps...) {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		n, err := tree.ParseNode(raw)
		if err != nil {
			return fmt.Errorf("node %v: %w", doc["id"], err)
		}
		if err := store.Write(n); err != nil {
			return err
		}
	}
	return nil
}

// buildSkeleton makes the root and component node documents. Everything in
// them is derived by code from the brief, the contract and the drafts.
func buildSkeleton(r *run, maxContext, maxRevisions int, c *contract.Contracts, dec decomposed, rootTests []RootTest) (map[string]any, []map[string]any, error) {
	var integ struct {
		Components []struct {
			ID    string `json:"id"`
			Tests []struct {
				Name    string `json:"name"`
				Given   string `json:"given"`
				Expect  string `json:"expect"`
				Command string `json:"command"`
			} `json:"integration_tests"`
		} `json:"components"`
	}
	if _, err := readJSON(r.path(fileContracts), &integ); err != nil {
		return nil, nil, err
	}
	integration := map[string][]any{}
	for _, comp := range integ.Components {
		for _, t := range comp.Tests {
			test := map[string]any{"name": t.Name, "level": "integration", "given": t.Given, "expect": t.Expect}
			if t.Command != "" {
				test["command"] = t.Command
			}
			integration[comp.ID] = append(integration[comp.ID], test)
		}
	}

	front := r.brief.Front
	budget := map[string]any{"max_context_tokens": maxContext, "max_revisions": maxRevisions}
	if front.Budget != nil {
		if front.Budget.MaxContextTokens > 0 {
			budget["max_context_tokens"] = front.Budget.MaxContextTokens
		}
		if front.Budget.MaxRevisions != nil {
			budget["max_revisions"] = *front.Budget.MaxRevisions
		}
	}
	children := []any{}
	for _, comp := range c.Components {
		children = append(children, comp.ID)
	}
	root := map[string]any{
		"spec_version": "2.0", "id": r.id, "kind": "root", "children": children,
		"title": cut(front.Title, 120), "description": firstParagraph(r.brief.Sections["Overview"]),
		"brief_ref": "#overview", "status": "pending", "wave": 0, "model_tier": "strong", "revision": 0,
		"budget": budget,
	}
	var constraints []any
	for _, q := range r.reqs {
		if q.Kind == ReqConstraint {
			constraints = append(constraints, q.Text)
		}
	}
	if len(constraints) > 0 {
		root["context"] = map[string]any{"constraints": constraints}
	}
	if len(front.Secrets) > 0 {
		names := []any{}
		for _, s := range front.Secrets {
			names = append(names, s.Name)
		}
		root["secrets"] = names
	}
	if len(front.Network) > 0 {
		hosts := []any{}
		for _, n := range front.Network {
			hosts = append(hosts, map[string]any{"host": n.Host, "critical": n.Critical})
		}
		root["network"] = hosts
	}
	if len(rootTests) > 0 {
		tests := []any{}
		for _, t := range rootTests {
			tests = append(tests, map[string]any{"name": t.Requirement + ": " + t.Name, "level": "acceptance",
				"given": t.Given, "expect": t.Expect, "command": t.Command})
		}
		root["tests"] = tests
	}

	var comps []map[string]any
	for _, comp := range c.Components {
		title, desc, ref := comp.ID, "Shared declarations.", "#architecture"
		if f, ok := featureFor(r.brief, comp.ID); ok {
			title, ref = cut(f.Name, 120), "#features/"+comp.ID
			if p := firstParagraph(f.Body); p != "" {
				desc = p
			}
		}
		kids := []any{}
		for _, d := range dec.Components[comp.ID] {
			kids = append(kids, d["id"])
		}
		doc := map[string]any{
			"spec_version": "2.0", "id": comp.ID, "kind": "component", "parent": r.id, "children": kids,
			"title": title, "description": desc, "brief_ref": ref,
			"status": "pending", "wave": 0, "model_tier": "strong", "revision": 0,
		}
		if tests := integration[comp.ID]; len(tests) > 0 {
			doc["tests"] = tests
		}
		comps = append(comps, doc)
	}
	return root, comps, nil
}

func featureFor(b *brief.Brief, component string) (brief.Feature, bool) {
	for _, f := range b.Features {
		if slug(f.Name) == component {
			return f, true
		}
	}
	return brief.Feature{}, false
}

func cut(s string, n int) string {
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n])
	}
	return s
}

// firstParagraph is the text up to the first blank line, on one line.
func firstParagraph(text string) string {
	para, _, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	return strings.Join(strings.Fields(para), " ")
}

// PlanNodes lists the plan's nodes the way the coverage check and the
// approval summary need them: the root, every component, and every function
// draft, read from the run folder.
func PlanNodes(runDir string) ([]PlanNode, error) {
	r := &run{dir: runDir}
	c, err := loadContracts(r)
	if err != nil {
		return nil, err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(r.path(fileBrief))
	if err != nil {
		return nil, err
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	nodes := []PlanNode{{ID: c.BriefID, Kind: tree.KindRoot, Title: b.Front.Title}}
	for _, comp := range c.Components {
		title := comp.ID
		if f, ok := featureFor(b, comp.ID); ok {
			title = f.Name
		}
		nodes = append(nodes, PlanNode{ID: comp.ID, Kind: tree.KindComponent, Parent: c.BriefID, Title: title})
	}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			title, _ := d["title"].(string)
			ct, _ := d["contract"].(map[string]any)
			file, _ := ct["file"].(string)
			nodes = append(nodes, PlanNode{ID: id, Kind: tree.KindFunction, Parent: comp.ID, Title: title, File: file})
		}
	}
	return nodes, nil
}
