package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

func normWarnings(g *rig) []events.Event {
	var out []events.Event
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if strings.Contains(e.Message, "outline_id_normalized") {
			out = append(out, e)
		}
	}
	return out
}

const pascalOutline = `{` + outlineHeadJSON + `, "components": [{"id": "Types", "package": "greet", "exports": []}, {"id": "Greeting", "package": "greet", "exports": []}, {"id": "Farewell", "package": "greet", "exports": []}],
 "types": [{"id": "NameError", "package": "greet", "file": "internal/greet/errors.go",
  "decl": "// NameError says why a name was refused.\ntype NameError struct {\n\tReason string\n}"}]}`

// The model's ids are rewritten to the id syntax, the run goes on, and a
// resume reads the same plan.
func TestPascalCaseIDsAreNormalisedEndToEnd(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"contract.outline.txt": pascalOutline}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
	w := normWarnings(g)
	if len(w) != 1 || !strings.Contains(w[0].Message, "4 ") || !strings.Contains(w[0].Message, "NameError") {
		t.Fatalf("warnings = %v, want one with the count and the old id", w)
	}
	if n := len(outlineStages(g)); n != 1 {
		t.Errorf("outline calls = %d, want 1 (no retry)", n)
	}
	if !strings.Contains(string(g.read("_state/contract.json")), `"ids_normalized": 4`) {
		t.Errorf("the count is not persisted:\n%s", g.read("_state/contract.json"))
	}
	if strings.Contains(string(g.read("contracts.json")), "NameError\"") {
		t.Error("a raw id reached contracts.json")
	}
	before := string(g.read("contracts.json"))

	// A second run of the same replies plans the same contract.
	h := newRig(t, approving(), variant(t, map[string]string{"contract.outline.txt": pascalOutline}))
	h.mustPlan(planner.Options{StopAfter: "contract"})
	if string(h.read("contracts.json")) != before {
		t.Error("normalisation is not deterministic")
	}
}

// A schema-invalid reply (not an id problem) gets one more ask that carries
// the bounded list of offending pointers, never reply text.
func TestSchemaInvalidReplyIsAskedAgainWithThePointers(t *testing.T) {
	bad := `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + comp("farewell") + `],
 "types": [{"id": "name-error", "package": "greet", "file": "internal/greet/errors.go", "CANARYfield": "CANARY reply text"}]}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt":   bad,
		"contract.outline.1.2.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := len(outlineStages(g)); n != 2 {
		t.Fatalf("outline calls = %d, want 2", n)
	}
	var second string
	seen := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:outline:1" {
			seen++
			if seen == 2 {
				for _, m := range r.Messages {
					if strings.Contains(m.Content, "That reply could not be used") {
						second = m.Content
					}
				}
			}
		}
	}
	if !strings.Contains(second, "/types/0") {
		t.Errorf("the retry does not list the offending pointers:\n%s", second)
	}
	if strings.Contains(second, "CANARY") {
		t.Errorf("the retry note quotes reply text:\n%s", second)
	}
}

// A pass that died between two normalised passes is resumed from the stored,
// already normalised contract; a later pass still names the earlier ids in the
// model's own spelling.
func TestNormalisedIDsSurviveAResume(t *testing.T) {
	first := pascalOutline
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt": first, "contract.outline.2.txt": "the model fell over", "contract.outline.2.2.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want pass 2 to fail")
	}
	g.wire(variant(t, map[string]string{"contract.outline.2.txt": `{"components": [{"id": "Audit_Log", "package": "greet", "exports": []}],
 "types": [{"id": "AuditEntry", "package": "greet", "file": "internal/greet/audit.go", "decl": "// AuditEntry is one record.\ntype AuditEntry struct{}", "uses": ["NameError"]}]}`,
		"contract.farewell.txt": `{"types": [], "functions": [{"id": "fn-farewell", "package": "greet", "file": "internal/greet/farewell.go", "signature": "func Farewell() error", "doc": "Farewell.", "uses": ["name-error", "Send_Receipt"]}], "more": false}`,
		"contract.audit-log.txt": `{"types": [], "functions": [{"id": "fn-audit", "package": "greet", "file": "internal/greet/audit.go", "signature": "func Audit() error", "doc": "Audit records.", "uses": ["AuditEntry"]},
 {"id": "fn-send-receipt", "package": "greet", "file": "internal/greet/receipt.go", "signature": "func SendReceipt() error", "doc": "SendReceipt sends.", "uses": ["audit-entry"]}], "more": false}`}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell audit-log" {
		t.Errorf("components = %q", got)
	}
	found := false
	for _, ty := range g.contracts().Types {
		if ty.ID == "audit-entry" {
			found = true
			if len(ty.Uses) != 1 || ty.Uses[0] != "name-error" {
				t.Errorf("uses = %v, want the earlier id in its normalised form", ty.Uses)
			}
		}
	}
	if !found {
		t.Error("audit-entry missing")
	}
	for _, f := range g.contracts().Functions {
		if f.ID == "fn-farewell" && (len(f.Uses) != 2 || f.Uses[1] != "fn-send-receipt") {
			t.Errorf("fn-farewell uses = %v, want the forward reference resolved to the function", f.Uses)
		}
	}
	if n := repairStages(g); n != 0 {
		t.Errorf("repair calls = %d, want 0", n)
	}
	if strings.Contains(string(g.read("contracts.json")), "|") {
		t.Error("a pending reference reached contracts.json")
	}
}

// A function that names a function a later component declares needs no repair
// pass: the reference is kept in both forms and resolved once the contract is whole.
func TestForwardReferenceToAFunctionNeedsNoRepair(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "Farewell_Helper"),
		"contract.farewell.txt": `{"types": [], "functions": [
 {"id": "fn-farewell", "package": "greet", "file": "internal/greet/farewell.go", "signature": "func Farewell(name string) (string, error)", "doc": "Farewell says goodbye.", "uses": ["name-error"]},
 {"id": "fn-farewell-helper", "package": "greet", "file": "internal/greet/helper.go", "signature": "func Helper() error", "doc": "Helper helps.", "uses": ["name-error"]}], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := repairStages(g); n != 0 {
		t.Errorf("repair calls = %d, want 0", n)
	}
	for _, f := range g.contracts().Functions {
		if f.ID == "fn-greet" && (len(f.Uses) != 2 || f.Uses[1] != "fn-farewell-helper") {
			t.Errorf("fn-greet uses = %v", f.Uses)
		}
	}
	if strings.Contains(string(g.read("contracts.json")), "|") {
		t.Error("a pending reference reached contracts.json")
	}
}

// A reference nobody declares is asked for once, as one canonical id with a hint.
func TestNeverDeclaredForwardReferenceIsRepairedAsOneID(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "Ghost_Fn"),
		"contract._repair.txt":  `{"types": [], "functions": [{"id": "fn-ghost-fn", "component": "greeting", "package": "greet", "file": "internal/greet/ghost.go", "signature": "func Ghost() error", "doc": "Ghost.", "uses": []}]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := repairStages(g); n != 1 {
		t.Fatalf("repair calls = %d, want 1", n)
	}
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:_repair" {
			p := r.Messages[1].Content
			if !strings.Contains(p, "<unresolved>\nghost-fn [") || strings.Count(p, "ghost-fn [") != 1 {
				t.Errorf("repair prompt does not list one canonical id with a hint:\n%s", p)
			}
		}
	}
}
