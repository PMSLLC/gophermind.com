package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"
	"gophermind/gophermind-lib/briefv2/tree"
)

// contractState is _state/contract.json: the contract as far as it has been
// written, and which components are finished. contracts.json itself is only
// written once every pass is done.
type contractState struct {
	Doc  map[string]any `json:"doc"`
	Done []string       `json:"done"`
	// The outline is built in harness-driven passes: a shared pass, then one
	// pass per batch of the brief's features. OutlinePasses counts the passes
	// stored; OutlineBatches is the feature names of each batch (fixed when
	// the shared pass is stored) and OutlineBatchNext the index of the first
	// batch not yet stored. A state file without these fields is a finished
	// outline.
	Deps             []Dependency `json:"deps,omitempty"`
	OutlinePasses    int          `json:"outline_passes,omitempty"`
	OutlineBatches   [][]string   `json:"outline_batches,omitempty"`
	OutlineBatchNext int          `json:"outline_batch_next,omitempty"`
	// OutlineBatchesSet is true once OutlineBatches is authoritative (a state
	// from before the batches existed lacks it); OutlineSharedIDs are the ids
	// the shared pass wrote, always listed to the batch passes.
	OutlineBatchesSet bool     `json:"outline_batches_set,omitempty"`
	OutlineSharedIDs  []string `json:"outline_shared_ids,omitempty"`
	// ComponentPasses counts the stored passes of each component, for the cap.
	ComponentPasses map[string]int `json:"component_passes,omitempty"`
	// SchemaRepairs counts the passes that asked for fields the merged contract
	// lacked (attempts that failed count too, across restarts).
	SchemaRepairs int `json:"schema_repairs,omitempty"`
	// OutlineRepairs counts the repair passes stored; OutlineDone is set once
	// the outline has no unresolved reference and dependencies.json is written.
	OutlineRepairs int  `json:"outline_repairs,omitempty"`
	OutlineDone    bool `json:"outline_done,omitempty"`
	// Repairs counts the stored repair passes that ask for ids the written
	// components use but never declare.
	Repairs int `json:"repairs,omitempty"`
	// IgnoredDuplicates lists, for the run report, the ids a later emission
	// repeated with different content; the first emission was kept.
	IgnoredDuplicates []string `json:"ignored_duplicates,omitempty"`
	// IgnoredTotal is the exact count of dropped emissions; the stored list is
	// capped, and IgnoredTruncated says when it is.
	IgnoredTotal     int  `json:"ignored_total,omitempty"`
	IgnoredTruncated bool `json:"ignored_truncated,omitempty"`
	// IDsNormalized counts the ids and references rewritten to the id syntax
	// (outline_id_normalized); the rewrite is deterministic, so this is only a count.
	IDsNormalized int `json:"ids_normalized,omitempty"`
}

const maxIgnoredRecorded = 200

// noteIgnored records dropped duplicate emissions in the state and reports
// them as one warning: ids that pass the id syntax (at most 64 bytes, at most
// 10 named) and the count, never content.
func (p *Planner) noteIgnored(st *contractState, stage string, ignored []string) {
	if len(ignored) == 0 {
		return
	}
	st.IgnoredTotal += len(ignored)
	for _, n := range ignored {
		if len(st.IgnoredDuplicates) < maxIgnoredRecorded {
			st.IgnoredDuplicates = append(st.IgnoredDuplicates, n)
		} else {
			st.IgnoredTruncated = true
		}
	}
	shown := ignored
	if len(shown) > maxUnresolvedInError {
		shown = shown[:maxUnresolvedInError]
	}
	p.emit(events.KindWarning, stage, "", fmt.Sprintf(
		"outline_duplicate_ignored: %d later emissions of an id already written were dropped and the first kept (%s); %d in all so far",
		len(ignored), strings.Join(shown, ", "), st.IgnoredTotal))
}

// failedAttempt is true for a model call that failed because of the model: the
// router ran out of models after replies the parser rejected. Only that counts
// as a repair attempt. A ledger, budget, privacy or transport error is not the
// model's doing and aborts the stage without using up an attempt, and neither
// does the caller giving up.
func failedAttempt(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var ce *router.ChainExhausted
	return errors.As(err, &ce) && ce.ParseErr != nil
}

// outlineBatchSize is how many of the brief's features one outline pass
// covers. The harness, not the model, decides which passes remain.
const outlineBatchSize = 3

// outlineBatches splits the feature names, in brief order, into batches.
func outlineBatches(features []string) [][]string {
	var out [][]string
	for i := 0; i < len(features); i += outlineBatchSize {
		end := i + outlineBatchSize
		if end > len(features) {
			end = len(features)
		}
		out = append(out, append([]string(nil), features[i:end]...))
	}
	return out
}

// outlinePassIsFinal says whether a pass ends the outline, so the whole
// outline is validated: a repair pass, the last batch, or the shared pass when
// there is no batch at all.
func outlinePassIsFinal(first bool, batches [][]string, next int, repair bool) bool {
	switch {
	case repair:
		return true
	case first:
		return len(batches) == 0
	}
	return next >= len(batches)-1
}

// maxComponentPasses caps the passes one component may take: a model that adds
// a function every time and never says it is done is stopped.
const maxComponentPasses = 40

