package planner

import (
	"strings"
	"testing"
)

func TestCoversRefusalsStateTheCountAndTheValidTags(t *testing.T) {
	const marker = "zzmarker-happy-path-case"
	for _, c := range []struct {
		name   string
		errors int
		covers string
		want   []string
	}{
		{"no errors listed", 0, "error:1", []string{"0 error conditions", "no test may use error:<n>", "input:<param name>", "happy"}},
		{"two errors listed", 2, "error:3", []string{"2 error conditions", "happy", "input:<name>", "error:1 to error:2"}},
		{"malformed tag, none listed", 0, "everything", []string{"0 error conditions", "no test may use error:<n>", "input:<param name>", "happy"}},
		{"malformed tag, two listed", 2, "everything", []string{"2 error conditions", "happy", "input:<name>", "error:1 to error:2"}},
	} {
		tests := []map[string]any{{"name": marker, "polarity": "success", "covers": c.covers}}
		err := checkPolarity(tests, c.errors)
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.HasPrefix(err.Error(), grpEnum+":") {
			t.Errorf("%s: err = %q", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q lacks %q", c.name, err, w)
			}
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), c.covers+"\"") {
			t.Errorf("%s: err quotes the reply: %q", c.name, err)
		}
	}
}

func TestTestwriterPromptSaysWhatToDoWithNoErrors(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Skip("prompt not readable this way: ", err)
	}
	if !strings.Contains(string(b), "lists no errors") {
		t.Error("testwriter.md does not say what to do when the contract lists no errors")
	}
}
