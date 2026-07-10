package prompt

import (
	"path/filepath"
	"testing"

	"gophermind/internal/golden"
)

// TestDefaultPromptGolden snapshots the built default system prompt so any
// unintended change to the template or builder is caught in review. Update the
// golden with: GOLDEN_UPDATE=1 go test ./internal/prompt/
func TestDefaultPromptGolden(t *testing.T) {
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, filepath.Join("testdata", "default_prompt.golden"), b.Build())
}
