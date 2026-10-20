package planner_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
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

func TestStripReplyRepairsStrayBackslashes(t *testing.T) {
	reply := "```json\n" + `{"root_tests":[{"requirement":"A1","name":"toolchain","command":"go version | grep -E 'go1\.(2[2-9]|[3-9][0-9])'"}],"functions":[]}` + "\n```"
	var got struct {
		RootTests []struct{ Command string } `json:"root_tests"`
	}
	if err := json.Unmarshal([]byte(planner.StripReply(reply)), &got); err != nil {
		t.Fatalf("repaired reply does not parse: %v", err)
	}
	if len(got.RootTests) != 1 || !strings.Contains(got.RootTests[0].Command, `go1\.(2[2-9]`) {
		t.Fatalf("command = %+v", got.RootTests)
	}

	same := []string{
		`{"a":"line\nquote\" back\\ \u00e9 \/ é"}`,
		`{"a": 1}`,
	}
	for _, in := range same {
		if out := planner.StripReply(in); out != in {
			t.Errorf("valid reply changed: %q -> %q", in, out)
		}
	}

	// A backslash outside a string is not touched, so a reply that is only
	// broken there stays broken.
	outside := `{"a":1} \. {"b":"x\.y"`
	if out := planner.StripReply(outside); strings.Contains(out, `\\.`) && strings.Contains(out, `1} `) {
		t.Errorf("backslash outside a string was touched: %q", out)
	}
	if out := planner.StripReply(`{"a":\.1}`); out != `{"a":\.1}` {
		t.Errorf("backslash outside a string was touched: %q", out)
	}

	// Still broken after repair: the text is returned as it was, so the
	// strict error stays truthful.
	broken := `{"a":"x\.y", oops}`
	if out := planner.StripReply(broken); out != broken {
		t.Errorf("unrepairable reply changed: %q", out)
	}
	if out := planner.StripReply(`{"a":"\u12G4 \."}`); out != `{"a":"\\u12G4 \\."}` {
		t.Errorf("bad unicode escape not repaired: %q", out)
	}
}

func TestCoverageReplyStrayBackslashHint(t *testing.T) {
	_, err := planner.ParseCoverageReply(`{"map":[],"root_tests":[{"command":"a\.b"}], oops}`, nil)
	if err == nil || !strings.Contains(err.Error(), "two backslashes") {
		t.Fatalf("err = %v, want the backslash hint", err)
	}
	r, err := planner.ParseCoverageReply(planner.StripReply(`{"map":[],"root_tests":[{"requirement":"A1","name":"n","command":"grep go1\.2"}],"functions":[]}`), testReqs)
	if err != nil || r.RootTests[0].Command != `grep go1\.2` {
		t.Fatalf("r = %+v, err = %v", r, err)
	}
}
