package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"
)

// The final contract is checked as a whole. A field the model left out of one
// node (a function without its doc, say) is found there and asked for again,
// for that node only, instead of ending a run that took hours.

// schemaRepairStage is the stage of the passes that ask for missing fields of
// named nodes. A component id cannot start with an underscore.
const schemaRepairStage = "contract:_schema"

const (
	maxSchemaRepairs     = 2  // passes over the whole set of gaps
	maxSchemaRepairBatch = 10 // nodes per call
	maxDefaultedIDsShown = 10
)

// repairableFields are the required fields of a node the model can be asked for
// again. The id is the key and the component is set by the harness.
var repairableFields = map[string][]string{
	"functions": {"package", "file", "signature", "doc"},
	"types":     {"package", "file", "decl"},
}

// schemaIssue is one failing value of the contract schema, located by node.
type schemaIssue struct {
	list    string // functions, types, components, or "" for the document itself
	index   int
	id      string // the node's id, empty when it has none
	field   string // the failing property, empty for a node-level failure
	kind    string // missing, invalid or node
	pointer string // the JSON pointer, kept for failures that are not in a node
}

func (i schemaIssue) what() string {
	switch i.list {
	case "functions":
		return "function"
	case "types":
		return "type"
	case "components":
		return "component"
	}
	return ""
}

// idOK is true when the node's id has the schema's syntax and is short enough to
// name: an id that fails the schema is never echoed.
func (i schemaIssue) idOK() bool {
	if i.id == "" || len(i.id) > maxBoundedID {
		return false
	}
	if i.list == "functions" {
		return fnIDRE.MatchString(i.id)
	}
	return typeIDRE.MatchString(i.id)
}

// repairable is true for a missing or invalid field the model can write again
// for a node it can be asked about by name.
func (i schemaIssue) repairable() bool {
	if i.kind == "node" || !i.idOK() {
		return false
	}
	for _, f := range repairableFields[i.list] {
		if f == i.field {
			return true
		}
	}
	return false
}

// String names the node (bounded) and the field, never a bare pointer for a
// node; a failure outside the nodes keeps its pointer.
func (i schemaIssue) String() string {
	if i.list == "" {
		return i.pointer
	}
	who := fmt.Sprintf("%s #%d", i.what(), i.index)
	if i.idOK() {
		who = i.what() + " " + boundedID(i.id)
	}
	switch i.kind {
	case "missing":
		return who + " is missing " + i.field
	case "invalid":
		return who + " has an invalid " + i.field
	}
	return who + " is not valid"
}

// issuesOf turns a schema failure into located issues, in document order.
func issuesOf(ve *jsonschema.ValidationError, doc map[string]any) []schemaIssue {
	var out []schemaIssue
	seen := map[string]bool{}
	add := func(i schemaIssue) {
		key := fmt.Sprintf("%s/%d/%s/%s/%s", i.list, i.index, i.field, i.kind, i.pointer)
		if !seen[key] {
			seen[key] = true
			out = append(out, i)
		}
	}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		loc := e.InstanceLocation
		pointer := "/" + strings.Join(loc, "/")
		nodeIssue := schemaIssue{list: "", pointer: pointer}
		var idx int
		if len(loc) >= 2 && (loc[0] == "functions" || loc[0] == "types" || loc[0] == "components") {
			if _, err := fmt.Sscanf(loc[1], "%d", &idx); err == nil {
				nodeIssue = schemaIssue{list: loc[0], index: idx}
				if objs := objects(doc[loc[0]]); idx >= 0 && idx < len(objs) {
					nodeIssue.id, _ = objs[idx]["id"].(string)
				}
			}
		}
		switch {
		case nodeIssue.list == "":
			text := pointer
			if req, ok := e.ErrorKind.(*kind.Required); ok {
				text += " is missing " + strings.Join(req.Missing, ", ")
			} else {
				text += " is not valid"
			}
			nodeIssue.pointer = text
			add(nodeIssue)
		default:
			if req, ok := e.ErrorKind.(*kind.Required); ok && len(loc) == 2 {
				for _, f := range req.Missing {
					i := nodeIssue
					i.kind, i.field = "missing", f
					add(i)
				}
			} else if len(loc) >= 3 {
				i := nodeIssue
				i.kind, i.field = "invalid", loc[2]
				add(i)
			} else {
				i := nodeIssue
				i.kind = "node"
				add(i)
			}
		}
	}
	walk(ve)
	return out
}

// schemaIssues validates doc against the contract schema and lists what fails;
// nil when the schema passes.
func schemaIssues(doc map[string]any) []schemaIssue {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	err = schema.Validate(schema.KindContract, raw)
	var ve *jsonschema.ValidationError
	if err == nil || !errors.As(err, &ve) {
		return nil
	}
	return issuesOf(ve, doc)
}

