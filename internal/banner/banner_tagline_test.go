package banner

import (
	"strings"
	"testing"
)

// TestRenderIncludesTagline verifies the "GO PHER IT" wordmark appears in the
// startup splash, under the gopher and above the version line.
func TestRenderIncludesTagline(t *testing.T) {
	out := RenderWith(Options{})

	// A distinctive slice of the tagline art: the "PH" of PHER.
	const needle = "|_) |_|"
	if !strings.Contains(out, needle) {
		t.Fatalf("tagline missing from banner; want substring %q in:\n%s", needle, out)
	}

	// The gopher's buck teeth must still come first, then the tagline.
	gopher := strings.Index(out, "|==|")
	tagline := strings.Index(out, needle)
	if gopher == -1 {
		t.Fatal("gopher art missing from banner")
	}
	if tagline < gopher {
		t.Errorf("tagline at %d precedes gopher at %d; want gopher first", tagline, gopher)
	}
}

// TestRenderTaglineIsUncolored keeps the plain-text path escape-free so the
// banner stays readable when piped or captured in tests.
func TestRenderTaglineIsUncolored(t *testing.T) {
	if strings.Contains(RenderWith(Options{}), "\x1b[") {
		t.Error("banner contains ANSI escapes; lipgloss should degrade to plain text off-TTY")
	}
}
