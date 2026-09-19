package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOverviewRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadOverview(dir); err != nil || got != "" {
		t.Fatalf("missing overview = %q, %v", got, err)
	}
	if err := WriteOverview(dir, "first\n\n"); err != nil {
		t.Fatal(err)
	}
	if err := WriteOverview(dir, "second version"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadOverview(dir); got != "second version\n" {
		t.Errorf("ReadOverview = %q", got)
	}
}

func TestFitOverview(t *testing.T) {
	short := "fits fine"
	if FitOverview(short, 100) != short {
		t.Error("text within the cap must be unchanged")
	}
	long := strings.Repeat("A paragraph of overview text.\n\n", 40)
	got := FitOverview(long, 200)
	if len(got) > 200 || !strings.HasSuffix(got, truncMarker) {
		t.Errorf("len=%d suffix ok=%v", len(got), strings.HasSuffix(got, truncMarker))
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, truncMarker)) {
		t.Error("the kept text must be a prefix of the original")
	}
	multibyte := strings.Repeat("é", 300)
	if got := FitOverview(multibyte, 101); !utf8.ValidString(got) || len(got) > 101 {
		t.Errorf("multibyte cut: valid=%v len=%d", utf8.ValidString(got), len(got))
	}
	if got := FitOverview(strings.Repeat("x", 50), 5); len(got) == 0 {
		t.Error("a tiny cap must still return something")
	}
}

func TestOutlineListsPhasesAndTasksOnly(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	got, err := Outline(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != "- Phase: Foundation\n  - Task: Repo layout\n" {
		t.Errorf("Outline = %q", got)
	}
}

func TestOutlineIsBoundedAndSaysWhatItOmitted(t *testing.T) {
	r := newRepo(t)
	var phases []PhaseOut
	for i := 0; i < 50; i++ {
		phases = append(phases, PhaseOut{Title: strings.Repeat("phase title ", 8) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Digest: "d"})
	}
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: phases}); err != nil {
		t.Fatal(err)
	}
	got, _ := Outline(r)
	if len(got) > outlineCapBytes+80 || !strings.Contains(got, "more phases and tasks not shown") {
		t.Errorf("outline len=%d, tail=%q", len(got), got[max(0, len(got)-120):])
	}
}

func TestPass1PromptCarriesOneChunkAndNoOthers(t *testing.T) {
	c := Chunk{Index: 1, Title: "Auth", Text: "The service must support login.\n"}
	p := Pass1Prompt("demo", "an overview", "- Phase: Foundation\n", c, 3)
	for _, want := range []string{`"demo"`, "part 2 of 3", "an overview", "- Phase: Foundation", "The service must support login.", "section: Auth", "ONE JSON object"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	empty := Pass1Prompt("demo", "", "", Chunk{Index: 0, Text: "x"}, 1)
	if strings.Count(empty, "(none yet)") != 2 {
		t.Errorf("empty overview and outline should read (none yet)")
	}
}

func TestRetryAndCompressPrompts(t *testing.T) {
	if p := RetryPrompt("ORIGINAL", "digest is empty"); !strings.Contains(p, "ORIGINAL") || !strings.Contains(p, "digest is empty") {
		t.Errorf("RetryPrompt = %q", p)
	}
	if p := CompressPrompt("long overview", 500); !strings.Contains(p, "Compress") || !strings.Contains(p, "500") || !strings.Contains(p, "long overview") {
		t.Errorf("CompressPrompt = %q", p)
	}
}
