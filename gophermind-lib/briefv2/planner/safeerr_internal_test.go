package planner

import (
	"strings"
	"testing"
)

const replyCanary = "CANARY-4c1d9e"

// No error a parse or merge path returns may quote the reply.
func TestParseAndMergeErrorsNeverQuoteTheReply(t *testing.T) {
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	fn := func(id, sig, file, uses string) string {
		return `{"id": "` + id + `", "package": "x", "file": "` + file + `", "signature": "` + sig + `", "doc": "d", "uses": [` + uses + `]}`
	}
	c := replyCanary
	outlines := []string{
		"prose " + c,
		`{"module": "` + c + `", "components": "` + c + `"}`,
		strings.Replace(okOutline, `"module": "example.com/x"`, `"module": "example.com/x", "`+c+`": 1`, 1),
		strings.Replace(okOutline, `internal/x/errors.go`, c+`/../x.go`, 1),
		strings.Replace(okOutline, `internal/x/errors.go`, c+`.txt`, 1),
		strings.Replace(okOutline, `"errors": "return error"`, `"`+c+`": "x"`, 1),
		strings.Replace(okOutline, `"id": "greeting"`, `"id": "`+c+`"`, 1),
		strings.Replace(okOutline, `"uses"`, `"uses"`, 1),
		strings.Replace(okOutline, `"decl": "type NameError struct{}"`, `"decl": "type NameError struct{}", "uses": ["`+c+`"]`, 1),
	}
	for i, o := range outlines {
		if _, _, err := parseOutline(o, testRunID); err != nil && strings.Contains(err.Error(), c) {
			t.Errorf("outline %d: error quotes the reply: %v", i, err)
		}
	}
	replies := []string{
		"prose " + c,
		`{"functions": [` + fn("fn-a", "func "+c+"(", "internal/x/a.go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", c+" A()", "internal/x/a.go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "func A() {}\\nfunc "+c+"()", "internal/x/a.go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "type "+c+" int", "internal/x/a.go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "func A()", "../"+c+".go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "func A()", "/"+c+".go", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "func A()", c+".txt", ``) + `]}`,
		`{"functions": [` + fn("fn-a", "func A()", "internal/x/a.go", `"`+c+`"`) + `]}`,
		`{"functions": [` + fn(c, "func A()", "internal/x/a.go", ``) + `]}`,
		`{"functions": [{"id": "fn-a", "` + c + `": "` + c + `"}]}`,
		`{"functions": "` + c + `"}`,
		`{"types": [{"id": "name-error", "package": "x", "file": "internal/x/e.go", "decl": "d"}], "functions": []}`,
		`{"more": true, "functions": []} ` + c,
	}
	for i, r := range replies {
		if _, _, err := mergePass(base, "greeting", r, testRunID); err != nil && strings.Contains(err.Error(), c) {
			t.Errorf("reply %d: error quotes the reply: %v", i, err)
		}
	}
	if _, err := parseSignature("func " + c + "("); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseSignature error = %v", err)
	}
	if _, err := parseClarify("[" + c + "]"); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseClarify error = %v", err)
	}
	if _, err := parseClarify(`{"` + c + `": 1}`); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseClarify type error = %v", err)
	}
}

func TestParseClarifyRejectsNull(t *testing.T) {
	if _, err := parseClarify("null"); err == nil {
		t.Error("null must be rejected")
	}
	if qs, err := parseClarify("[]"); err != nil || qs == nil {
		t.Errorf("[] = %v, %v", qs, err)
	}
}