// outlinePassStage is the stage of outline pass n: 1 is the shared pass, 2 the
// first batch.
func outlinePassStage(n int) string { return fmt.Sprintf("contract:outline:%d", n) }

// outlineRepairStage is the stage of the passes that ask for types the outline
// uses but never declared.
const outlineRepairStage = "contract:outline:repair"

// maxOutlineRepairs bounds the passes that ask the model to write the ids a
// finished outline uses but never declared.
const maxOutlineRepairs = 2

// repairStage is the stage of the passes that ask for ids the written
// components use but never declared. A component id cannot start with an
// underscore, so no component can own this stage name.
const repairStage = "contract:_repair"

const (
	maxUnresolvedInPrompt = 50
	maxUnresolvedInError  = 10
)

func contractDone(r *run) bool { return exists(r.path(fileContracts)) }

// contract is the Contract stage (Wave 0), built in passes so that a large
// brief makes more calls rather than a coarser contract: a shared outline
// pass, one outline pass per batch of features (the harness knows which
// remain, the model never says more), then one call per component, repeated
// while the model says more remains for that component.
func (p *Planner) contract(ctx context.Context, r *run) error {
	if !confirmDone(r) {
		return errNotConfirmed
	}
	as, err := loadAnswers(r)
	if err != nil {
		return err
	}
	var st contractState
	found, err := readJSON(r.path(stateContract), &st)
	if err != nil {
		return err
	}
	if !found || (st.OutlinePasses > 0 && !st.OutlineDone) {
		typeSchema, err := contractItemSchemas("types")
		if err != nil {
			return err
		}
		// ask makes one outline call: the shared pass (batch nil, no unresolved),
		// a batch pass, or a repair pass (unresolved ids).
		var featureNames []string
		for _, f := range r.brief.Features {
			featureNames = append(featureNames, f.Name)
		}
		allBatches := outlineBatches(featureNames)
		ask := func(stage string, batch, unresolved []string) error {
			names := batch
			if len(names) == 0 {
				names = featureNames
			}
			build := func(level int) (string, error) {
				brief := string(r.src)
				if level > 0 {
					brief = briefExcerpt(r, names, level)
				}
				return render("contract_outline", map[string]string{
					"Brief": brief, "Answers": answersText(as), "TypeSchema": typeSchema,
					"Fixed": outlineFixedText(st.Doc), "Emitted": outlineEmittedTextAt(st.Doc, st.OutlineSharedIDs, level),
					"Batch":      strings.Join(batch, ", "),
					"Unresolved": unresolvedPromptText(unresolved), "UnresolvedCount": fmt.Sprint(len(unresolved))})
			}
			var ignored []string
			var notes idNotes
			var extras []string
			added := 0
			first := st.Doc == nil
			cs := callSpec{stage: stage, taskType: "contract", scope: router.ScopeBrief,
				maxTokens: maxTokensOutline, maxGrown: maxGrownOutline}
			if err := p.callSized(ctx, r, cs, build, func(text string) error {
				text, nt, err := normalizeReply(st.Doc, StripReply(text), replyOutline)
				if err != nil {
					return err
				}
				if first {
					var dropped []string
					if text, dropped, err = dropSharedExtras(text); err != nil {
						return err
					}
					extras = dropped
				}
				final := outlinePassIsFinal(first, pick(first, allBatches, st.OutlineBatches), st.OutlineBatchNext, len(unresolved) > 0)
				doc, ds, n, ign, err := mergeOutline(st.Doc, st.Deps, text, r.id, final)
				if err != nil {
					return err
				}
				st.Doc, st.Deps, added, ignored, notes = doc, ds, n, append(nt.Ignored, ign...), nt
				return nil
			}); err != nil {
				if failedAttempt(ctx, err) && len(unresolved) > 0 {
					st.OutlineRepairs++
					if werr := writeJSON(r.path(stateContract), st); werr != nil {
						return werr
					}
				}
				return err
			}
			switch {
			case len(unresolved) > 0:
				st.OutlineRepairs++
			case first:
				st.OutlinePasses++
				st.OutlineBatches, st.OutlineBatchesSet = allBatches, true
				st.OutlineSharedIDs = outlineIDs(st.Doc, maxSharedIDs)
			default:
				st.OutlinePasses++
				st.OutlineBatchNext++
				if added == 0 {
					p.emit(events.KindWarning, "contract", "", fmt.Sprintf(
						"outline_pass_empty: outline batch %d added no component, type or dependency", st.OutlineBatchNext))
				}
			}
			if len(extras) > 0 {
				shown := extras
				if len(shown) > maxUnresolvedInError {
					shown = shown[:maxUnresolvedInError]
				}
				p.emit(events.KindWarning, "contract", "", fmt.Sprintf(
					"outline_shared_extra: %d components other than types were dropped from the shared pass, the batches write them (%s)",
					len(extras), strings.Join(shown, ", ")))
			}
			p.noteNormalized(&st, "contract", notes)
			p.noteIgnored(&st, "contract", ignored)
			return writeJSON(r.path(stateContract), st)
		}
		if st.Doc != nil && !st.OutlineBatchesSet {
			// A state from before the batches were stored: its shared pass is
			// done, the batches are worked out again from the brief.
			if len(st.OutlineBatches) == 0 {
				st.OutlineBatches = allBatches
			}
			st.OutlineBatchesSet = true
		}
		if st.Doc == nil {
			if err := ask(outlinePassStage(1), nil, nil); err != nil {
				return err
			}
		}
		for st.OutlineBatchNext < len(st.OutlineBatches) {
			if err := ask(outlinePassStage(st.OutlineBatchNext+2), st.OutlineBatches[st.OutlineBatchNext], nil); err != nil {
				return err
			}
		}
		if len(objects(st.Doc["components"])) == 0 {
			return errors.New("contract outline lists no component")
		}
		// The outline is complete as written; ids it uses but never declared are
		// asked for, not treated as an unusable reply.
		for {
			un := unresolvedUses(st.Doc)
			if len(un) == 0 {
				break
			}
			if st.OutlineRepairs >= maxOutlineRepairs {
				return unresolvedErr("contract:outline", un)
			}
			if err := ask(outlineRepairStage, nil, un); err != nil {
				return err
			}
		}
		st.OutlineDone = true
		if err := writeJSON(r.path(fileDependencies), st.Deps); err != nil {
			return err
		}
		if err := writeJSON(r.path(stateContract), st); err != nil {
			return err
		}
	}

	itemSchemas, err := contractItemSchemas("types", "functions")
	if err != nil {
		return err
	}
	for _, comp := range objects(st.Doc["components"]) {
		id, _ := comp["id"].(string)
		if contains(st.Done, id) {
			continue
		}
		for {
			build := func(level int) (string, error) {
				return render("contract_component", map[string]string{
					"Component":    mustJSON(comp),
					"Outline":      outlineJSONAt(st.Doc, level),
					"Declared":     declaredTextAt(st.Doc, level),
					"Written":      writtenText(st.Doc, id),
					"BriefSection": briefSectionAt(r, id, level),
					"Answers":      answersText(as),
					"ItemSchemas":  itemSchemas,
				})
			}
			more := false
			var ignored []string
			var notes idNotes
			cs := callSpec{stage: "contract:" + id, taskType: "contract", scope: router.ScopeComponent, maxTokens: maxTokensContract}
			if err := p.callSized(ctx, r, cs, build, func(text string) error {
				text, nt, err := normalizeReply(st.Doc, StripReply(text), replyComponent)
				if err != nil {
					return err
				}
				doc, m, ign, err := mergePass(st.Doc, id, text, r.id)
				if err != nil {
					return err
				}
				st.Doc, more, ignored, notes = doc, m, append(nt.Ignored, ign...), nt
				return nil
			}); err != nil {
				return err
			}
			p.noteNormalized(&st, "contract:"+id, notes)
			p.noteIgnored(&st, "contract:"+id, ignored)
			if !more {
				st.Done = append(st.Done, id)
			}
			if st.ComponentPasses == nil {
				st.ComponentPasses = map[string]int{}
			}
			st.ComponentPasses[id]++
			if err := writeJSON(r.path(stateContract), st); err != nil {
				return err
			}
			if !more {
				break
			}
			if st.ComponentPasses[id] >= maxComponentPasses {
				return fmt.Errorf("stage contract:%s did not finish after %d passes; the model keeps adding functions",
					strings.Trim(boundedID(id), `"`), maxComponentPasses)
			}
		}
	}

	// Every component is written; ids a function or type uses but nobody
	// declared are asked for, not treated as an unusable reply.
	for {
		un := unresolvedFinal(st.Doc)
		if len(un) == 0 {
			break
		}
		if st.Repairs >= maxOutlineRepairs {
			return unresolvedErr(repairStage, un)
		}
		build := func(level int) (string, error) {
			return render("contract_repair", map[string]string{
				"Outline":  outlineJSONAt(st.Doc, level),
				"Declared": declaredTextAt(st.Doc, level), "Answers": answersText(as), "ItemSchemas": itemSchemas,
				"BriefSections": briefExcerpt(r, featuresOfComponents(r, unresolvedOwnerComponents(st.Doc, un)), repairExcerptLevel(level)),
				"Unresolved":    unresolvedOwnersText(st.Doc, un), "UnresolvedCount": fmt.Sprint(len(un))})
		}
		var ignored []string
		var notes idNotes
		cs := callSpec{stage: repairStage, taskType: "contract", scope: router.ScopeBrief, maxTokens: maxTokensContract}
		if err := p.callSized(ctx, r, cs, build, func(text string) error {
			text, nt, err := normalizeReply(st.Doc, StripReply(text), replyRepair)
			if err != nil {
				return err
			}
			doc, ign, err := mergeRepair(st.Doc, text, r.id)
			if err != nil {
				return err
			}
			st.Doc, ignored, notes = doc, append(nt.Ignored, ign...), nt
			return nil
		}); err != nil {
			if failedAttempt(ctx, err) {
				st.Repairs++
				if werr := writeJSON(r.path(stateContract), st); werr != nil {
					return werr
				}
			}
			return err
		}
		p.noteNormalized(&st, repairStage, notes)
		p.noteIgnored(&st, repairStage, ignored)
		st.Repairs++
		if err := writeJSON(r.path(stateContract), st); err != nil {
			return err
		}
	}

	// Every reference is declared now: pending ones become the declared form,
	// and the stored contract carries only declared ids.
	st.Doc = resolveRefs(st.Doc)
	if err := writeJSON(r.path(stateContract), st); err != nil {
		return err
	}

	// The merged contract is stored. Fields a node lacks are asked for again,
	// for those nodes only, before the last validation.
	itemSchemasFix, err := contractItemSchemas("types", "functions")
	if err != nil {
		return err
	}
	if err := p.repairSchema(ctx, r, &st, answersText(as), itemSchemasFix); err != nil {
		return err
	}

	// A component whose exports the outline left empty exports all its functions.
	fns := objects(st.Doc["functions"])
	if len(fns) == 0 {
		return errors.New("the contract declares no function")
	}
	for _, comp := range objects(st.Doc["components"]) {
		if len(strList(comp["exports"])) > 0 {
			continue
		}
		exports := []any{}
		for _, f := range fns {
			if f["component"] == comp["id"] {
				exports = append(exports, f["id"])
			}
		}
		comp["exports"] = exports
	}
	if _, err := validateContractDoc(st.Doc, r.id); err != nil {
		return err
	}
	return writeJSON(r.path(fileContracts), st.Doc)
}

