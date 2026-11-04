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

// greeterReplies returns the legacy greeter reply, the same reply as JSON
// tests plus a fenced block, and the Go file both carry.
func greeterReplies(t *testing.T) (legacy, split, file string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/greeter/testwrite.fn-greet.txt")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(obj["test_file"], &file); err != nil {
		t.Fatal(err)
	}
	tests, _ := json.Marshal(map[string]json.RawMessage{"tests": obj["tests"]})
	return string(raw), string(tests) + "\n```go\n" + file + "```\n", file
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
