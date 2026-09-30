package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"
)

// contractState is _state/contract.json: the contract as far as it has been
// written, and which components are finished. contracts.json itself is only
// written once every pass is done.
type contractState struct {
	Doc  map[string]any `json:"doc"`
	Done []string       `json:"done"`
}

func contractDone(r *run) bool { return exists(r.path(fileContracts)) }

// contract is the Contract stage (Wave 0), built in passes so that a large
// brief makes more calls rather than a coarser contract: one outline call,
// then one call per component, repeated while the model says more remains.
func (p *Planner) contract(ctx context.Context, r *run) error {
	as, err := loadAnswers(r)
	if err != nil {
		return err
	}
	var st contractState
	found, err := readJSON(r.path(stateContract), &st)
	if err != nil {
		return err
	}
	if !found {
		typeSchema, err := contractItemSchemas("types")
		if err != nil {
			return err
		}
		prompt, err := render("contract_outline", map[string]string{
			"Brief": string(r.src), "Answers": answersText(as), "TypeSchema": typeSchema})
		if err != nil {
			return err
		}
		var deps []Dependency
		cs := callSpec{stage: "contract:outline", taskType: "contract", scope: router.ScopeBrief, maxTokens: maxTokensContract}
		if err := p.call(ctx, r, cs, prompt, func(text string) error {
			doc, ds, err := parseOutline(StripReply(text), r.id)
			if err != nil {
				return err
			}
			st.Doc, deps = doc, ds
			return nil
		}); err != nil {
			return err
		}
		if err := writeJSON(r.path(fileDependencies), deps); err != nil {
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
			prompt, err := render("contract_component", map[string]string{
				"Component":    mustJSON(comp),
				"Outline":      mustJSON(map[string]any{"module": st.Doc["module"], "conventions": st.Doc["conventions"], "components": st.Doc["components"]}),
				"Declared":     declaredText(st.Doc),
				"Written":      writtenText(st.Doc, id),
				"BriefSection": briefSection(r, id),
				"Answers":      answersText(as),
				"ItemSchemas":  itemSchemas,
			})
			if err != nil {
				return err
			}
			more := false
			cs := callSpec{stage: "contract:" + id, taskType: "contract", scope: router.ScopeComponent, maxTokens: maxTokensContract}
			if err := p.call(ctx, r, cs, prompt, func(text string) error {
				doc, m, err := mergePass(st.Doc, id, StripReply(text), r.id)
				if err != nil {
					return err
				}
				st.Doc, more = doc, m
				return nil
			}); err != nil {
				return err
			}
			if !more {
				st.Done = append(st.Done, id)
			}
			if err := writeJSON(r.path(stateContract), st); err != nil {
				return err
			}
			if !more {
				break
			}
		}
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

// parseOutline turns the outline reply into the start of contracts.json: the
// harness supplies the version, the brief id and an empty function list.
func parseOutline(text, briefID string) (map[string]any, []Dependency, error) {
	var o struct {
		Module      string           `json:"module"`
		Conventions map[string]any   `json:"conventions"`
		Components  []map[string]any `json:"components"`
		Types       []map[string]any `json:"types"`
		Deps        []map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(text), &o); err != nil {
		return nil, nil, fmt.Errorf("contract outline is not a JSON object (%s)", jsonErr(err))
	}
	if len(o.Components) == 0 {
		return nil, nil, errors.New("contract outline lists no component")
	}
	deps, err := ParseDependencies(o.Deps)
	if err != nil {
		return nil, nil, err
	}
	comps := make([]any, 0, len(o.Components))
	for _, c := range o.Components {
		if _, ok := c["exports"].([]any); !ok {
			c["exports"] = []any{}
		}
		comps = append(comps, c)
	}
	types := make([]any, 0, len(o.Types))
	for _, t := range o.Types {
		types = append(types, t)
	}
	doc := map[string]any{
		"spec_version": "2.0", "brief_id": briefID, "revision": 0,
		"module": o.Module, "conventions": o.Conventions,
		"types": types, "functions": []any{}, "components": comps,
	}
	if _, err := validateContractDoc(doc, briefID); err != nil {
		return nil, nil, err
	}
	return doc, deps, nil
}

// mergePass adds one component pass to a copy of doc and validates the whole
// contract again. It never changes doc, so a rejected reply leaves no trace.
func mergePass(doc map[string]any, component, text, briefID string) (map[string]any, bool, error) {
	var pass struct {
		Types     []map[string]any `json:"types"`
		Functions []map[string]any `json:"functions"`
		More      bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(text), &pass); err != nil {
		return nil, false, fmt.Errorf("contract reply for %s is not a JSON object (%s)", component, jsonErr(err))
	}
	if pass.More && len(pass.Functions) == 0 {
		return nil, false, fmt.Errorf("contract reply for %s says more remains but holds no function", component)
	}
	next, err := copyDoc(doc)
	if err != nil {
		return nil, false, err
	}
	types, _ := next["types"].([]any)
	for _, t := range pass.Types {
		types = append(types, t)
	}
	fns, _ := next["functions"].([]any)
	for _, f := range pass.Functions {
		f["component"] = component
		fns = append(fns, f)
	}
	next["types"], next["functions"] = types, fns
	if _, err := validateContractDoc(next, briefID); err != nil {
		return nil, false, err
	}
	return next, pass.More, nil
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
	if c.BriefID != briefID {
		return nil, fmt.Errorf("contract: brief_id does not match the run id (%d bytes)", len(c.BriefID))
	}
	comps := map[string]bool{}
	for _, comp := range c.Components {
		switch {
		case !componentIDRE.MatchString(comp.ID):
			return nil, fmt.Errorf("contract: component id %q must be lower case letters, digits and dashes", comp.ID)
		case comp.ID == "logs" || comp.ID == "outline":
			return nil, fmt.Errorf("contract: component id %q is reserved", comp.ID)
		case comp.ID == briefID:
			return nil, fmt.Errorf("contract: component id %q is the run id", comp.ID)
		case comps[comp.ID]:
			return nil, fmt.Errorf("contract: duplicate component id %q", comp.ID)
		}
		comps[comp.ID] = true
	}
	for _, t := range c.Types {
		if err := cleanGoFile(t.File); err != nil {
			return nil, fmt.Errorf("contract: type %s: %w", t.ID, err)
		}
	}
	for _, f := range c.Functions {
		if comps[f.ID] || f.ID == briefID {
			return nil, fmt.Errorf("contract: function id %q is also a component or the run id", f.ID)
		}
		if !comps[f.Component] {
			return nil, fmt.Errorf("contract: function %s belongs to unknown component (%d bytes)", f.ID, len(f.Component))
		}
		if err := cleanGoFile(f.File); err != nil {
			return nil, fmt.Errorf("contract: function %s: %w", f.ID, err)
		}
		if _, err := parseSignature(f.Signature); err != nil {
			return nil, fmt.Errorf("contract: function %s: %w", f.ID, err)
		}
	}
	return c, nil
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