// mergeOutline adds one outline pass to a copy of doc (nil for the first
// pass) and checks the merged outline; a rejected reply leaves doc as it was.
// Components and types are merged by id: a repeat of an id is dropped and the
// first emission kept. The first pass fixes the module and the conventions. A
// "more" flag in the reply is ignored: the harness decides which passes remain.
// added counts the components, types and dependencies the pass made new.
//
// Between passes only what is local and monotone is checked (schema shape,
// id syntax, duplicates, file paths), because a type may use one a later pass
// writes. final marks the last pass of the outline (or a repair pass): only
// then are the reference checks run, and an unresolved reference is not an
// error here, the planner asks for it.
func mergeOutline(doc map[string]any, have []Dependency, text, briefID string, final bool) (map[string]any, []Dependency, int, []string, error) {
	var o struct {
		Module      string           `json:"module"`
		Conventions map[string]any   `json:"conventions"`
		Components  []map[string]any `json:"components"`
		Types       []map[string]any `json:"types"`
		Deps        []map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(text), &o); err != nil {
		return nil, nil, 0, nil, fmt.Errorf("contract outline is not a JSON object (%s)", jsonErr(err))
	}
	first := doc == nil
	deps, err := ParseDependencies(o.Deps)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	var next map[string]any
	if first {
		next = map[string]any{
			"spec_version": "2.0", "brief_id": briefID, "revision": 0,
			"module": o.Module, "conventions": o.Conventions,
			"types": []any{}, "functions": []any{}, "components": []any{},
		}
	} else if next, err = copyDoc(doc); err != nil {
		return nil, nil, 0, nil, err
	}
	// The harness fills a component's exports once its functions exist; a
	// model's exports in the outline would name functions that are not there.
	added, ignored := mergeList(next, "components", o.Components, 0, "component", func(c map[string]any) {
		c["exports"] = []any{}
	})
	added, ign2 := mergeList(next, "types", o.Types, added, "type", nil)
	ignored = append(ignored, ign2...)

	merged := append([]Dependency{}, have...)
	for _, d := range deps {
		dup := false
		for _, h := range merged {
			if h.Module == d.Module {
				if h != d {
					return nil, nil, 0, nil, fmt.Errorf("contract outline lists dependency %s twice with different content", boundedModule(d.Module))
				}
				dup = true
			}
		}
		if !dup {
			merged = append(merged, d)
			added++
		}
	}
	sort.Slice(merged, func(a, b int) bool { return merged[a].Module < merged[b].Module })
	var verr error
	if !final || len(unresolvedFinal(next)) > 0 {
		verr = validateOutlineShape(next, briefID)
	} else {
		_, verr = validateContractDoc(resolveRefs(next), briefID)
	}
	if verr != nil {
		return nil, nil, 0, nil, verr
	}
	return next, merged, added, ignored, nil
}

