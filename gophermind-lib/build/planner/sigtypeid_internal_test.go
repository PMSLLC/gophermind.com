package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/events"
)

func sigDoc() map[string]any {
	return map[string]any{
		"module": "example.com/m",
		"types": []any{
			map[string]any{"id": "column-summary", "package": "example.com/m/internal/stats", "file": "internal/stats/s.go", "decl": "type ColumnSummary struct{ N int }"},
		},
	}
}

func sigReply(sig string) string {
	return `{"functions": [{"id": "fn-render-text", "package": "example.com/m/internal/out", "file": "internal/out/r.go", "signature": ` + jsonStr(sig) + `, "doc": "d", "uses": []}]}`
}

func jsonStr(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func TestSigTypeIDReplacedByGoName(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "func f(s column-summary) error", "func f(s ColumnSummary) error"},
		{"slice", "func renderText(w io.Writer, summaries []column-summary) error", "func renderText(w io.Writer, summaries []ColumnSummary) error"},
		{"pointer", "func f(s *column-summary) error", "func f(s *ColumnSummary) error"},
		{"map", "func f(m map[string]column-summary) error", "func f(m map[string]ColumnSummary) error"},
		{"array chan result", "func f(a [3]column-summary, c chan<- column-summary) (column-summary, error)", "func f(a [3]ColumnSummary, c chan<- ColumnSummary) (ColumnSummary, error)"},
		{"receiver", "func (c column-summary) F() error", "func (c ColumnSummary) F() error"},
		{"unknown left", "func f(s []no-such-type) error", "func f(s []no-such-type) error"},
	}
	for _, c := range cases {
		m, notes := normalize(t, sigDoc(), replyComponent, sigReply(c.in))
		got := objects(m["functions"])[0]["signature"]
		if got != c.want {
			t.Errorf("%s: %q -> %q, want %q", c.name, c.in, got, c.want)
		}
		if c.in == c.want && notes.SigTypeIDs != 0 {
			t.Errorf("%s: counted %d for a signature left alone", c.name, notes.SigTypeIDs)
		}
	}
}

func TestSigTypeIDOfTypeInTheSameReply(t *testing.T) {
	reply := `{"types": [{"id": "row-set", "package": "p", "file": "f.go", "decl": "type RowSet []int"}],
 "functions": [{"id": "fn-a", "package": "p", "file": "f.go", "signature": "func A(r row-set)", "doc": "d", "uses": []}]}`
	m, _ := normalize(t, nil, replyComponent, reply)
	if got := objects(m["functions"])[0]["signature"]; got != "func A(r RowSet)" {
		t.Errorf("signature = %v", got)
	}
}

func TestSigTypeIDThenQualifiedWhenForeign(t *testing.T) {
	doc := sigDoc()
	doc["functions"] = []any{}
	m, _ := normalize(t, doc, replyComponent, sigReply("func renderText(summaries []column-summary) error"))
	doc["functions"] = m["functions"]
	if n, _, _ := qualifyForeignTypes(doc); n != 1 {
		t.Fatalf("qualified %d, want 1 (the Go name is bare and foreign)", n)
	}
	if got := objects(doc["functions"])[0]["signature"]; got != "func renderText(summaries []stats.ColumnSummary) error" {
		t.Errorf("signature = %v", got)
	}
}

func TestSigTypeIDWarningHasNoSignatureText(t *testing.T) {
	_, notes := normalize(t, sigDoc(), replyComponent, sigReply("func CANARYFN(s []column-summary) error"))
	if notes.SigTypeIDs != 1 {
		t.Fatalf("count = %d", notes.SigTypeIDs)
	}
	col := events.NewCollector()
	p := New(Deps{Sink: col})
	var st contractState
	p.noteNormalized(&st, "contract:x", notes)
	w := col.OfKind(events.KindWarning)
	if len(w) != 1 || !strings.Contains(w[0].Message, "signature_type_id_normalized: 1 ") || !strings.Contains(w[0].Message, `"fn-render-text"`) {
		t.Fatalf("warnings = %v", w)
	}
	for _, bad := range []string{"CANARYFN", "column-summary", "ColumnSummary", "func "} {
		if strings.Contains(w[0].Message, bad) {
			t.Errorf("warning quotes %q: %s", bad, w[0].Message)
		}
	}
	if st.SigTypeIDsNormalized != 1 {
		t.Errorf("persisted count = %d", st.SigTypeIDsNormalized)
	}
}

func TestUnparsableSignatureWithHyphenTokenGetsHint(t *testing.T) {
	_, err := parseSignature("func f(s []no-such-type) error")
	if err == nil || !strings.Contains(err.Error(), "hyphenated id where a Go type name is required") {
		t.Fatalf("err = %v, want the hint", err)
	}
	if strings.Contains(err.Error(), "no-such-type") {
		t.Errorf("hint quotes the signature: %v", err)
	}
	_, err = parseSignature("func f(s []) error {{")
	if err == nil || strings.Contains(err.Error(), "hyphenated") {
		t.Errorf("err = %v, want no hint without a hyphenated token", err)
	}
}
