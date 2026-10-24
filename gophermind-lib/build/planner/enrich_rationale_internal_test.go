package planner

import (
	"strings"
	"testing"
)

func TestTrimRationaleCutsAtTheLastSentenceUnderTheLimit(t *testing.T) {
	s1 := strings.Repeat("a", 300) + ". "
	s2 := strings.Repeat("b", 250) + ". "
	s3 := strings.Repeat("c", 200) + "."
	got, trimmed := trimRationale(s1 + s2 + s3)
	if !trimmed {
		t.Fatal("an over-long rationale must be trimmed")
	}
	if want := strings.TrimSpace(s1 + s2); got != want {
		t.Errorf("trim kept %d runes, want the first two sentences (%d)", len([]rune(got)), len([]rune(want)))
	}
}

func TestTrimRationaleFallsBackToAWordBoundary(t *testing.T) {
	in := strings.Repeat("word ", 200) // 1000 runes, no sentence end
	got, trimmed := trimRationale(in)
	if !trimmed || len([]rune(got)) > 600 || len([]rune(got)) < 20 {
		t.Fatalf("trimmed=%v len=%d", trimmed, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "word") || strings.HasSuffix(got, "...") {
		t.Errorf("want a whole last word and no ellipsis, got the tail %q", got[len(got)-10:])
	}
}

func TestTrimRationaleLeavesAcceptableTextAlone(t *testing.T) {
	for _, in := range []string{"short", strings.Repeat("x", 600), "  " + strings.Repeat("y", 40) + "  "} {
		if got, trimmed := trimRationale(in); trimmed || got != strings.TrimSpace(in) {
			t.Errorf("trimRationale(len %d) = trimmed %v", len(in), trimmed)
		}
	}
}

func TestMergeEnrichmentTrimsALongRationaleAndWarns(t *testing.T) {
	long := strings.Repeat("This sentence explains the reason for the function. ", 16) // 831 runes
	merged, ds, warns := mergeEnrichment(draftFixture(t), replyFixture(t, func(m map[string]any) { m["rationale"] = long }), contractFixtureForEnrich(), enrichEnvFixture())
	if len(ds) != 0 {
		t.Fatalf("defects: %v", kindsOf(ds))
	}
	if n := len([]rune(merged["rationale"].(string))); n > 600 || n < 20 {
		t.Errorf("merged rationale has %d characters", n)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "rationale_trimmed") && strings.Contains(w, "fn-register") && strings.Contains(w, "831") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want rationale_trimmed naming fn-register and 831", warns)
	}
}

func TestMergeEnrichmentShortRationaleStatesTheLength(t *testing.T) {
	_, ds, _ := mergeEnrichment(draftFixture(t), replyFixture(t, func(m map[string]any) { m["rationale"] = "too short" }), contractFixtureForEnrich(), enrichEnvFixture())
	if len(ds) != 1 || ds[0].kind != grpMissing {
		t.Fatalf("defects = %v", ds)
	}
	for _, want := range []string{"9 characters", "20"} {
		if !strings.Contains(ds[0].msg, want) {
			t.Errorf("message %q lacks %q", ds[0].msg, want)
		}
	}
	if strings.Contains(ds[0].msg, "too short") {
		t.Errorf("message quotes the reply: %q", ds[0].msg)
	}
}

func TestRationaleProblemWording(t *testing.T) {
	if got := rationaleProblem(5, true); !strings.Contains(got, "5 characters") || !strings.Contains(got, "at least 20") {
		t.Errorf("short wording: %q", got)
	}
	if got := rationaleProblem(0, false); !strings.Contains(got, "missing") || !strings.Contains(got, "20 to 600") {
		t.Errorf("missing wording: %q", got)
	}
}