var (
	idSyntaxRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	// An id is named in an error or a prompt only when it has the syntax of an
	// id, cut to this length; anything else is reported by its length.
	maxBoundedID = 64
)

// boundedID is how a model-supplied id may appear in an error: quoted when it
// has the syntax of an id and at most 64 bytes, otherwise only its length (a
// cut prefix would read like a real id).
func boundedID(id string) string {
	if !idSyntaxRE.MatchString(id) || len(id) > maxBoundedID {
		return fmt.Sprintf("<%d bytes>", len(id))
	}
	return fmt.Sprintf("%q", id)
}

// boundedModule is boundedID for a module path.
func boundedModule(m string) string {
	if !depModuleRE.MatchString(m) || len(m) > maxBoundedID {
		return fmt.Sprintf("<%d bytes>", len(m))
	}
	return fmt.Sprintf("%q", m)
}

// unresolvedUses lists, in order of first use, the ids a declaration's uses
// names that no type or function declares. A function's pending
// reference is not listed (see unresolvedFinal).
func unresolvedUses(doc map[string]any) []string { return unresolved(doc, false) }

// unresolvedFinal is unresolvedUses for a contract whose components are all
// written: a function's pending reference counts too, named by its type form.
func unresolvedFinal(doc map[string]any) []string { return unresolved(doc, true) }

