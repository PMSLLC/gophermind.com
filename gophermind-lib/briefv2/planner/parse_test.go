package planner_test

import (
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func TestStripReply(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare object", `{"a": 1}`, `{"a": 1}`},
		{"bare array with spaces", "  [1, 2]\n", `[1, 2]`},
		{"json fence", "```json\n{\"a\": 1}\n```", `{"a": 1}`},
		{"plain fence with prose", "Here it is:\n```\n[1]\n```\nHope that helps.", `[1]`},
		{"second fence is the json", "```text\nnot json\n```\n```json\n{\"a\": 2}\n```", `{"a": 2}`},
		{"no fence parses, first wins", "```\nnot json\n```\n```\nalso not\n```", `not json`},
		{"prose before and after", "Sure. {\"a\": [1, 2]} Done.", `{"a": [1, 2]}`},
		{"brackets inside strings", `Result: {"a": "x } y ] z"} ok`, `{"a": "x } y ] z"}`},
		{"array with prose", "The list: [\"a\", \"b\"] as asked", `["a", "b"]`},
		{"valid json holding a fence is left alone", "{\"test_file\": \"x := `a`\\n```\\n\"}", "{\"test_file\": \"x := `a`\\n```\\n\"}"},
		{"unclosed fence", "```json\n{\"a\": 1}", `{"a": 1}`},
		{"nothing to find", "no json here", "no json here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := planner.StripReply(c.in); got != c.want {
				t.Errorf("StripReply(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestQuestion(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"marker alone on a line", "QUESTION:\nShould names be trimmed?", "Should names be trimmed?", true},
		{"prose before the marker", "I need one thing.\n  QUESTION:  \nTrim or not?\nIt changes the tests.", "Trim or not?\nIt changes the tests.", true},
		{"marker with text on the same line is not the protocol", "QUESTION: trim?", "", false},
		{"marker with nothing after it", "QUESTION:\n\n", "", false},
		{"ordinary reply", `[{"id": "fn-a"}]`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := planner.Question(c.in)
			if got != c.want || ok != c.ok {
				t.Errorf("Question(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}