// nodeSchemaErr is the error text of a schema failure: at most maxSchemaPointers
// issues, each naming its node and field, and a count of the rest.
func nodeSchemaErr(ve *jsonschema.ValidationError, doc map[string]any) string {
	return issuesText("the contract fails its schema", issuesOf(ve, doc))
}

func issuesText(prefix string, issues []schemaIssue) string {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		parts = append(parts, i.String())
	}
	if len(parts) > maxSchemaPointers {
		parts = append(parts[:maxSchemaPointers], fmt.Sprintf("and %d more", len(parts)-maxSchemaPointers))
	}
	return prefix + ": " + strings.Join(parts, "; ")
}

// defaultDocs gives a function whose doc is still missing a fixed text built
// from its signature's name and its component. Only the doc (a documentation
// comment, not a behaviour contract) is ever defaulted; a function whose
// signature cannot be read is left alone. It returns how many were defaulted
// and their bounded ids (at most maxDefaultedIDsShown).
func defaultDocs(doc map[string]any) (int, []string) {
	n := 0
	var ids []string
	for _, f := range objects(doc["functions"]) {
		if d, _ := f["doc"].(string); d != "" {
			continue
		}
		sig, _ := f["signature"].(string)
		fd, err := parseSignature(sig)
		if err != nil {
			continue
		}
		comp, _ := f["component"].(string)
		f["doc"] = fd.Name.Name + " implements " + comp + " behaviour described in the brief."
		n++
		if len(ids) < maxDefaultedIDsShown {
			id, _ := f["id"].(string)
			ids = append(ids, boundedID(id))
		}
	}
	return n, ids
}

// gapNode is one node to ask about: its list, id and the fields it lacks.
type gapNode struct {
	list, id string
	fields   []string
}

func gapNodes(issues []schemaIssue) []gapNode {
	at := map[string]int{}
	var out []gapNode
	for _, i := range issues {
		key := i.list + "\x00" + i.id
		k, ok := at[key]
		if !ok {
			k = len(out)
			at[key] = k
			out = append(out, gapNode{list: i.list, id: i.id})
		}
		out[k].fields = append(out[k].fields, i.field)
	}
	return out
}

// mergeFieldRepair takes from a reply, for each asked node, only the fields it
// lacked, and only a value of the right kind. Everything else in the reply (other
// nodes, other fields) is ignored, so the first emission stands. It returns the
// number of fields filled.
func mergeFieldRepair(doc map[string]any, text string, asked []gapNode) (map[string]any, int, error) {
	var reply map[string][]map[string]any
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, 0, fmt.Errorf("contract schema repair reply is not a JSON object (%s)", jsonErr(err))
	}
	next, err := copyDoc(doc)
	if err != nil {
		return nil, 0, err
	}
	want := map[string]map[string]bool{}
	for _, a := range asked {
		fs := map[string]bool{}
		for _, f := range a.fields {
			fs[f] = true
		}
		want[a.list+"\x00"+a.id] = fs
	}
	filled := 0
	for _, list := range []string{"functions", "types"} {
		byID := map[string]map[string]any{}
		for _, o := range objects(next[list]) {
			if id, _ := o["id"].(string); id != "" {
				byID[id] = o
			}
		}
		for _, m := range reply[list] {
			id, _ := m["id"].(string)
			fs, node := want[list+"\x00"+id], byID[id]
			if fs == nil || node == nil {
				continue
			}
			for f := range fs {
				if v, ok := m[f].(string); ok && strings.TrimSpace(v) != "" {
					node[f] = v
					filled++
				}
			}
		}
	}
	return next, filled, nil
}

// gapNodesText lists the nodes to fix: id, the fields missing (the schema's own
// names) and the node as it stands.
func gapNodesText(doc map[string]any, nodes []gapNode, level int) string {
	var b strings.Builder
	for _, n := range nodes {
		for _, o := range objects(doc[n.list]) {
			if o["id"] != n.id {
				continue
			}
			fields := append([]string(nil), n.fields...)
			sort.Strings(fields)
			fmt.Fprintf(&b, "%s %q is missing: %s\n%s\n\n", strings.TrimSuffix(n.list, "s"), n.id, strings.Join(fields, ", "), mustJSON(trimNode(o, level)))
		}
	}
	return strings.TrimSpace(b.String())
}