func unresolved(doc map[string]any, final bool) []string {
	declared := declaredIDs(doc)
	var out []string
	seen := map[string]bool{}
	for _, key := range []string{"types", "functions"} {
		for _, o := range objects(doc[key]) {
			for _, u := range strList(o["uses"]) {
				if t, f, ok := pendingRef(u); ok {
					// Before the components are written no function exists, so a
					// function's pending reference waits; a type's is a missing
					// type the outline repair asks for.
					if declared[t] || declared[f] || (!final && key == "functions") {
						continue
					}
					u = t
				}
				if !declared[u] && !seen[u] {
					seen[u] = true
					out = append(out, u)
				}
			}
		}
	}
	return out
}

// unresolvedPromptText lists up to 50 of the ids that pass the id syntax, one
// per line, and says how many more are not shown.
func unresolvedPromptText(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var shown []string
	for _, id := range ids {
		if idSyntaxRE.MatchString(id) && len(id) <= maxBoundedID && len(shown) < maxUnresolvedInPrompt {
			shown = append(shown, id)
		}
	}
	text := strings.Join(shown, "\n")
	if hidden := len(ids) - len(shown); hidden > 0 {
		text += fmt.Sprintf("\n(and %d more ids not shown)", hidden)
	}
	return text
}

// unresolvedErr is the error after the repair passes are spent.
func unresolvedErr(stage string, ids []string) error {
	var named []string
	for _, id := range ids {
		if idSyntaxRE.MatchString(id) && len(named) < maxUnresolvedInError {
			named = append(named, boundedID(id))
		}
	}
	return fmt.Errorf("stage %s: %d ids are used but never declared after %d repair passes (named: %s; other ids fail the id syntax or are not shown)",
		stage, len(ids), maxOutlineRepairs, strings.Join(named, ", "))
}

// mergeByID appends the new objects to list. A repeat of an id (within the
// reply or of an earlier one) is dropped and the first emission kept: silently
// when the content is identical, otherwise it is returned in ignored, named
// with boundedID (an id that fails the id syntax is reported by length). That
// is model noise, not a plan defect, and nothing the brief needs is lost
// because the first emission stays. added counts the objects that were new.
// prep, when set, normalises an object before it is compared. others holds the
// ids of the other lists (components, types, functions share one id namespace):
// an id in it is dropped and reported like any repeat, whatever its content.
func mergeByID(list any, in []map[string]any, added int, what string, prep func(map[string]any), others map[string]bool) ([]any, int, []string) {
	out, _ := list.([]any)
	at := map[string]int{}
	for i, x := range out {
		if m, ok := x.(map[string]any); ok {
			if id, _ := m["id"].(string); id != "" {
				at[id] = i
			}
		}
	}
	var ignored []string
	for _, m := range in {
		if prep != nil {
			prep(m)
		}
		id, _ := m["id"].(string)
		if others[id] && id != "" {
			// The id belongs to a component, type or function of another list:
			// one namespace, first emission wins.
			ignored = append(ignored, what+" "+boundedID(id))
			continue
		}
		if i, ok := at[id]; ok && id != "" {
			old, _ := json.Marshal(out[i])
			cur, _ := json.Marshal(m)
			if !bytes.Equal(old, cur) {
				ignored = append(ignored, what+" "+boundedID(id))
			}
			continue
		}
		if id != "" {
			at[id] = len(out)
		}
		out = append(out, m)
		added++
	}
	return out, added, ignored
}

// mergeList is mergeByID for the list doc[key] ("components", "types" or
// "functions"): the ids of the other two lists are the others, and the merged
// list is stored back in doc.
func mergeList(doc map[string]any, key string, in []map[string]any, added int, what string, prep func(map[string]any)) (int, []string) {
	others := map[string]bool{}
	for _, k := range []string{"components", "types", "functions"} {
		if k == key {
			continue
		}
		for _, o := range objects(doc[k]) {
			if id, _ := o["id"].(string); id != "" {
				others[id] = true
			}
		}
	}
	out, added, ignored := mergeByID(doc[key], in, added, what, prep, others)
	doc[key] = out
	return added, ignored
}

// outlineFixedText is the module and conventions the first pass fixed, for the
// continuation prompt.
func outlineFixedText(doc map[string]any) string {
	if doc == nil {
		return "(not written yet: this is the first pass, so write them)"
	}
	return mustJSON(map[string]any{"module": doc["module"], "conventions": doc["conventions"]})
}

const (
	maxSharedIDs        = 100
	maxEmittedRecent    = 150 // ids listed in full besides the shared ones
	maxEmittedSummaries = 100 // withheld components named in the summary line
	emittedSummaryBytes = 24  // bytes of one summary entry
	maxEmittedTextBytes = 16000
)

func pick(first bool, a, b [][]string) [][]string {
	if first {
		return a
	}
	return b
}

// outlineIDs lists the component then the type ids of doc, at most max.
func outlineIDs(doc map[string]any, max int) []string {
	var out []string
	for _, key := range []string{"components", "types"} {
		for _, o := range objects(doc[key]) {
			if len(out) < max {
				out = append(out, fmt.Sprint(o["id"]))
			}
		}
	}
	return out
}

