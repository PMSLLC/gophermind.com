package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
)

// enrichGroups are the groups a model writes for a function node. error_kinds
// is part of the reply, not of the node: it is merged into contract.errors.
var enrichGroups = []string{"rationale", "construction", "alternatives", "portability", "security", "performance",
	"observability", "refactor_notes", "profile_hooks", "assumptions", "open_questions", "decision_ids"}

// enrichFieldsAll is every field a reply may carry, for a node that must be asked for again from scratch.
var enrichFieldsAll = append(append([]string(nil), enrichGroups...), "error_kinds")

const (
	maxEnrichRepairs = 2
	enrichFixStage   = "enrich:_fix"
)

// tierFor is the model chain a stage that works on one node uses: the node's
// own model_tier, or strong when routing by tier is off or the tier is unknown.
func (p *Planner) tierFor(modelTier string) router.Tier {
	if !p.d.Settings.Defaults.RouteByTier() {
		return router.TierStrong
	}
	switch t := router.Tier(modelTier); t {
	case router.TierStrong, router.TierStandard, router.TierAny:
		return t
	}
	return router.TierStrong
}

var tierRank = map[router.Tier]int{router.TierAny: 0, router.TierStandard: 1, router.TierStrong: 2}

// batchTier is the strongest tier among the drafts: no node gets a weaker
// chain than it asked for.
func (p *Planner) batchTier(drafts []map[string]any) router.Tier {
	best := router.Tier("")
	for _, d := range drafts {
		t := p.tierFor(strField(d, "model_tier"))
		if best == "" || tierRank[t] > tierRank[best] {
			best = t
		}
	}
	if best == "" {
		return router.TierStrong
	}
	return best
}

// nextBatchSize halves a batch after a reply that could not be used, down to one.
func nextBatchSize(size, n int, truncated bool) int {
	if truncated && n > 1 {
		return max(1, n/2)
	}
	return size
}

// pendingEnrich is a node whose enrichment failed a check, kept with what the
// model sent so far, the defect kinds, and the groups to ask for again.
type pendingEnrich struct {
	Component string         `json:"component"`
	ID        string         `json:"id"`
	Raw       map[string]any `json:"raw,omitempty"`
	Defects   []string       `json:"defects"`
	Fields    []string       `json:"fields"`
	Tries     int            `json:"tries,omitempty"`
}

// enrichedState is _state/enriched.json. The function groups themselves are
// merged into the drafts in decomposed.json; this file holds what has no draft
// to live in: the component and root groups, the pending repairs, the warnings.
type enrichedState struct {
	Components map[string]map[string]any `json:"components"`
	Root       map[string]any            `json:"root,omitempty"`
	Pending    []pendingEnrich           `json:"pending,omitempty"`
	Warnings   []string                  `json:"warnings,omitempty"`
	Done       bool                      `json:"done"`
}

func loadEnriched(r *run) (enrichedState, error) {
	st := enrichedState{Components: map[string]map[string]any{}}
	if _, err := readJSON(r.path(stateEnriched), &st); err != nil {
		return enrichedState{}, err
	}
	if st.Components == nil {
		st.Components = map[string]map[string]any{}
	}
	return st, nil
}

func (s enrichedState) save(r *run) error { return writeJSON(r.path(stateEnriched), s) }

func enrichDone(r *run) bool {
	st, err := loadEnriched(r)
	return err == nil && st.Done
}

// EnrichError is returned when some nodes still fail the group checks after
// their repairs. It names node ids and defect kinds, never reply text.
type EnrichError struct{ Items []string }

func (e *EnrichError) Error() string {
	shown := e.Items
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return fmt.Sprintf("%d node(s) still fail the group checks after %d repairs: %s", len(e.Items), maxEnrichRepairs, strings.Join(shown, "; "))
}

// groupEnvFor gathers what the group checks need that is the same for every node.
func (p *Planner) groupEnvFor(r *run, c *contract.Contracts) (groupEnv, error) {
	env := groupEnv{Secrets: map[string]bool{}, Decisions: map[string]bool{}}
	for _, s := range r.brief.Front.Secrets {
		env.Secrets[s.Name] = true
	}
	qs, err := loadQStore(r)
	if err != nil {
		return env, err
	}
	for _, q := range qs.Questions {
		if q.Status == qSettled {
			env.Decisions[q.ID] = true
		}
	}
	f, err := loadFacts(r)
	if err != nil {
		return env, err
	}
	env.GoMin = f.GoVersion
	deps, err := ReadDependencies(r.dir)
	if err != nil {
		return env, err
	}
	env.DepMods = []string{c.Module}
	for _, d := range deps {
		env.DepMods = append(env.DepMods, d.Module)
	}
	return env, nil
}

