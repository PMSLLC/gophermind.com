package planner

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const splitSrc = "package cli\n\nfunc TestX(t *testing.T) {}\n"

func TestSplitSourceReplyTakesTheTrailingGoBlock(t *testing.T) {
	obj := `{"tests": []}`
	cases := map[string]string{
		"plain":      obj + "\n```go\n" + splitSrc + "```",
		"json fence": "```json\n" + obj + "\n```\n\n```go\n" + splitSrc + "```\n",
		"Go":         obj + "\n```Go\n" + splitSrc + "```",
		"golang":     obj + "\n```golang\n" + splitSrc + "```",
		"no info":    obj + "\n```\n" + splitSrc + "```",
		"crlf":       strings.ReplaceAll(obj+"\n```go\n"+splitSrc+"```\n", "\n", "\r\n"),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			head, src := splitSourceReply(reply)
			if src != splitSrc {
				t.Errorf("source = %q, want %q", src, splitSrc)
			}
			if strings.Contains(src, "\r") {
				t.Errorf("source keeps a carriage return: %q", src)
			}
			if name == "json fence" {
				if strings.Contains(head, "package") || !strings.Contains(head, obj) {
					t.Errorf("head = %q", head)
				}
				return
			}
			if strings.TrimSpace(head) != obj {
				t.Errorf("head = %q, want %q", head, obj)
			}
		})
	}
	if _, src := splitSourceReply(obj + "\n```python\n" + splitSrc + "```"); src != "" {
		t.Errorf("a python block was taken as the file: %q", src)
	}
}

func TestSplitSourceReplyPrefersTheLastPackageBlock(t *testing.T) {
	reply := "{\"tests\": []}\n```go\nfunc helper() {}\n```\n```go\npackage p\n\nvar _ = 1\n```\n"
	_, src := splitSourceReply(reply)
	if src != "package p\n\nvar _ = 1\n" {
		t.Errorf("source = %q", src)
	}
	reply = "{}\n```go\npackage a\n```\n```go\nfunc helper() {}\n```\n"
	if _, src := splitSourceReply(reply); src != "package a\n" {
		t.Errorf("source = %q, want the package block", src)
	}
}

func TestSplitSourceReplyWithoutABlock(t *testing.T) {
	legacy := `{"tests": [], "test_file": "package a\n"}`
	head, src := splitSourceReply(legacy)
	if head != legacy || src != "" {
		t.Errorf("head = %q, source = %q", head, src)
	}
}

// greeterReplies returns the greeter reply as the legacy single JSON object
// (tests plus a test_file field), the same reply as JSON tests plus a fenced
// block, and the Go file both carry. The fixture holds the fenced form.
func greeterReplies(t *testing.T) (legacy, split, file string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/greeter/testwrite.fn-greet.txt")
	if err != nil {
		t.Fatal(err)
	}
	head, file := splitSourceReply(string(raw))
	if file == "" {
		t.Fatal("the greeter fixture holds no fenced go block")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(head), &obj); err != nil {
		t.Fatal(err)
	}
	fileJSON, _ := json.Marshal(file)
	l, _ := json.Marshal(map[string]json.RawMessage{"tests": obj["tests"], "test_file": fileJSON})
	tests, _ := json.Marshal(map[string]json.RawMessage{"tests": obj["tests"]})
	return string(l), string(tests) + "\n```go\n" + file + "```\n", file
}

var greeterCT = map[string]any{"file": "internal/greet/greet.go", "errors": []any{map[string]any{"when": "empty", "returns": "*NameError"}}}

func TestParseTestwriteAcceptsBothForms(t *testing.T) {
	legacy, split, file := greeterReplies(t)
	parse := func(reply string) ([]map[string]any, string) {
		tests, src, err := parseTestwrite(reply, greeterCT, "greet", "TestGreet", "example.com/greeter", nil)
		if err != nil {
			t.Fatalf("refused: %v", err)
		}
		return tests, src
	}
	lt, ls := parse(legacy)
	st, ss := parse(split)
	if !reflect.DeepEqual(lt, st) || ls != ss {
		t.Errorf("the forms differ:\nlegacy %v\nsplit  %v", lt, st)
	}
	if len(st) != 3 || !strings.Contains(ss, "func TestGreet(") {
		t.Errorf("tests = %d, source = %q", len(st), ss)
	}

	// Both forms in one reply: the fenced file wins.
	both := strings.Replace(legacy, "func TestGreet(", "func TestGreetLegacy(", 1)
	both = strings.TrimSpace(both) + "\n```go\n" + file + "```\n"
	_, src := parse(both)
	if src != ss {
		t.Errorf("the legacy file won over the fenced one: %q", src)
	}
}

func TestParseTestwriteRefusesAMissingFile(t *testing.T) {
	legacy, _, _ := greeterReplies(t)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(legacy), &obj); err != nil {
		t.Fatal(err)
	}
	reply, _ := json.Marshal(map[string]json.RawMessage{"tests": obj["tests"]})
	_, _, err := parseTestwrite(string(reply), greeterCT, "greet", "TestGreet", "example.com/greeter", nil)
	if err == nil || !strings.Contains(err.Error(), "no test file") || !strings.Contains(err.Error(), "fenced go block") {
		t.Errorf("err = %v", err)
	}
}