// idShown is an id as it appears in a prompt: as is, or its length when it is long.
func idShown(id string) string {
	if len(id) > maxBoundedID {
		return fmt.Sprintf("<%d bytes>", len(id))
	}
	return id
}

func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// outlineEmittedText lists the component and type ids earlier passes wrote.
// While they are few the list is complete. Beyond that it names the shared
// ids, the most recent ones (maxEmittedRecent), and a count of the rest with
// one short entry per withheld component, so a batch prompt does not grow with
// the brief. Duplicate handling never depends on the model seeing this list.
func outlineEmittedText(doc map[string]any, shared []string) string {
	return outlineEmittedTextLimits(doc, shared, maxEmittedRecent, maxEmittedSummaries)
}

// outlineEmittedTextAt is the list for a prompt level: from level 2 fewer ids
// are listed in full and no summaries.
func outlineEmittedTextAt(doc map[string]any, shared []string, level int) string {
	switch {
	case level >= 3:
		return outlineEmittedTextLimits(doc, shared, 15, 0)
	case level == 2:
		return outlineEmittedTextLimits(doc, shared, 40, 0)
	}
	return outlineEmittedText(doc, shared)
}

func outlineEmittedTextLimits(doc map[string]any, shared []string, recent, summaries int) string {
	if doc == nil {
		return "(nothing yet)"
	}
	var comps, types []map[string]any
	comps, types = objects(doc["components"]), objects(doc["types"])
	idOf := func(o map[string]any) string { return idShown(fmt.Sprint(o["id"])) }
	join := func(list []map[string]any) string {
		var ids []string
		for _, o := range list {
			ids = append(ids, idOf(o))
		}
		if len(ids) == 0 {
			return "(none)"
		}
		return strings.Join(ids, ", ")
	}
	if len(comps)+len(types) <= recent {
		return fmt.Sprintf("components: %s\ntypes: %s", join(comps), join(types))
	}
	isShared := map[string]bool{}
	var sharedShown []string
	for _, id := range shared {
		isShared[id] = true
		sharedShown = append(sharedShown, idShown(id))
	}
	var restC, restT []map[string]any
	for _, o := range comps {
		if !isShared[fmt.Sprint(o["id"])] {
			restC = append(restC, o)
		}
	}
	for _, o := range types {
		if !isShared[fmt.Sprint(o["id"])] {
			restT = append(restT, o)
		}
	}
	takeC := len(restC)
	if takeC > recent*2/3 {
		takeC = recent * 2 / 3
	}
	takeT := len(restT)
	if takeT > recent-takeC {
		takeT = recent - takeC
	}
	if takeC < len(restC) && takeC+takeT < recent {
		takeC = len(restC)
		if takeC > recent-takeT {
			takeC = recent - takeT
		}
	}
	withheld := len(restC) - takeC + len(restT) - takeT
	var b strings.Builder
	if len(sharedShown) > 0 {
		fmt.Fprintf(&b, "shared: %s\n", strings.Join(sharedShown, ", "))
	}
	fmt.Fprintf(&b, "components (most recent): %s\n", join(restC[len(restC)-takeC:]))
	fmt.Fprintf(&b, "types (most recent): %s\n", join(restT[len(restT)-takeT:]))
	fmt.Fprintf(&b, "(and %d more ids, names withheld)", withheld)
	var sums []string
	for _, o := range restC[:len(restC)-takeC] {
		if len(sums) >= summaries {
			break
		}
		sum := idOf(o)
		if ex := strList(o["exports"]); len(ex) > 0 {
			sum += ":" + ex[0]
		}
		sums = append(sums, cutBytes(sum, emittedSummaryBytes))
	}
	if len(sums) > 0 {
		fmt.Fprintf(&b, "\nearlier components (id, first export; at most %d shown): %s", summaries, strings.Join(sums, ", "))
	}
	return cutBytes(b.String(), maxEmittedTextBytes)
}

// dropSharedExtras removes from the shared pass's reply every component other
// than "types": the feature components are written by the batches. dropped
// names them (bounded ids). A reply that is not a JSON object is returned as it is.
func dropSharedExtras(text string) (string, []string, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var reply map[string]any
	if err := dec.Decode(&reply); err != nil || reply == nil {
		return text, nil, nil
	}
	arr, ok := reply["components"].([]any)
	if !ok {
		return text, nil, nil
	}
	var kept []any
	var dropped []string
	for _, x := range arr {
		if o, ok := x.(map[string]any); ok {
			if id, _ := o["id"].(string); id != "types" {
				dropped = append(dropped, "component "+boundedID(id))
				continue
			}
		}
		kept = append(kept, x)
	}
	if len(dropped) == 0 {
		return text, nil, nil
	}
	reply["components"] = kept
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reply); err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(out.String()), dropped, nil
}