// paramNames is the set of parameter names in a signature.
func paramNames(sig string) map[string]bool {
	out := map[string]bool{}
	fd, err := parseSignature(sig)
	if err != nil || fd.Type.Params == nil {
		return out
	}
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			out[n.Name] = true
		}
	}
	return out
}

// mergeEnrichment puts a model's groups onto a draft and checks the result.
// The draft is not changed: merged is a copy. Code derives go_type from the
// signature; the model never writes it. A type the contract does not declare
// is a warning, not a defect: the Enrich model cannot repair the Contract.
func mergeEnrichment(draft, reply map[string]any, c *contract.Contracts, env groupEnv) (merged map[string]any, defects []groupDefect, warnings []string) {
	merged, err := copyDoc(draft)
	if err != nil {
		return nil, []groupDefect{{grpSchema, "", "the node cannot be copied"}}, nil
	}
	id, _ := merged["id"].(string)
	for _, g := range enrichGroups {
		if v, ok := reply[g]; ok {
			merged[g] = v
		}
	}
	class, _ := merged["node_class"].(string)
	ct, _ := merged["contract"].(map[string]any)
	sig, _ := ct["signature"].(string)
	if errs := objects(ct["errors"]); len(errs) > 0 {
		kinds := strList(reply["error_kinds"])
		if len(kinds) != len(errs) {
			defects = append(defects, groupDefect{grpEnum, "error_kinds", fmt.Sprintf("error_kinds has %d entries for %d errors", len(kinds), len(errs))})
		} else {
			for i, e := range errs {
				e["kind"] = kinds[i]
			}
		}
	}
	if gt, gerr := deriveGoTypes(sig, c); gerr == nil {
		for _, in := range objects(ct["inputs"]) {
			if name, _ := in["name"].(string); name != "" {
				if t, ok := gt.ParamByName[name]; ok {
					in["go_type"] = t
				}
			}
		}
		for i, o := range objects(ct["outputs"]) {
			if i < len(gt.Results) {
				o["go_type"] = gt.Results[i]
			}
		}
		if n := len(gt.Unresolved); n > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %d type name(s) in the signature are not declared in the contract", boundedID(id), n))
		}
	}
	env.Params = paramNames(sig)
	defects = append(defects, groupDefects(merged, class, env)...)
	if len(defects) == 0 {
		if verr := validateDraft(merged); verr != nil {
			defects = append(defects, groupDefect{grpSchema, "", "the groups do not match the node schema"})
		}
	}
	return merged, defects, warnings
}

func kindsOfGroup(ds []groupDefect) []string {
	var out []string
	for _, d := range ds {
		if !contains(out, d.kind) {
			out = append(out, d.kind)
		}
	}
	return out
}

// fieldsOfGroup is the reply fields to ask for again for a set of defects. A
// defect with no named field asks for everything.
func fieldsOfGroup(ds []groupDefect) []string {
	var out []string
	for _, d := range ds {
		switch {
		case d.field == "":
			return append([]string(nil), enrichFieldsAll...)
		case d.field == "contract":
			continue
		case !contains(out, d.field):
			out = append(out, d.field)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), enrichFieldsAll...)
	}
	sort.Strings(out)
	return out
}

// enrichResult is what an Enrich reply held.
type enrichResult struct {
	good     []map[string]any // merged drafts that passed every check
	bad      []pendingEnrich
	warnings []string
}

// enrichView is a draft as the Enrich prompt shows it.
func enrichView(d map[string]any) map[string]any {
	nctx, _ := d["context"].(map[string]any)
	return map[string]any{
		"id": d["id"], "title": d["title"], "description": d["description"], "node_class": d["node_class"],
		"model_tier": d["model_tier"], "depends_on": d["depends_on"], "contract": d["contract"],
		"dependency_signatures": nctx["dependency_signatures"], "constraints": nctx["constraints"],
	}
}

