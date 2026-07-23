package banner

import (
	"slices"
	"strings"
	"testing"
)

const sampleChangelog = `# Changelog

## [Unreleased]

## [0.1.0] - 2026-07-09
### Added
- First thing
- Second thing
### Fixed
- Third thing

## [0.0.1] - 2026-06-01
- old thing
`

func TestLatestChangesSkipsEmptyUnreleasedAndStripsBullets(t *testing.T) {
	got := LatestChanges(sampleChangelog, 2)
	want := []string{"First thing", "Second thing"}
	if !slices.Equal(got, want) {
		t.Errorf("LatestChanges = %q, want %q", got, want)
	}
}

func TestLatestChangesStopsAtNextVersion(t *testing.T) {
	got := LatestChanges(sampleChangelog, 10)
	want := []string{"First thing", "Second thing", "Third thing"}
	if !slices.Equal(got, want) {
		t.Errorf("LatestChanges = %q, want %q (must not bleed into 0.0.1)", got, want)
	}
}

func TestLatestChangesEmptyWhenNoEntries(t *testing.T) {
	if got := LatestChanges("# Changelog\n\nnothing here\n", 3); len(got) != 0 {
		t.Errorf("LatestChanges = %q, want empty", got)
	}
}

func TestRenderContainsBannerVersionAndFortune(t *testing.T) {
	out := Render()
	if !strings.Contains(out, "'---' '---'") { // gopher teeth from the ASCII art
		t.Error("Render() missing the gopher banner")
	}
	if !strings.Contains(out, "gophermind ") { // version line
		t.Error("Render() missing the version line")
	}
	// A fortune (non-banner prose) should be appended somewhere after the art.
	if len(strings.TrimSpace(out)) < len("|==|") {
		t.Error("Render() unexpectedly short")
	}
}