// mergePass adds one component pass to a copy of doc and checks the merged
// contract locally (references wait until every component is written). It
// never changes doc, so a rejected reply leaves no trace. A function or type
// that repeats an id already written keeps its first emission; ignored lists
// the repeats that differed.
func mergePass(doc map[string]any, component, text, briefID string) (map[string]any, bool, []string, error) {
	var pass struct {
		Types     []map[string]any `json:"types"`
		Functions []map[string]any `json:"functions"`
		More      bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(text), &pass); err != nil {
		return nil, false, nil, fmt.Errorf("contract reply for %s is not a JSON object (%s)", component, jsonErr(err))
	}
	next, err := copyDoc(doc)
	if err != nil {
		return nil, false, nil, err
	}
	for _, f := range pass.Functions {
		f["component"] = component
	}
	added, ignored := mergeList(next, "types", pass.Types, 0, "type", nil)
	added, ign2 := mergeList(next, "functions", pass.Functions, added, "function", nil)
	ignored = append(ignored, ign2...)
	if err := validateOutlineShape(next, briefID); err != nil {
		return nil, false, nil, err
	}
	// A reply that says more but adds nothing new is the model repeating
	// itself: the component is finished (a continuation needs progress).
	return next, pass.More && added > 0, ignored, nil
}

var componentIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateContractDoc runs the schema and reference checks of contract.Load
// and then the ones only the planner knows: ids that would collide in the
// tree or with a stage name, files that leave the repository, signatures that
// are not Go.
func validateContractDoc(doc map[string]any, briefID string) (*contract.Contracts, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	c, err := contract.Load(raw)
	if err != nil {
		return nil, loadErr(err, doc)
	}
	return c, localChecks(c, briefID)
}

// validateOutlineShape is validateContractDoc without the reference checks.
func validateOutlineShape(doc map[string]any, briefID string) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		return loadErr(err, doc)
	}
	var c contract.Contracts
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range append(typeIDs(&c), fnIDs(&c)...) {
		if seen[id] {
			return fmt.Errorf("contract: duplicate id %s", boundedID(id))
		}
		seen[id] = true
	}
	return localChecks(&c, briefID)
}

func typeIDs(c *contract.Contracts) []string {
	var out []string
	for _, t := range c.Types {
		out = append(out, t.ID)
	}
	return out
}

func fnIDs(c *contract.Contracts) []string {
	var out []string
	for _, f := range c.Functions {
		out = append(out, f.ID)
	}
	return out
}

// localChecks are the checks only the planner knows, and that do not need the
// complete contract: ids that would collide in the tree or with a stage name,
// files that leave the repository, signatures that are not Go.
func localChecks(c *contract.Contracts, briefID string) error {
	if c.BriefID != briefID {
		return fmt.Errorf("contract: brief_id does not match the run id (%d bytes)", len(c.BriefID))
	}
	comps := map[string]bool{}
	for _, comp := range c.Components {
		switch {
		case !componentIDRE.MatchString(comp.ID):
			return fmt.Errorf("contract: component id %s must be lower case letters, digits and dashes", boundedID(comp.ID))
		case tree.IsReservedComponentID(comp.ID) || comp.ID == "outline":
			return fmt.Errorf("contract: component id %s is reserved (logs, attempts and decisions name folders of the run, outline a stage)", boundedID(comp.ID))
		case comp.ID == briefID:
			return fmt.Errorf("contract: component id %s is the run id", boundedID(comp.ID))
		case comps[comp.ID]:
			return fmt.Errorf("contract: duplicate component id %s", boundedID(comp.ID))
		}
		comps[comp.ID] = true
	}
	for _, t := range c.Types {
		if err := cleanGoFile(t.File); err != nil {
			return fmt.Errorf("contract: type %s: %w", boundedID(t.ID), err)
		}
	}
	for _, f := range c.Functions {
		if comps[f.ID] || f.ID == briefID {
			return fmt.Errorf("contract: function id %s is also a component or the run id", boundedID(f.ID))
		}
		if !comps[f.Component] {
			return fmt.Errorf("contract: function %s belongs to unknown component (%d bytes)", boundedID(f.ID), len(f.Component))
		}
		if err := cleanGoFile(f.File); err != nil {
			return fmt.Errorf("contract: function %s: %w", boundedID(f.ID), err)
		}
		if _, err := parseSignature(f.Signature); err != nil {
			return fmt.Errorf("contract: function %s: %w", boundedID(f.ID), err)
		}
	}
	return nil
}

