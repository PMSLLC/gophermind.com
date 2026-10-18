package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

// The slip seen in a real test-writer reply: a string opened on a line is
// never closed.
const unclosedStringReply = `{
  "tests": [
    {
      "name": "happy_path_integer",
      "given": "value: \"123\"",
      "expect": "CANARY-4c1d9e state updated",
      "polarity": "success"
    },
    {
      "name": "boundary",
      "given": "value: \"<10000 chars>\",
      "expect": "CANARY-4c1d9e processed",
      "polarity": "boundary"
    }
  ]
}`

func TestJSONErrInNamesLineColumnAndKindWithoutQuotingTheReply(t *testing.T) {
	var v any
	err := json.Unmarshal([]byte(unclosedStringReply), &v)
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	got := jsonErrIn(err, unclosedStringReply)
	for _, want := range []string{"line 11", "column", "string", "not closed", `\"`} {
		if !strings.Contains(got, want) {
			t.Errorf("hint %q lacks %q", got, want)
		}
	}
	for _, bad := range []string{"CANARY", "10000", "happy_path", "boundary"} {
		if strings.Contains(got, bad) {
			t.Errorf("hint %q quotes the reply (%q)", got, bad)
		}
	}
}

func TestJSONErrInOtherKinds(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"missing comma", "{\n \"a\": 1\n \"b\": 2\n}", "line 3"},
		{"truncated", "{\n \"a\": [1, 2", "ends before"},
		{"bad escape", "{\n \"a\": \"x\\qy\"\n}", "two backslashes"},
	}
	for _, c := range cases {
		dec := json.NewDecoder(strings.NewReader(c.text))
		var v any
		err := dec.Decode(&v)
		if err == nil {
			t.Fatalf("%s: expected an error", c.name)
		}
		if got := jsonErrIn(err, c.text); !strings.Contains(got, c.want) {
			t.Errorf("%s: hint %q lacks %q", c.name, got, c.want)
		}
	}
}

func TestParseTestwriteSyntaxErrorHintHasLocation(t *testing.T) {
	_, _, err := parseTestwrite(unclosedStringReply, map[string]any{}, "p", "TestX", "m", nil)
	if err == nil || !strings.Contains(err.Error(), "line 11") || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("error %v", err)
	}
}
