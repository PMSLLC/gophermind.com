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