// cleanGoFile accepts a Go file path that stays inside the repository:
// relative, already clean, no parent steps.
func cleanGoFile(file string) error {
	switch {
	case file == "":
		return errors.New("file is empty")
	case strings.Contains(file, `\`) || path.IsAbs(file):
		return fmt.Errorf("file (%d bytes) must be a relative path with forward slashes", len(file))
	case path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../"):
		return fmt.Errorf("file (%d bytes) must be a clean path inside the repository", len(file))
	case !strings.HasSuffix(file, ".go"):
		return fmt.Errorf("file (%d bytes) is not a Go file", len(file))
	}
	return nil
}

// parseSignature parses one Go function signature line.
func parseSignature(sig string) (*ast.FuncDecl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+sig+" {}\n", parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("signature (%d bytes) is not valid Go: %s", len(sig), syntaxErr(err))
	}
	if len(f.Decls) != 1 {
		return nil, fmt.Errorf("signature (%d bytes) must declare exactly one function", len(sig))
	}
	fd, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("signature (%d bytes) is not a function", len(sig))
	}
	return fd, nil
}

// contractItemSchemas returns the item definitions of the named contract
// arrays ("types", "functions") as JSON, for the prompts.
func contractItemSchemas(names ...string) (string, error) {
	raw, err := schema.Raw(schema.KindContract)
	if err != nil {
		return "", err
	}
	var doc struct {
		Properties map[string]struct {
			Items json.RawMessage `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	out := map[string]json.RawMessage{}
	for _, n := range names {
		out[strings.TrimSuffix(n, "s")] = doc.Properties[n].Items
	}
	b, err := json.MarshalIndent(out, "", "  ")
	return string(b), err
}

func copyDoc(doc map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// objects returns the JSON objects of a decoded array.
func objects(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// strList returns the strings of a decoded array.
func strList(v any) []string {
	if ss, ok := v.([]string); ok {
		return append([]string{}, ss...)
	}
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// declaredText lists every declaration written so far, id first, the way a
// component pass needs to see them.
func declaredText(doc map[string]any) string {
	var b strings.Builder
	for _, t := range objects(doc["types"]) {
		fmt.Fprintf(&b, "%v:\n%v\n\n", t["id"], t["decl"])
	}
	for _, f := range objects(doc["functions"]) {
		fmt.Fprintf(&b, "%v: %v\n", f["id"], f["signature"])
	}
	if b.Len() == 0 {
		return "(nothing yet)"
	}
	return strings.TrimRight(b.String(), "\n")
}

func writtenText(doc map[string]any, component string) string {
	var ids []string
	for _, f := range objects(doc["functions"]) {
		if f["component"] == component {
			ids = append(ids, fmt.Sprint(f["id"]))
		}
	}
	if len(ids) == 0 {
		return "(none yet)"
	}
	return strings.Join(ids, ", ")
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a feature heading into the id its component is expected to have.
func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// briefSection is the part of the brief a component came from: the feature
// whose slug is the component id, or the Architecture section.
func briefSection(r *run, component string) string {
	for _, f := range r.brief.Features {
		if slug(f.Name) == component {
			return "### " + f.Name + "\n\n" + f.Body
		}
	}
	return r.brief.Sections["Architecture"]
}

// mergeRepair adds a repair reply to a copy of doc. A new function names its
// component in "component". A repeat of an id already written keeps the first
// emission. The merged contract is checked locally only.
func mergeRepair(doc map[string]any, text, briefID string) (map[string]any, []string, error) {
	var pass struct {
		Types     []map[string]any `json:"types"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(text), &pass); err != nil {
		return nil, nil, fmt.Errorf("contract repair reply is not a JSON object (%s)", jsonErr(err))
	}
	next, err := copyDoc(doc)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	for _, key := range []string{"components", "types", "functions"} {
		for _, o := range objects(next[key]) {
			id, _ := o["id"].(string)
			have[id] = true
		}
	}
	for _, f := range pass.Functions {
		id, _ := f["id"].(string)
		if c, _ := f["component"].(string); c == "" && !have[id] {
			return nil, nil, fmt.Errorf("contract repair: function %s names no component", boundedID(id))
		}
	}
	_, ignored := mergeList(next, "types", pass.Types, 0, "type", nil)
	_, ign2 := mergeList(next, "functions", pass.Functions, 0, "function", nil)
	ignored = append(ignored, ign2...)
	if err := validateOutlineShape(next, briefID); err != nil {
		return nil, nil, err
	}
	return next, ignored, nil
}

// unresolvedOwnerComponents lists the components whose functions use an id in
// ids, in document order.
func unresolvedOwnerComponents(doc map[string]any, ids []string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range objects(doc["functions"]) {
		c, _ := f["component"].(string)
		for _, u := range strList(f["uses"]) {
			if t, _, ok := pendingRef(u); ok {
				u = t
			}
			if want[u] && c != "" && !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// unresolvedOwnersText lists up to 50 unresolved ids that pass the id syntax,
// each with the declarations that use it and their components.
func unresolvedOwnersText(doc map[string]any, ids []string) string {
	users := map[string][]string{}
	hint := map[string]string{} // canonical id -> its function form, for a pending reference
	for _, key := range []string{"types", "functions"} {
		for _, o := range objects(doc[key]) {
			id, _ := o["id"].(string)
			who := id
			if key == "functions" {
				who += " in component " + fmt.Sprint(o["component"])
			}
			for _, u := range strList(o["uses"]) {
				if t, f, ok := pendingRef(u); ok {
					u = t
					hint[t] = f
				}
				users[u] = append(users[u], who)
			}
		}
	}
	var lines []string
	for _, id := range ids {
		if idSyntaxRE.MatchString(id) && len(id) <= maxBoundedID && len(lines) < maxUnresolvedInPrompt {
			kind := ""
			if f, ok := hint[id]; ok {
				kind = " [a type, or a function whose id is " + f + "]"
			}
			lines = append(lines, id+kind+" (used by "+strings.Join(users[id], ", ")+")")
		}
	}
	text := strings.Join(lines, "\n")
	if hidden := len(ids) - len(lines); hidden > 0 {
		text += fmt.Sprintf("\n(and %d more ids not shown)", hidden)
	}
	return text
}
