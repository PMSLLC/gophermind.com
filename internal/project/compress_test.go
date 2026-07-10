package project

import (
	"strings"
	"testing"
)

func TestCompressContextFitsBudget(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Important Heading\n\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("this is a filler line of context number x\n")
	}
	sb.WriteString("- a key bullet point\n")
	text := sb.String()

	out := CompressContext(text, 20) // ~80 bytes
	if len(out) > 20*bytesPerToken+80 { // allow the trailing note
		t.Errorf("compressed output too large: %d bytes", len(out))
	}
	// Structural lines (heading, bullet) should be preferentially kept.
	if !strings.Contains(out, "# Important Heading") {
		t.Errorf("heading should be preserved:\n%s", out)
	}
}

func TestCompressContextNoopWhenFits(t *testing.T) {
	if got := CompressContext("small", 100); got != "small" {
		t.Errorf("fitting text should be unchanged, got %q", got)
	}
	if got := CompressContext("x", 0); got != "x" {
		t.Errorf("zero budget should be a no-op, got %q", got)
	}
}