// repairSchema checks the merged contract against the schema and asks for the
// fields it lacks, node by node, in up to maxSchemaRepairs passes. A doc still
// missing afterwards gets its fixed text; any other gap ends the stage with an
// error that names the nodes and fields. The state is written after every call,
// so nothing is asked twice and a resume carries on from here.
func (p *Planner) repairSchema(ctx context.Context, r *run, st *contractState, answers, itemSchemas string) error {
	for {
		issues := schemaIssues(st.Doc)
		if len(issues) == 0 {
			return nil
		}
		fatal := false
		var asked []schemaIssue
		for _, i := range issues {
			if i.repairable() {
				asked = append(asked, i)
			} else {
				fatal = true
			}
		}
		if fatal || len(asked) == 0 {
			return errors.New(issuesText("stage contract: the contract fails its schema in fields that cannot be asked for again", issues))
		}
		if st.SchemaRepairs >= maxSchemaRepairs {
			break
		}
		nodes := gapNodes(asked)
		for lo := 0; lo < len(nodes); lo += maxSchemaRepairBatch {
			hi := lo + maxSchemaRepairBatch
			if hi > len(nodes) {
				hi = len(nodes)
			}
			batch := nodes[lo:hi]
			build := func(level int) (string, error) {
				owners := nodeOwners(st.Doc, batch)
				return render("contract_schema_fix", map[string]string{
					"Nodes": gapNodesText(st.Doc, batch, level), "Count": fmt.Sprint(len(batch)),
					"Answers": answers, "ItemSchemas": itemSchemas,
					"BriefSections": briefExcerpt(r, featuresOfComponents(r, owners), repairExcerptLevel(level)),
					"Outline":       mustJSON(map[string]any{"module": st.Doc["module"], "components": componentsByID(st.Doc, owners)})})
			}
			var notes idNotes
			cs := callSpec{stage: schemaRepairStage, taskType: "contract", scope: router.ScopeBrief, maxTokens: maxTokensContract}
			if err := p.callSized(ctx, r, cs, build, func(text string) error {
				text, nt, err := normalizeReply(st.Doc, StripReply(text), replyRepair)
				if err != nil {
					return err
				}
				doc, _, err := mergeFieldRepair(st.Doc, text, batch)
				if err != nil {
					return err
				}
				st.Doc, notes = doc, nt
				return nil
			}); err != nil {
				if failedAttempt(ctx, err) {
					st.SchemaRepairs++
					if werr := writeJSON(r.path(stateContract), st); werr != nil {
						return werr
					}
				}
				return err
			}
			p.noteNormalized(st, schemaRepairStage, notes)
			if err := writeJSON(r.path(stateContract), st); err != nil {
				return err
			}
		}
		st.SchemaRepairs++
		if err := writeJSON(r.path(stateContract), st); err != nil {
			return err
		}
	}
	// The bound is spent: only a missing doc may be defaulted.
	n, ids := defaultDocs(st.Doc)
	if n > 0 {
		p.emit(events.KindWarning, "contract", "", fmt.Sprintf(
			"doc_defaulted: %d functions still lacked a doc after %d repair passes and got a fixed text from their signature (%s)",
			n, maxSchemaRepairs, strings.Join(ids, ", ")))
		if err := writeJSON(r.path(stateContract), st); err != nil {
			return err
		}
	}
	if issues := schemaIssues(st.Doc); len(issues) > 0 {
		return errors.New(issuesText(fmt.Sprintf("stage contract: the contract still fails its schema after %d repair passes", maxSchemaRepairs), issues))
	}
	return nil
}

// trimNode is a node for a prompt: complete up to level 2; at level 3 only its
// identity, signature and doc, with a declaration cut short.
func trimNode(o map[string]any, level int) map[string]any {
	if level < maxPromptLevel {
		return o
	}
	out := map[string]any{}
	for _, k := range []string{"id", "component", "package", "file", "signature", "doc"} {
		if v, ok := o[k]; ok {
			out[k] = v
		}
	}
	if d, ok := o["decl"].(string); ok {
		out["decl"] = capText(d, 300)
	}
	return out
}

// nodeOwners lists the components that own the function nodes in nodes.
func nodeOwners(doc map[string]any, nodes []gapNode) []string {
	want := map[string]bool{}
	for _, n := range nodes {
		if n.list == "functions" {
			want[n.id] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range objects(doc["functions"]) {
		id, _ := f["id"].(string)
		if c, _ := f["component"].(string); want[id] && c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// componentsByID is the component objects with these ids.
func componentsByID(doc map[string]any, ids []string) []any {
	out := []any{}
	for _, c := range objects(doc["components"]) {
		for _, id := range ids {
			if c["id"] == id {
				out = append(out, c)
			}
		}
	}
	return out
}