func TestSplitSourceReplyKeepsFenceLinesInsideTheFile(t *testing.T) {
	file := func(inner string) string {
		return "package p\n\n/*\n" + inner + "\n*/\n\nfunc TestX(t *testing.T) {}\n"
	}
	cases := map[string]struct{ reply, want string }{
		"inner go fence": {"{\"tests\": []}\n```go\n" + file("```go\npackage inner\n```") + "```", file("```go\npackage inner\n```")},
		"inner bare":     {"{\"tests\": []}\n```go\n" + file("```\npackage inner\n```") + "```", file("```\npackage inner\n```")},
		"four backticks": {"{\"tests\": []}\n````go\n" + file("```\nx\n```") + "````", file("```\nx\n```")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			head, src := splitSourceReply(c.reply)
			if src != c.want {
				t.Errorf("source = %q, want %q", src, c.want)
			}
			if head != `{"tests": []}` {
				t.Errorf("head = %q", head)
			}
		})
	}
}

// The model writes a build line or a leading comment before the package
// clause; the block is still the Go file.
func TestSplitSourceReplyAcceptsALeadingBuildLineOrComment(t *testing.T) {
	for name, file := range map[string]string{
		"build line": "//go:build acceptance\n\npackage acceptance\n\nfunc TestA(t *testing.T) {}\n",
		"comment":    "// Tests for Greet.\npackage cli\n\nfunc TestX(t *testing.T) {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			head, src := splitSourceReply(`{"tests": []}` + "\n```go\n" + file + "```\n")
			if src != file || head != `{"tests": []}` {
				t.Errorf("head = %q, source = %q", head, src)
			}
		})
	}
}

func TestParseTestwriteAcceptsALeadingBuildLineOrComment(t *testing.T) {
	_, split, file := greeterReplies(t)
	for name, lead := range map[string]string{"build line": "//go:build acceptance\n\n", "comment": "// Tests for Greet.\n"} {
		t.Run(name, func(t *testing.T) {
			reply := strings.Replace(split, "```go\n"+file, "```go\n"+lead+file, 1)
			_, src, err := parseTestwrite(reply, greeterCT, "greet", "TestGreet", "example.com/greeter", nil)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !strings.Contains(src, "func TestGreet(") {
				t.Errorf("source = %q", src)
			}
		})
	}
}

func TestParseAcceptanceReplyTakesTheFencedFile(t *testing.T) {
	for name, lead := range map[string]string{"plain": "", "build line": "//go:build acceptance\n\n", "comment": "// Tests for A4.\n"} {
		t.Run(name, func(t *testing.T) {
			src, err := parseAcceptanceReply("```go\n"+lead+goodAcceptSrc+"```\n", a4)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !strings.HasPrefix(src, "//go:build "+AcceptanceTag+"\n") || !strings.Contains(src, "func TestA4(") {
				t.Errorf("source = %q", src)
			}
		})
	}
	// Cut off at the token limit: no closing fence, but the file is whole.
	if _, err := parseAcceptanceReply("```go\n"+goodAcceptSrc, a4); err != nil {
		t.Errorf("unclosed block refused: %v", err)
	}
}

func TestSplitSourceReplyTakesAnUnclosedTrailingGoBlock(t *testing.T) {
	head, src := splitSourceReply(`{"tests": []}` + "\n```go\n" + splitSrc)
	if src != splitSrc || head != `{"tests": []}` {
		t.Errorf("head = %q, source = %q", head, src)
	}
	// Cut mid-function: not Go, so not a file.
	if _, src := splitSourceReply(`{"tests": []}` + "\n```go\npackage cli\n\nfunc TestX(t *testing.T) {\n"); src != "" {
		t.Errorf("a cut-off file was taken: %q", src)
	}
}

func TestSplitSourceReplyKeepsLookingPastAnUnclosedOpener(t *testing.T) {
	reply := "```go\nfunc helper() {\n"
	head, src := splitSourceReply(reply)
	if src != "" || strings.Contains(head, "helper") {
		t.Errorf("head = %q, source = %q", head, src)
	}
}

func TestParseTestwriteRefusalsBlameTheRightPart(t *testing.T) {
	_, split, file := greeterReplies(t)
	cases := map[string]struct{ reply, want, not string }{
		"unclosed file is accepted": {strings.TrimSuffix(split, "```\n"), "", "not a JSON"},
		"fenced file alone":         {"```go\n" + file + "```\n", "reply has no tests: put the JSON object of tests before the fenced go block", "not a JSON"},
		"bad head, go block cut":    {"{\"tests\": [\n```go\npackage a\n\nfunc F() {\n", "not a JSON object", ""},
		"bad head, go block kept":   {"prose\n```go\nfunc helper() {}\n```\n", "not a JSON object", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseTestwrite(c.reply, greeterCT, "greet", "TestGreet", "example.com/greeter", nil)
			if c.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// The JSON error is about the head: the Go block is gone from the text.
	reply := "{\"tests\": [\n```go\npackage a\n\nfunc F() {\n"
	head, _ := splitSourceReply(reply)
	if strings.Contains(head, "package") {
		t.Errorf("head still holds the go block: %q", head)
	}
}

// A legacy JSON reply the model fenced as go by mistake is still accepted.
func TestParseTestwriteAcceptsLegacyJSONFencedAsGo(t *testing.T) {
	legacy, _, _ := greeterReplies(t)
	_, src, err := parseTestwrite("```go\n"+legacy+"\n```\n", greeterCT, "greet", "TestGreet", "example.com/greeter", nil)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(src, "func TestGreet(") {
		t.Errorf("source = %q", src)
	}
}

func TestParseAcceptanceReplyAcceptsLegacyJSONFencedAsGo(t *testing.T) {
	js, _ := json.Marshal(map[string]string{"test_file": goodAcceptSrc})
	src, err := parseAcceptanceReply("```go\n"+string(js)+"\n```\n", a4)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(src, "func TestA4(") {
		t.Errorf("source = %q", src)
	}
}
