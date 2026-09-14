package intro

import (
	"bytes"
	"strings"
	"testing"
)

func TestVisibleWidthIgnoresColor(t *testing.T) {
	line := []Segment{seg("ab", basePalette.Fur), bseg("cde", basePalette.Cyan)}
	if got := visibleWidth(line); got != 5 {
		t.Errorf("visibleWidth = %d, want 5", got)
	}
}

func TestRenderLineCentersAndResets(t *testing.T) {
	line := []Segment{seg("xy", basePalette.Fur)}
	out := renderLine(line, 10)
	// 2 visible chars in width 10 -> 4 leading spaces.
	if !strings.HasPrefix(out, "    ") {
		t.Errorf("expected 4 leading spaces, got %q", out)
	}
	if !strings.HasSuffix(out, reset) {
		t.Error("line should end with a reset sequence")
	}
	if !strings.Contains(out, "xy") {
		t.Error("line should contain its text")
	}
}

func TestEyeBlinkVsOpen(t *testing.T) {
	blink := eyeInterior(AnimState{Blink: true}, basePalette, 0)
	open := eyeInterior(AnimState{Blink: false, Shine: -1}, basePalette, 0)
	// Blink is a single closed-eye segment; open eye splits around the pupil.
	if len(blink) != 1 {
		t.Errorf("blink should be one segment, got %d", len(blink))
	}
	if len(open) < 2 {
		t.Errorf("open eye should split around the pupil, got %d segments", len(open))
	}
	// Both lenses render the same visible width (13 columns).
	if visibleWidth(blink) != 13 || visibleWidth(open) != 13 {
		t.Errorf("lens widths differ: blink=%d open=%d", visibleWidth(blink), visibleWidth(open))
	}
}

func TestFrameHasCursorHomeAndColor(t *testing.T) {
	f := frame(AnimState{EyeX: 0, Shine: -1}, basePalette, 80)
	if !strings.HasPrefix(f, home) {
		t.Error("frame should start by homing the cursor")
	}
	if !strings.Contains(f, "\x1b[38;2;") {
		t.Error("frame should contain truecolor escapes")
	}
	if strings.Count(f, "\n") < 20 {
		t.Errorf("frame should have the full mascot's rows, got %d newlines", strings.Count(f, "\n"))
	}
}

func TestPlaySequenceSkips(t *testing.T) {
	var buf bytes.Buffer
	calls := 0
	skip := func() bool { calls++; return true } // skip immediately
	if !playSequence(&buf, 80, skip) {
		t.Error("playSequence should report it was skipped")
	}
	if calls != 1 {
		t.Errorf("skip should be polled once before bailing, got %d", calls)
	}
	if buf.Len() == 0 {
		t.Error("at least the first frame should have been written before skipping")
	}
}

func TestPlaySequenceCompletesWithoutSkip(t *testing.T) {
	var buf bytes.Buffer
	if playSequence(&buf, 80, func() bool { return false }) {
		t.Error("playSequence should report completion when never skipped")
	}
	if buf.Len() == 0 {
		t.Error("frames should have been written")
	}
}

func TestShouldPlayDisabled(t *testing.T) {
	t.Setenv("GOPHERMIND_INTRO", "off")
	if shouldPlay() {
		t.Error("GOPHERMIND_INTRO=off must disable the intro")
	}
	t.Setenv("GOPHERMIND_INTRO", "")
	t.Setenv("NO_COLOR", "1")
	if shouldPlay() {
		t.Error("NO_COLOR must disable the intro")
	}
}
