package planner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
)

func TestNormalizeID(t *testing.T) {
	cases := []struct{ name, raw, typ, fn string }{
		{"pascal", "IntakeSession", "intake-session", "fn-intake-session"},
		{"camel", "intakeSession", "intake-session", "fn-intake-session"},
		{"snake", "intake_session", "intake-session", "fn-intake-session"},
		{"spaces", "Intake Session", "intake-session", "fn-intake-session"},
		{"dots", "intake.session", "intake-session", "fn-intake-session"},
		{"upper", "INTAKE", "intake", "fn-intake"},
		{"acronym", "HTTPServer", "http-server", "fn-http-server"},
		{"unicode", "na\u00efve", "na-ve", "fn-na-ve"},
		{"leading digit", "2faCode", "2fa-code", "fn-2fa-code"},
		{"leading dash", "-intake", "intake", "fn-intake"},
		{"already valid", "intake-session", "intake-session", "fn-intake-session"},
		{"fn underscore", "fn_validate_email", "fn-validate-email", "fn-validate-email"},
		{"fn pascal", "FnValidateEmail", "fn-validate-email", "fn-validate-email"},
		{"fn kebab", "fn-validate-email", "fn-validate-email", "fn-validate-email"},
		{"empty", "", "", ""},
		{"only junk", "___", "", ""},
		{"only fn", "fn", "fn", ""},
	}
	for _, c := range cases {
		if got := normalizeID(c.raw, false); got != c.typ {
			t.Errorf("%s: type id %q -> %q, want %q", c.name, c.raw, got, c.typ)
		}
		if got := normalizeID(c.raw, true); got != c.fn {
			t.Errorf("%s: function id %q -> %q, want %q", c.name, c.raw, got, c.fn)
		}
	}
	// A valid id is never touched, even one the collapse rule would change.
	if got := normalizeID("a--b", false); got != "a--b" {
		t.Errorf("valid id changed: %q", got)
	}
}

func normalize(t *testing.T, doc map[string]any, kind replyKind, text string) (map[string]any, idNotes) {
	t.Helper()
	out, notes, err := normalizeReply(doc, text, kind)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("normalised reply is not JSON: %v\n%s", err, out)
	}
	return m, notes
}

func idsOf(m map[string]any, key string) string {
	var ids []string
	for _, o := range objects(m[key]) {
		ids = append(ids, fmt.Sprint(o["id"]))
	}
	return strings.Join(ids, " ")
}

func TestNormalizeOutlineRewritesIDsAndReferences(t *testing.T) {
	m, notes := normalize(t, nil, replyOutline, `{"module": "example.com/x",
 "components": [{"id": "Types", "package": "x"}, {"id": "intake_session", "package": "x"}],
 "types": [{"id": "IntakeSession", "package": "x", "file": "internal/x/a.go", "decl": "type IntakeSession int"},
           {"id": "Visit Record", "package": "x", "file": "internal/x/b.go", "decl": "type V int", "uses": ["IntakeSession", "Unknown_Thing"]}]}`)
	if got := idsOf(m, "components"); got != "types intake-session" {
		t.Errorf("components = %q", got)
	}
	if got := idsOf(m, "types"); got != "intake-session visit-record" {
		t.Errorf("types = %q", got)
	}
	uses := objects(m["types"])[1]["uses"].([]any)
	if fmt.Sprint(uses) != "[intake-session unknown-thing]" {
		t.Errorf("uses = %v", uses)
	}
	if notes.Count != 6 {
		t.Errorf("count = %d, want 6 (4 ids and 2 references)", notes.Count)
	}
	if m["module"] != "example.com/x" {
		t.Error("an unrelated field changed")
	}
}