// splitEnrich reads an Enrich reply for the drafts asked for. A node that
// fails a check does not sink the batch: it comes back in bad. prev holds what
// an earlier answer already supplied (a repair merges the new fields into it).
func splitEnrich(text string, batch []map[string]any, c *contract.Contracts, env groupEnv, prev map[string]map[string]any) (enrichResult, error) {
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return enrichResult{}, fmt.Errorf("enrich reply is not a JSON array of objects (%s)", jsonErr(err))
	}
	want := map[string]bool{}
	for _, d := range batch {
		id, _ := d["id"].(string)
		want[id] = true
	}
	byID := map[string]map[string]any{}
	for _, x := range got {
		if id, _ := x["id"].(string); want[id] && byID[id] == nil {
			byID[id] = x // anything else is noise and ignored
		}
	}
	var res enrichResult
	for _, d := range batch {
		id, _ := d["id"].(string)
		reply := byID[id]
		if reply == nil && prev[id] == nil {
			res.bad = append(res.bad, pendingEnrich{ID: id, Defects: []string{grpMissing}, Fields: append([]string(nil), enrichFieldsAll...)})
			continue
		}
		raw := map[string]any{}
		for k, v := range prev[id] {
			raw[k] = v
		}
		for k, v := range reply {
			if k != "id" {
				raw[k] = v
			}
		}
		merged, ds, warns := mergeEnrichment(d, raw, c, env)
		if len(ds) > 0 {
			res.bad = append(res.bad, pendingEnrich{ID: id, Raw: raw, Defects: kindsOfGroup(ds), Fields: fieldsOfGroup(ds)})
			continue
		}
		res.good = append(res.good, merged)
		res.warnings = append(res.warnings, warns...)
	}
	return res, nil
}

func decisionsText(s qstore) string {
	var b strings.Builder
	for _, q := range s.Questions {
		if q.Status == qSettled && b.Len() < 6000 {
			fmt.Fprintf(&b, "%s: %s => %s\n", q.ID, oneLine(q.Text, 200), oneLine(q.Answer, 200))
		}
	}
	if b.Len() == 0 {
		return "(none)"
	}
	return strings.TrimRight(b.String(), "\n")
}

