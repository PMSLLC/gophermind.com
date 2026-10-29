package planner

import (
	"encoding/json"
	"testing"
)

// The schema wants an integration test's given, expect and command as text. A
// model wrote given as a list of the arguments (["a", "b"]) and an expected
// exit code as a number, and the outline was refused three times.
func TestOutlineIntegrationTestFieldsAreCoercedToText(t *testing.T) {
	reply := `{"components": [{"id": "report-errors", "package": "x", "integration_tests": [
		{"name": "two files", "given": ["a", "b"], "expect": 2, "command": "slugify a b"},
		{"name": 7, "given": true, "expect": "ok"}]}]}`
	out, _, err := normalizeReply(nil, reply, replyOutline)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Components []struct {
			Tests []map[string]any `json:"integration_tests"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	tests := r.Components[0].Tests
	want := []map[string]string{
		{"name": "two files", "given": "a, b", "expect": "2", "command": "slugify a b"},
		{"name": "7", "given": "true", "expect": "ok"},
	}
	for i, w := range want {
		for k, v := range w {
			if got, _ := tests[i][k].(string); got != v {
				t.Errorf("test %d %s = %v, want %q", i, k, tests[i][k], v)
			}
		}
	}
}

func TestOutlineIntegrationTestsLeaveGoodRepliesAlone(t *testing.T) {
	reply := `{"components": [{"id": "x", "package": "x", "integration_tests": [{"name": "n", "given": "g", "expect": "e"}]}]}`
	out, _, err := normalizeReply(nil, reply, replyOutline)
	if err != nil {
		t.Fatal(err)
	}
	if out != reply {
		t.Errorf("a reply that needed no change was rewritten:\n got %s\nwant %s", out, reply)
	}
}
