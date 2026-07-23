package prompt

import (
	"path/filepath"
	"strings"
	"testing"

	"gophermind/internal/golden"
)

// TestGoPherItBannerWidth guards the lockup constraint: the tagline banner sits
// directly under GopherArt (~46 columns) and must survive an 80-column terminal.
func TestGoPherItBannerWidth(t *testing.T) {
	const maxCols = 46
	for i, line := range strings.Split(GoPherItBanner, "\n") {
		if n := len([]rune(line)); n > maxCols {
			t.Errorf("line %d is %d columns, want <= %d: %q", i, n, maxCols, line)
		}
	}
}

// TestGoPherItBannerIsASCII keeps the banner safe in a Go raw string literal and
// in terminals without wide-character support.
func TestGoPherItBannerIsASCII(t *testing.T) {
	if strings.Contains(GoPherItBanner, "\x60") {
		t.Error("banner contains a backtick; it cannot live in a raw string literal")
	}
	for _, r := range GoPherItBanner {
		if r > 126 || (r < 32 && r != '\n') {
			t.Errorf("non-printable-ASCII rune %q in banner", r)
		}
	}
}

// TestGoPherItBannerGolden snapshots the art so any accidental edit shows up in
// review. Update with: GOLDEN_UPDATE=1 go test ./internal/prompt/
func TestGoPherItBannerGolden(t *testing.T) {
	golden.Assert(t, filepath.Join("testdata", "gopher_it_banner.golden"), GoPherItBanner)
}
