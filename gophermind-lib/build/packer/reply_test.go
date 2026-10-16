package packer

import (
	"crypto/sha256"
	"encoding/hex"
	"go/format"
	"strings"
	"testing"
)

const goodFile = "package greet\n\nimport \"fmt\"\n\nfunc Greet(name string) (string, error) {\n\treturn fmt.Sprint(name), nil\n}\n"

func fixture(t *testing.T) Expect {
	t.Helper()
	e, err := NewExpect("greet", "func Greet(name string) (string, error)")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestParseReplyContract(t *testing.T) {
	e := fixture(t)
	recv, err := NewExpect("greet", "func (g *G) Hello() string")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		text      string
		e         Expect
		source    bool
		problem   string
		malformed string
	}{
		{"plain", goodFile, e, true, "", ""},
		{"fenced", "```go\n" + goodFile + "```\n", e, true, "", ""},
		{"contract problem", "CONTRACT_PROBLEM: x is impossible", e, false, "x is impossible", ""},
		{"problem with prose", "Sorry.\nCONTRACT_PROBLEM: x is impossible\n", e, false, "", "contract problem mixed"},
		{"problem too long", "CONTRACT_PROBLEM: " + strings.Repeat("a", 301), e, false, "", "contract problem too long"},
		{"problem empty", "CONTRACT_PROBLEM:   ", e, false, "", "syntax error"},
		{"prose before", "Here is the file:\n" + goodFile, e, false, "", "syntax error at line 1 col"},
		{"prose before fence", "Here:\n```go\n" + goodFile + "```\n", e, false, "", "text outside"},
		{"odd fence", "```go\n" + goodFile, e, false, "", "unbalanced fence"},
		{"empty", "", e, false, "", "empty reply"},
		{"whitespace", " \n\t\n", e, false, "", "empty reply"},
		{"wrong package", strings.Replace(goodFile, "package greet", "package other", 1), e, false, "", "package mismatch"},
		{"test package", strings.Replace(goodFile, "package greet", "package greet_test", 1), e, false, "", "test package"},
		{"cgo", "package greet\n\nimport \"C\"\n\nfunc Greet(name string) (string, error) { return \"\", nil }\n", e, false, "", "cgo import"},
		{"missing func", "package greet\n\nfunc Other() {}\n", e, false, "", "not found"},
		{"two funcs", goodFile + "\nfunc Greet(name string) (string, error) { return \"\", nil }\n", e, false, "", "declared 2 times"},
		{"syntax", "package greet\n\nfunc Greet( {\n", e, false, "", "syntax error at line 3 col"},
		{"receiver differs", "package greet\n\nfunc (g G) Hello() string { return \"\" }\n", recv, false, "", "signature mismatch"},
		{"receiver ok", "package greet\n\nfunc (g *G) Hello() string { return \"\" }\n", recv, true, "", ""},
	}
	for _, c := range cases {
		r, err := ParseReply(c.text, c.e)
		if c.malformed != "" {
			if err == nil || !strings.Contains(err.Error(), c.malformed) {
				t.Errorf("%s: want malformed %q, got %v", c.name, c.malformed, err)
			}
			if r.Source != nil {
				t.Errorf("%s: source set on malformed", c.name)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if (r.Source != nil) != c.source {
			t.Errorf("%s: source presence %v", c.name, r.Source != nil)
		}
		if r.ContractProblem != c.problem {
			t.Errorf("%s: problem %q", c.name, r.ContractProblem)
		}
		if r.Forbidden != "" {
			t.Errorf("%s: forbidden %q", c.name, r.Forbidden)
		}
		if strings.TrimSpace(c.text) != "" {
			sum := sha256.Sum256([]byte(c.text))
			if r.SHA256 != hex.EncodeToString(sum[:]) || r.Bytes != len(c.text) {
				t.Errorf("%s: sha/bytes wrong", c.name)
			}
		}
	}
	r1, _ := ParseReply(goodFile, e)
	r2, _ := ParseReply(goodFile, e)
	if r1.SHA256 == "" || r1.SHA256 != r2.SHA256 {
		t.Error("sha not stable")
	}
	if len(r1.Imports) != 1 || r1.Imports[0] != "fmt" {
		t.Errorf("imports %v", r1.Imports)
	}
}

func TestParseReplyForbidden(t *testing.T) {
	e := fixture(t)
	cases := map[string]string{
		"file_header":     "// FILE: other.go\n" + goodFile,
		"diff_header":     "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n",
		"multiple_fences": "```go\n" + goodFile + "```\n```go\npackage x\n```\n",
	}
	for kind, text := range cases {
		r, err := ParseReply(text, e)
		if err != nil || r.Forbidden != kind || r.Source != nil {
			t.Errorf("%s: got forbidden=%q err=%v source=%v", kind, r.Forbidden, err, r.Source != nil)
		}
		if r.SHA256 == "" {
			t.Errorf("%s: no sha", kind)
		}
	}
}

func TestParseReplyFormats(t *testing.T) {
	e := fixture(t)
	messy := "package greet\nimport \"fmt\"\nfunc Greet(name string) (string, error) {\nreturn fmt.Sprint(name),nil\n}"
	r, err := ParseReply(messy, e)
	if err != nil {
		t.Fatal(err)
	}
	out, err := format.Source(r.Source)
	if err != nil || string(out) != string(r.Source) {
		t.Error("not gofmt clean")
	}
	r, err = ParseReply(goodFile, e)
	if err != nil || string(r.Source) != goodFile {
		t.Errorf("clean file changed: %v", err)
	}
}

func TestReplySignatureMismatchMalformed(t *testing.T) {
	e := fixture(t)
	pe, _ := NewExpect("greet", "func (g *G) Hello() string")
	file := func(sig string) string {
		return "package greet\n\n" + sig + " { panic(0) }\n"
	}
	bad := []struct {
		name string
		e    Expect
		sig  string
	}{
		{"param name", e, "func Greet(who string) (string, error)"},
		{"return type", e, "func Greet(name string) (int, error)"},
		{"added param", e, "func Greet(name string, n int) (string, error)"},
		{"receiver value", pe, "func (g G) Hello() string"},
	}
	for _, c := range bad {
		_, err := ParseReply(file(c.sig), c.e)
		if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	good := []string{
		"func  Greet( name  string )  ( string ,error )",
		"func Greet(name string) (string, error) // trailing",
		"func Greet(\n\tname string,\n) (\n\tstring,\n\terror,\n)",
		"// Greet says hi.\nfunc Greet(name\tstring) (string, error)",
	}
	for _, g := range good {
		if _, err := ParseReply(file(g), e); err != nil {
			t.Errorf("accepted variant %q: %v", g, err)
		}
	}
}

func TestParseErrorNoCanary(t *testing.T) {
	e := fixture(t)
	texts := []string{
		"package greet\n\nvar CANARY-reply = 1\n",
		"package CANARY_reply\n\nfunc Greet(name string) (string, error) { return \"CANARY-reply\", nil }\n",
		"package greet\n\nfunc CANARY_reply() {}\n",
		"package greet\n\nfunc Greet(CANARY_reply string) (string, error) { return \"\", nil }\n",
		"package greet\n\nimport \"CANARY-reply/x\"\nimport \"C\"\nfunc Greet(name string) (string, error) { return \"\", nil }\n",
		"CANARY-reply\n" + goodFile,
		"```go\nCANARY-reply\n" + goodFile,
		"Text CANARY-reply\n```go\n" + goodFile + "```\n",
		"package greet_test // CANARY-reply\n",
		"package greet\n\nimport \"CANARY-reply\\x\"\n",
		"package greet\n\nfunc Greet(name string) (string, error) { return \"\", nil }\nfunc Greet(name string) (string, error) { return \"CANARY-reply\", nil }\n",
		"CONTRACT_PROBLEM: CANARY-reply\nmore",
		"CONTRACT_PROBLEM: " + strings.Repeat("CANARY-reply", 40),
	}
	sawLoc := false
	for _, text := range texts {
		_, err := ParseReply(text, e)
		if err == nil {
			t.Errorf("expected error for %q", text[:min(len(text), 20)])
			continue
		}
		if strings.Contains(err.Error(), "CANARY") {
			t.Errorf("canary leaked: %v", err)
		}
		if strings.Contains(err.Error(), "line ") && strings.Contains(err.Error(), "col ") {
			sawLoc = true
		}
	}
	if !sawLoc {
		t.Error("no error named line and column")
	}
}

func TestNewExpectRejectsBadContractSignature(t *testing.T) {
	_, err := NewExpect("p", "func (")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "malformed") {
		t.Errorf("must not read as a reply defect: %v", err)
	}
}
