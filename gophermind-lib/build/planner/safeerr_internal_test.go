package planner

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gophermind/gophermind-lib/build/schema"
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
		if _, _, _, err := mergePass(base, "greeting", r, testRunID); err != nil && strings.Contains(err.Error(), c) {
			t.Errorf("reply %d: error quotes the reply: %v", i, err)
		}
	}
	if _, err := parseSignature("func " + c + "("); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseSignature error = %v", err)
	}
	if _, err := parseQuestionList("["+c+"]", qstore{}, qparseOpts{}); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseQuestionList error = %v", err)
	}
	if _, err := parseQuestionList(`{"`+c+`": 1}`, qstore{}, qparseOpts{}); err == nil || strings.Contains(err.Error(), c) {
		t.Errorf("parseQuestionList type error = %v", err)
	}
}

func TestParseQuestionListRejectsNull(t *testing.T) {
	if _, err := parseQuestionList("null", qstore{}, qparseOpts{}); err == nil {
		t.Error("null must be rejected")
	}
	if qs, err := parseQuestionList("[]", qstore{}, qparseOpts{}); err != nil || qs == nil {
		t.Errorf("[] = %v, %v", qs, err)
	}
}

func contractVE(t *testing.T, doc string) *jsonschema.ValidationError {
	t.Helper()
	var ve *jsonschema.ValidationError
	if err := schema.Validate(schema.KindContract, []byte(doc)); !errors.As(err, &ve) {
		t.Fatalf("want a schema failure, got %v", err)
	}
	return ve
}

const extraKeyComponent = `{"components":[{"id":"summarize-file","package":"p","exports":[],"zzcanaryzz":"x"}]}`

// An unknown property is reported with the properties the schema allows, and
// never with the name or value the reply supplied.
func TestSchemaErrListsAllowedPropertiesForAnUnknownOne(t *testing.T) {
	ve := contractVE(t, extraKeyComponent)
	var doc map[string]any
	if err := json.Unmarshal([]byte(extraKeyComponent), &doc); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{"schemaErr": schemaErr(ve), "nodeSchemaErr": nodeSchemaErr(ve, doc)} {
		if !strings.Contains(got, "allowed: id, package, exports, integration_tests") {
			t.Errorf("%s = %q, want the allowed list", name, got)
		}
		if strings.Contains(got, "zzcanaryzz") {
			t.Errorf("%s = %q, names the reply's property", name, got)
		}
	}
	if got := nodeSchemaErr(ve, doc); !strings.Contains(got, `component "summarize-file" has a property the schema does not allow`) {
		t.Errorf("nodeSchemaErr = %q", got)
	}
}

func TestSchemaErrAllowedListForANodeIsCapped(t *testing.T) {
	var ve *jsonschema.ValidationError
	err := schema.Validate(schema.KindNode, []byte(`{"zzcanaryzz": 1}`))
	if !errors.As(err, &ve) {
		t.Fatalf("want a schema failure, got %v", err)
	}
	got := schemaErr(ve)
	if !strings.Contains(got, "does not allow (allowed: ") || !strings.Contains(got, "more)") {
		t.Errorf("schemaErr = %q, want a capped allowed list", got)
	}
	if strings.Contains(got, "zzcanaryzz") {
		t.Errorf("schemaErr = %q, names the reply's property", got)
	}
}

func TestSchemaErrOtherKindsKeepTheirShape(t *testing.T) {
	got := schemaErr(contractVE(t, `{"components":[{"id":"a","package":"p"}]}`))
	if want := "/components/0 is missing exports"; !strings.Contains(got, want) {
		t.Errorf("schemaErr = %q, want %q", got, want)
	}
	got = schemaErr(contractVE(t, `{"components":[{"id":"A b","package":"p","exports":[]}]}`))
	if !strings.Contains(got, "/components/0/id is not valid") || strings.Contains(got, "allowed") {
		t.Errorf("schemaErr = %q", got)
	}
}