func TestNormalizeComponentPassRewritesReferencesToEarlierPasses(t *testing.T) {
	doc, _, err := parseOutline(strings.NewReplacer(`"name-error"`, `"intake-session"`).Replace(okOutline), testRunID)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, _, err = mergePass(doc, "greeting", `{"functions": [{"id": "fn-validate-email", "package": "x", "file": "internal/x/a.go", "signature": "func V()", "doc": "d", "uses": []}]}`, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	m, notes := normalize(t, doc, replyComponent, `{"types": [], "functions": [
 {"id": "CheckVisit", "package": "x", "file": "internal/x/b.go", "signature": "func C()", "doc": "d", "uses": ["IntakeSession", "ValidateEmail", "validate_email", "fn-validate-email"]}]}`)
	f := objects(m["functions"])[0]
	if f["id"] != "fn-check-visit" {
		t.Errorf("function id = %v", f["id"])
	}
	if fmt.Sprint(f["uses"]) != "[intake-session fn-validate-email fn-validate-email fn-validate-email]" {
		t.Errorf("uses = %v", f["uses"])
	}
	if notes.Count != 4 {
		t.Errorf("count = %d, want 4 (the id and three references)", notes.Count)
	}
}

func TestNormalizeRepairRewritesTheFunctionComponent(t *testing.T) {
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := normalize(t, doc, replyRepair, `{"functions": [{"id": "GhostFn", "component": "Greeting", "package": "x", "file": "internal/x/a.go", "signature": "func G()", "doc": "d", "uses": []}]}`)
	f := objects(m["functions"])[0]
	if f["id"] != "fn-ghost-fn" || f["component"] != "greeting" {
		t.Errorf("function = %v", f)
	}
}

func TestNormalizeErrorsNameTheIndexNotTheText(t *testing.T) {
	_, _, err := normalizeReply(nil, `{"components": [{"id": "ok", "package": "x"}], "types": [{"id": "ok2", "package": "x"}, {"id": "CANARY ___ \u00e9", "package": "x"}]}`, replyOutline)
	if err != nil {
		t.Fatalf("an id with letters is fine: %v", err)
	}
	_, _, err = normalizeReply(nil, `{"components": [], "types": [{"id": "fine"}, {"id": "___"}]}`, replyOutline)
	if err == nil || !strings.Contains(err.Error(), "types[1]") || strings.Contains(err.Error(), "___") {
		t.Errorf("err = %v, want the index and no id text", err)
	}
	_, _, err = normalizeReply(nil, `{"functions": [{"id": "fn"}]}`, replyComponent)
	if err == nil || !strings.Contains(err.Error(), "functions[0]") {
		t.Errorf("err = %v", err)
	}
}

func TestNormalizeLeavesNonObjectsAlone(t *testing.T) {
	for _, text := range []string{"prose", `["x"]`, `{"types": "no"}`, `{"types": [1, "x"]}`} {
		out, notes, err := normalizeReply(nil, text, replyOutline)
		if err != nil || out != text || notes.Count != 0 {
			t.Errorf("%q -> %q %v %v, want it returned unchanged for the merge to reject", text, out, notes.Count, err)
		}
	}
}

func TestNormalizeCollisionKeepsTheFirst(t *testing.T) {
	m, notes := normalize(t, nil, replyOutline, `{"components": [{"id": "a", "package": "x"}],
 "types": [{"id": "IntakeSession", "package": "x", "file": "internal/x/a.go", "decl": "type A int"},
           {"id": "intake_session", "package": "x", "file": "internal/x/a.go", "decl": "type A int"},
           {"id": "IntakeSession", "package": "x", "file": "internal/x/a.go", "decl": "type A int"}]}`)
	if got := idsOf(m, "types"); got != "intake-session intake-session" {
		t.Errorf("types = %q, want the colliding raw id dropped and the exact repeat left for the merge", got)
	}
	if len(notes.Ignored) != 1 || notes.Ignored[0] != `type "intake-session"` {
		t.Errorf("ignored = %v", notes.Ignored)
	}
}

func TestNormalizeExamplesAreBounded(t *testing.T) {
	var types []string
	for i := 0; i < 15; i++ {
		types = append(types, fmt.Sprintf(`{"id": "Type_%c", "package": "x", "file": "f.go", "decl": "d"}`, 'A'+i))
	}
	types = append(types,
		fmt.Sprintf(`{"id": "Long%s", "package": "x", "file": "f.go", "decl": "d"}`, strings.Repeat("a", 70)),
		`{"id": "caf\u00e9 CANARY", "package": "x", "file": "f.go", "decl": "d"}`)
	_, notes := normalize(t, nil, replyOutline, `{"components": [{"id": "a", "package": "x"}], "types": [`+strings.Join(types, ",")+`]}`)
	if notes.Count != 17 || len(notes.Examples) > maxIDExamples {
		t.Errorf("count %d examples %d", notes.Count, len(notes.Examples))
	}
	col := events.NewCollector()
	p := New(Deps{Sink: col})
	var st contractState
	p.noteNormalized(&st, "contract", notes)
	// Force the long and the non-ASCII ones into the examples too.
	p.noteNormalized(&st, "contract", idNotes{Count: 2, Examples: []string{exampleOf("Long"+strings.Repeat("a", 70), "long-x"), exampleOf("caf\u00e9 CANARY", "caf-canary")}})
	w := col.OfKind(events.KindWarning)
	if len(w) != 2 || !strings.Contains(w[0].Message, "outline_id_normalized") || !strings.Contains(w[0].Message, "17") {
		t.Fatalf("warnings = %v", w)
	}
	if st.IDsNormalized != 19 {
		t.Errorf("persisted count = %d, want 19", st.IDsNormalized)
	}
	if strings.Contains(w[1].Message, "CANARY") || strings.Contains(w[1].Message, strings.Repeat("a", 70)) || !strings.Contains(w[1].Message, "<74 bytes>") {
		t.Errorf("unbounded old id in %q", w[1].Message)
	}
	if len(w[0].Message) > 600 {
		t.Errorf("warning is %d bytes", len(w[0].Message))
	}
}

// The prompts state the id syntax with examples for types, components and
// functions.
func TestContractPromptsStateTheIDSyntax(t *testing.T) {
	for _, name := range []string{"contract_outline", "contract_component", "contract_repair"} {
		out, err := render(name, map[string]string{"Brief": "b", "Answers": "a", "TypeSchema": "s", "Fixed": "f", "Emitted": "e", "Unresolved": "u", "UnresolvedCount": "1",
			"Component": "c", "Outline": "o", "Declared": "d", "Written": "w", "BriefSection": "bs", "ItemSchemas": "i"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"`intake-session`", "`IntakeSession`", "`intake_session`", "`fn-validate-email`"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s prompt lacks %s", name, want)
			}
		}
	}
}