func defectsText(fix []pendingEnrich) string {
	if len(fix) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("This is a repair. Your earlier answer had these defects (the kinds are fixed words; nothing you wrote is quoted). Answer again for the functions below, with ONLY the groups wanted:\n")
	for _, pe := range fix {
		fmt.Fprintf(&b, "%s: %s (groups: %s)\n", pe.ID, strings.Join(pe.Defects, ", "), strings.Join(pe.Fields, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// enrichCall asks for the groups of one batch of one component's function
// drafts. fix and prev are set when it is a repair.
func (p *Planner) enrichCall(ctx context.Context, r *run, c *contract.Contracts, comp contract.Component, batch []map[string]any, env groupEnv, fix []pendingEnrich, prev map[string]map[string]any) (enrichResult, error) {
	qs, err := loadQStore(r)
	if err != nil {
		return enrichResult{}, err
	}
	facts, err := loadFacts(r)
	if err != nil {
		return enrichResult{}, err
	}
	views := make([]map[string]any, len(batch))
	for i, d := range batch {
		views[i] = enrichView(d)
	}
	fields := enrichFieldsAll
	if len(fix) > 0 {
		fields = nil
		for _, pe := range fix {
			for _, f := range pe.Fields {
				if !contains(fields, f) {
					fields = append(fields, f)
				}
			}
		}
	}
	deps := append([]string(nil), env.DepMods...)
	secrets := make([]string, 0, len(env.Secrets))
	for n := range env.Secrets {
		secrets = append(secrets, n)
	}
	sort.Strings(secrets)
	prompt, err := render("enrich", map[string]string{
		"Component": mustJSON(comp), "Nodes": mustJSON(views), "BriefSection": briefSection(r, comp.ID),
		"Decisions": decisionsText(qs), "Facts": factsPrompt(facts), "Deps": orNone(deps), "Secrets": orNone(secrets),
		"Fields": strings.Join(fields, ", "), "Defects": defectsText(fix),
	})
	if err != nil {
		return enrichResult{}, err
	}
	stage := "enrich:" + comp.ID
	if len(fix) > 0 {
		stage = enrichFixStage
	}
	var res enrichResult
	cs := callSpec{stage: stage, taskType: "enrich", scope: router.ScopeComponent, maxTokens: maxTokensEnrich, tier: p.batchTier(batch)}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		got, perr := splitEnrich(StripReply(text), batch, c, env, prev)
		if perr != nil {
			return perr
		}
		res = got
		return nil
	})
	for i := range res.bad {
		res.bad[i].Component = comp.ID
	}
	return res, err
}

// draftIndex finds a function draft by id.
func draftIndex(dec *decomposed, id string) (comp string, i int, ok bool) {
	for c, drafts := range dec.Components {
		for j, d := range drafts {
			if got, _ := d["id"].(string); got == id {
				return c, j, true
			}
		}
	}
	return "", 0, false
}

// enrichMissing writes the groups of every function draft that has none, then
// the component and root groups. Progress is saved after every call, so a
// resume (and a Coverage fill round that added functions) asks only for what is
// missing.
func (p *Planner) enrichMissing(ctx context.Context, r *run, c *contract.Contracts, dec *decomposed) error {
	st, err := loadEnriched(r)
	if err != nil {
		return err
	}
	env, err := p.groupEnvFor(r, c)
	if err != nil {
		return err
	}
	cfg := p.d.Settings.Defaults
	pending := map[string]bool{}
	for _, pe := range st.Pending {
		pending[pe.ID] = true
	}
	for _, comp := range c.Components {
		var missing []map[string]any
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			if _, done := d["rationale"]; !done && !pending[id] {
				if strField(d, "node_class") == "" {
					classes, _ := loadClasses(r)
					d["node_class"] = classes[id]
				}
				missing = append(missing, d)
			}
		}
		size := cfg.EnrichBatchSize
		for len(missing) > 0 {
			n := min(size, len(missing))
			batch := missing[:n]
			res, err := p.enrichCall(ctx, r, c, comp, batch, env, nil, nil)
			if err != nil {
				if failedAttempt(ctx, err) && n > 1 {
					size = nextBatchSize(size, n, true)
					continue
				}
				return err
			}
			p.applyEnrichment(dec, &st, res)
			if err := writeJSON(r.path(stateDecomposed), *dec); err != nil {
				return err
			}
			if err := st.save(r); err != nil {
				return err
			}
			missing = missing[n:]
		}
	}
	if err := p.repairEnrichment(ctx, r, c, dec, &st, env); err != nil {
		return err
	}
	if err := p.enrichStructure(ctx, r, c, *dec, &st, env); err != nil {
		return err
	}
	st.Done = true
	return st.save(r)
}

// applyEnrichment stores the nodes that passed in their drafts and the ones
// that did not in the pending list.
func (p *Planner) applyEnrichment(dec *decomposed, st *enrichedState, res enrichResult) {
	for _, m := range res.good {
		id, _ := m["id"].(string)
		if comp, i, ok := draftIndex(dec, id); ok {
			dec.Components[comp][i] = m
		}
		kept := st.Pending[:0]
		for _, pe := range st.Pending {
			if pe.ID != id {
				kept = append(kept, pe)
			}
		}
		st.Pending = kept
	}
	for _, w := range res.warnings {
		if !contains(st.Warnings, w) {
			st.Warnings = append(st.Warnings, w)
			p.emit(events.KindWarning, "enrich", "", w)
		}
	}
	for _, b := range res.bad {
		replaced := false
		for i := range st.Pending {
			if st.Pending[i].ID == b.ID {
				b.Tries = st.Pending[i].Tries + 1
				st.Pending[i] = b
				replaced = true
			}
		}
		if !replaced {
			st.Pending = append(st.Pending, b)
		}
	}
}

// repairEnrichment asks again, alone and for the defective groups only, for
// each node that failed a check, up to maxEnrichRepairs passes. A node still
// failing after that ends the stage with an EnrichError: nothing is invented.
func (p *Planner) repairEnrichment(ctx context.Context, r *run, c *contract.Contracts, dec *decomposed, st *enrichedState, env groupEnv) error {
	cfg := p.d.Settings.Defaults
	for {
		byComp := map[string][]pendingEnrich{}
		for _, pe := range st.Pending {
			if pe.Tries < maxEnrichRepairs {
				byComp[pe.Component] = append(byComp[pe.Component], pe)
			}
		}
		if len(byComp) == 0 {
			break
		}
		for _, comp := range c.Components {
			todo := byComp[comp.ID]
			for lo := 0; lo < len(todo); lo += cfg.EnrichBatchSize {
				chunk := todo[lo:min(lo+cfg.EnrichBatchSize, len(todo))]
				drafts := make([]map[string]any, 0, len(chunk))
				prev := map[string]map[string]any{}
				for _, pe := range chunk {
					if cname, i, ok := draftIndex(dec, pe.ID); ok {
						drafts = append(drafts, dec.Components[cname][i])
						prev[pe.ID] = pe.Raw
					}
				}
				res, err := p.enrichCall(ctx, r, c, comp, drafts, env, chunk, prev)
				if err != nil {
					if failedAttempt(ctx, err) {
						for i := range st.Pending {
							for _, pe := range chunk {
								if st.Pending[i].ID == pe.ID {
									st.Pending[i].Tries++
								}
							}
						}
						if serr := st.save(r); serr != nil {
							return serr
						}
						continue
					}
					return err
				}
				p.applyEnrichment(dec, st, res)
				if err := writeJSON(r.path(stateDecomposed), *dec); err != nil {
					return err
				}
				if err := st.save(r); err != nil {
					return err
				}
			}
		}
	}
	if len(st.Pending) > 0 {
		var items []string
		for _, pe := range st.Pending {
			items = append(items, fmt.Sprintf("%s: %s", boundedID(pe.ID), strings.Join(pe.Defects, ", ")))
		}
		sort.Strings(items)
		return &EnrichError{Items: items}
	}
	return nil
}

// enrichStructure writes the rationale, assumptions, open questions and
// decision ids of every component and of the root: one small call each.
func (p *Planner) enrichStructure(ctx context.Context, r *run, c *contract.Contracts, dec decomposed, st *enrichedState, env groupEnv) error {
	qs, err := loadQStore(r)
	if err != nil {
		return err
	}
	for _, comp := range c.Components {
		if st.Components[comp.ID] != nil {
			continue
		}
		var fns []map[string]any
		for _, d := range dec.Components[comp.ID] {
			fns = append(fns, map[string]any{"id": d["id"], "title": d["title"], "description": d["description"]})
		}
		subject := mustJSON(map[string]any{"component": comp, "functions": fns})
		g, err := p.structureCall(ctx, r, "enrich_comp:"+comp.ID, "component", subject, briefSection(r, comp.ID), router.ScopeComponent, qs, env)
		if err != nil {
			return err
		}
		st.Components[comp.ID] = g
		if err := st.save(r); err != nil {
			return err
		}
	}
	if st.Root == nil {
		var ids []string
		for _, comp := range c.Components {
			ids = append(ids, comp.ID)
		}
		subject := mustJSON(map[string]any{"title": r.brief.Front.Title, "overview": oneLine(r.brief.Sections["Overview"], 2000), "components": ids})
		g, err := p.structureCall(ctx, r, "enrich_root", "root", subject, oneLine(r.brief.Sections["Overview"], 2000), router.ScopeBrief, qs, env)
		if err != nil {
			return err
		}
		st.Root = g
		if err := st.save(r); err != nil {
			return err
		}
	}
	return nil
}

// structureCall asks for the groups of a component or the root.
func (p *Planner) structureCall(ctx context.Context, r *run, stage, kind, subject, section string, scope router.Scope, qs qstore, env groupEnv) (map[string]any, error) {
	prompt, err := render("enrich_structure", map[string]string{"Kind": kind, "Subject": subject, "BriefSection": section, "Decisions": decisionsText(qs)})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	cs := callSpec{stage: stage, taskType: "enrich", scope: scope, maxTokens: maxTokensEnrichStructure}
	err = p.call(ctx, r, cs, prompt, func(text string) error {
		var x map[string]any
		if err := json.Unmarshal([]byte(StripReply(text)), &x); err != nil {
			return fmt.Errorf("enrich reply is not a JSON object (%s)", jsonErr(err))
		}
		rat, _ := x["rationale"].(string)
		if n := len([]rune(strings.TrimSpace(rat))); n < 20 || n > 600 {
			return errors.New("enrich reply: rationale must be 20 to 600 characters")
		}
		for _, k := range []string{"assumptions", "open_questions", "decision_ids"} {
			if _, ok := x[k].([]any); !ok {
				return fmt.Errorf("enrich reply: %s must be an array", k)
			}
		}
		if len(strList(x["open_questions"])) > 0 {
			return errors.New("enrich reply: open_questions must be empty")
		}
		for i, id := range strList(x["decision_ids"]) {
			if !env.Decisions[id] {
				return fmt.Errorf("enrich reply: decision_ids entry %d is not a settled question", i+1)
			}
		}
		out = map[string]any{"rationale": strings.TrimSpace(rat), "assumptions": x["assumptions"], "open_questions": []any{}, "decision_ids": x["decision_ids"]}
		return nil
	})
	return out, err
}

// enrich is the Enrich stage.
func (p *Planner) enrich(ctx context.Context, r *run) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	if err := p.enrichMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	// The tree skeleton carries the component and root groups and the decisions.
	return p.rewriteSkeleton(r, nil)
}

// openQuestionStructures lists the components and the root that still list an open question.
func openQuestionStructures(st enrichedState) []string {
	var out []string
	for id, g := range st.Components {
		if len(strList(g["open_questions"])) > 0 {
			out = append(out, id)
		}
	}
	if len(strList(st.Root["open_questions"])) > 0 {
		out = append(out, "root")
	}
	sort.Strings(out)
	return out
}
