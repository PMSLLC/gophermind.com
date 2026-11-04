package planner

import (
	"strings"
	"testing"
)

// A test table that reuses one strings.NewReader passes its first case and fails
// every later one, because the first read uses the reader up. The function
// under test is fine, so no implementation attempt can fix it: a run stopped
// on exactly that (TestOpenFile/dash_name_uses_stdin). The prompt must say so.
func TestTestwriterPromptForbidsSharingAReaderAcrossCases(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Fatal(err)
	}
	p := string(b)
	if !strings.Contains(p, "share a reader") || !strings.Contains(p, "its own reader") {
		t.Error("testwriter.md does not tell the model that every case needs its own reader")
	}
}

// The prompt asked for "Exactly one rule" and then listed two requirements, a
// happy-path success test and a negative test for every error. The wording
// must count them.
func TestTestwriterPromptStatesTwoRules(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Fatal(err)
	}
	p := string(b)
	if !strings.Contains(p, "Two rules you must meet:") || strings.Contains(p, "Exactly one rule") {
		t.Error("testwriter.md does not state two rules")
	}
}

func TestTestwriterPromptForbidsARealHost(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Fatal(err)
	}
	if p := string(b); !strings.Contains(p, "never names a real host") || !strings.Contains(p, "srv.URL") || !strings.Contains(p, ".invalid") {
		t.Error("testwriter.md does not forbid a real host")
	}
}

// The test file is Go source; asking for it inside a JSON string made the
// model escape every quote and newline. Both test-writer prompts ask for the
// file in a fenced go block instead.
func TestTestwriterPromptAsksForAFencedGoFile(t *testing.T) {
	for _, name := range []string{"prompts/testwriter.md", "prompts/testwriter_accept.md"} {
		b, err := promptFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		p := string(b)
		if !strings.Contains(p, "one fenced go block") {
			t.Errorf("%s does not ask for the file in one fenced go block", name)
		}
		if strings.Contains(p, `"test_file"`) {
			t.Errorf("%s still asks for a test_file JSON field", name)
		}
	}
}
