package planner

import (
	"fmt"
	"strings"
	"testing"
)

func fwdBase(t *testing.T) map[string]any {
	t.Helper()
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func fnReply(id string, uses ...string) string {
	q := make([]string, len(uses))
	for i, u := range uses {
		q[i] = fmt.Sprintf("%q", u)
	}
	return fmt.Sprintf(`{"types": [], "functions": [{"id": %q, "package": "x", "file": "internal/x/%s.go", "signature": "func F()", "doc": "d", "uses": [%s]}]}`,
		id, id, strings.Join(q, ","))
}

// pass normalises a reply against doc and merges it the way the stage does.
func pass(t *testing.T, doc map[string]any, reply string) map[string]any {
	t.Helper()
	text, _, err := normalizeReply(doc, reply, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	out, _, _, err := mergePass(doc, "greeting", text, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func usesOf(doc map[string]any, key, id string) string {
	for _, o := range objects(doc[key]) {
		if o["id"] == id {
			return strings.Join(strList(o["uses"]), " ")
		}
	}
	return "<missing>"
}

// A forward reference to a function keeps both forms until the contract is
// whole, then resolves to the one that was declared.
func TestForwardReferenceToALaterFunctionResolvesToTheFunction(t *testing.T) {
	doc := pass(t, fwdBase(t), fnReply("fn-a", "ValidateEmail", "validate_email"))
	if got := usesOf(doc, "functions", "fn-a"); got != "validate-email|fn-validate-email validate-email|fn-validate-email" {
		t.Fatalf("pending uses = %q", got)
	}
	if un := unresolvedUses(doc); len(un) != 0 {
		t.Errorf("outline-phase unresolved = %v, want none (a later pass may declare the function)", un)
	}
	if un := unresolvedFinal(doc); len(un) != 1 || un[0] != "validate-email" {
		t.Errorf("final unresolved = %v, want the one canonical id", un)
	}
	doc = pass(t, doc, fnReply("fn-validate-email", "name-error"))
	if un := unresolvedFinal(doc); len(un) != 0 {
		t.Fatalf("unresolved after the declaration = %v", un)
	}
	got := resolveRefs(doc)
	if u := usesOf(got, "functions", "fn-a"); u != "fn-validate-email fn-validate-email" {
		t.Errorf("resolved uses = %q", u)
	}
	if _, err := validateContractDoc(got, testRunID); err != nil {
		t.Errorf("resolved contract does not validate: %v", err)
	}
	if strings.Contains(mustJSON(got), "|") {
		t.Error("a pending reference survived resolution")
	}
	if !strings.Contains(mustJSON(doc), "|") {
		t.Error("resolveRefs changed its input")
	}
}

func TestForwardReferenceFromATypeResolvesToALaterFunction(t *testing.T) {
	text, _, err := normalizeReply(fwdBase(t), `{"types": [{"id": "audit", "package": "x", "file": "internal/x/t.go", "decl": "type A int", "uses": ["ValidateEmail"]}], "functions": []}`, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, _, err := mergePass(fwdBase(t), "greeting", text, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	doc = pass(t, doc, fnReply("fn-validate-email", "name-error"))
	if u := usesOf(resolveRefs(doc), "types", "audit"); u != "fn-validate-email" {
		t.Errorf("type uses resolved to %q", u)
	}
}

// A type and a function with the same base name: each reference resolves to the
// kind of the thing that refers to it.
func TestBothFormsDeclaredPrefersTheContext(t *testing.T) {
	doc := fwdBase(t)
	ty := `{"types": [{"id": "audit", "package": "x", "file": "internal/x/t.go", "decl": "type A int", "uses": ["ValidateEmail"]}], "functions": [
 {"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func F()", "doc": "d", "uses": ["ValidateEmail"]}]}`
	text, _, err := normalizeReply(doc, ty, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	if doc, _, _, err = mergePass(doc, "greeting", text, testRunID); err != nil {
		t.Fatal(err)
	}
	doc = pass(t, doc, `{"types": [{"id": "validate-email", "package": "x", "file": "internal/x/v.go", "decl": "type V int"}], "functions": [
 {"id": "fn-validate-email", "package": "x", "file": "internal/x/v2.go", "signature": "func V()", "doc": "d", "uses": []}]}`)
	got := resolveRefs(doc)
	if a, b := usesOf(got, "types", "audit"), usesOf(got, "functions", "fn-a"); a != "validate-email" || b != "fn-validate-email" {
		t.Errorf("type uses %q, function uses %q, want the type form and the function form", a, b)
	}
}

// A reference that was declared when it was written is exact, never rewritten
// into the other form later.
func TestADeclaredReferenceIsNotAmbiguous(t *testing.T) {
	doc := pass(t, fwdBase(t), `{"types": [{"id": "validate-email", "package": "x", "file": "internal/x/v.go", "decl": "type V int"}], "functions": [
 {"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func F()", "doc": "d", "uses": ["validate-email"]}]}`)
	doc = pass(t, doc, fnReply("fn-validate-email"))
	if u := usesOf(resolveRefs(doc), "functions", "fn-a"); u != "validate-email" {
		t.Errorf("uses = %q, want the declared type kept", u)
	}
}

func TestNeverDeclaredReferenceIsListedOnceWithAHint(t *testing.T) {
	doc := pass(t, fwdBase(t), fnReply("fn-a", "ValidateEmail", "validate_email"))
	un := unresolvedFinal(doc)
	text := unresolvedOwnersText(doc, un)
	if strings.Count(text, "\n") != 0 || !strings.HasPrefix(text, "validate-email [") || !strings.Contains(text, "fn-validate-email") ||
		!strings.Contains(text, "used by fn-a in component greeting") {
		t.Errorf("owners text = %q, want one line, one canonical id and the function hint", text)
	}
	// Repaired by declaring either form.
	for name, reply := range map[string]string{"function": fnReply("fn-validate-email"),
		"type": `{"types": [{"id": "validate-email", "package": "x", "file": "internal/x/v.go", "decl": "type V int"}], "functions": []}`} {
		fixed := pass(t, doc, reply)
		if un := unresolvedFinal(fixed); len(un) != 0 {
			t.Errorf("%s: still unresolved %v", name, un)
		}
	}
}

func TestLegacyCutStateReportsTruncated(t *testing.T) {
	dir := t.TempDir()
	var ids []string
	for i := 0; i < maxIgnoredRecorded; i++ {
		ids = append(ids, fmt.Sprintf("type %q", fmt.Sprint("x", i)))
	}
	if err := writeState(t, dir, contractState{IgnoredDuplicates: ids}); err != nil {
		t.Fatal(err)
	}
	if _, total, trunc, err := IgnoredDuplicates(dir); err != nil || total != maxIgnoredRecorded || !trunc {
		t.Errorf("total %d truncated %v err %v, want 200 and true", total, trunc, err)
	}
}
