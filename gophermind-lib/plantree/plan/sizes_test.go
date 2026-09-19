package plan

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// worstPass1 builds the largest pass-1 prompt possible for a chunk size.
func worstPass1(chunkBytes int) string {
	return Pass1Prompt(strings.Repeat("n", 100),
		strings.Repeat("o", OverviewCapBytes),
		strings.Repeat("x", outlineCapBytes+60),
		Chunk{Index: 0, Text: strings.Repeat("b", chunkBytes)}, 99)
}

// worstPass2For builds the largest pass-2 prompt possible for a brief budget
// and a batch size, ordinary or re-planning.
func worstPass2For(t *testing.T, briefBytes, batch int, replan bool) string {
	t.Helper()
	phase := node(t, "phase-001", strings.Repeat("p", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	task := node(t, "phase-001.task-001", strings.Repeat("t", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		s := node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("s", 200), strings.Repeat("d", 500), "")
		if replan {
			s.Planning.Stage = plantree.StageNeedsReconciliation
			s.Work = &plantree.Work{Description: strings.Repeat("w", priorWorkBytes*8), AcceptanceCriteria: []string{"x"}}
			s.ResumeNote = strings.Repeat("r", reconcileNoteShownBytes*10)
		}
		steps = append(steps, s)
	}
	return Pass2Prompt(Pass2Input{
		Project: strings.Repeat("n", 100), Overview: strings.Repeat("o", OverviewCapBytes*7),
		Facts: strings.Repeat("f", 40000), Decisions: worstDecisions("q") + strings.Repeat("d", 40000),
		Excerpts: strings.Repeat("e", 40000), ExcerptsCap: briefBytes,
		Phase: phase, Task: task, Siblings: steps, Batch: steps[:batch],
	})
}

// TestWorstPromptBytesMatchesTheRealPrompts is the pin: the constants
// SizesFor reasons with are the sizes the prompt builders really produce, so
// a change to a prompt shows up here rather than as an overflow in a session.
func TestWorstPromptBytesMatchesTheRealPrompts(t *testing.T) {
	for _, chunk := range []int{floorChunkBytes, 4000, DefaultChunkBytes} {
		got := len(worstPass1(chunk))
		if want := chunk + pass1FixedBytes; got != want {
			t.Errorf("worst pass-1 prompt with a %d byte chunk is %d bytes, pass1FixedBytes says %d", chunk, got, want)
		}
	}
	for _, brief := range []int{floorBriefBytes, 1320, defaultBriefBytes} {
		for _, batch := range []int{1, 2, 3} {
			got := len(worstPass2For(t, brief, batch, true))
			if want := pass2ReplanBaseBytes + brief + batch*pass2ReplanPerStepBytes; got != want {
				t.Errorf("worst re-planning pass-2 prompt (brief %d, batch %d) is %d bytes, the constants say %d", brief, batch, got, want)
			}
		}
		for _, batch := range []int{1, defaultStepsPerPass} {
			got := len(worstPass2For(t, brief, batch, false))
			if want := pass2BaseBytes + brief + batch*pass2PerStepBytes; got != want {
				t.Errorf("worst ordinary pass-2 prompt (brief %d, batch %d) is %d bytes, the constants say %d", brief, batch, got, want)
			}
		}
	}
	// The default sizes must still produce the worst case M5 measured.
	if got := len(worstPass2For(t, defaultBriefBytes, defaultStepsPerPass, false)); got != 26481 {
		t.Errorf("ordinary worst case is %d bytes, M5 measured 26481", got)
	}
	if got := len(worstPass2For(t, defaultBriefBytes, reconcileStepsPerPass, true)); got != 26740 {
		t.Errorf("re-planning worst case is %d bytes, M5 measured 26740", got)
	}
	if got := WorstPromptBytes(Options{}, Options2{}); got != 26740 {
		t.Errorf("WorstPromptBytes at the defaults = %d, want 26740", got)
	}
	if WorstPromptBytes(Options{}, Options2{}) > 27000 {
		t.Error("the 27,000 byte pin was raised")
	}
}

func TestSizesForNeverExceedsTheDefaults(t *testing.T) {
	for _, w := range []int{1, 4096, 8192, 16384, 32768, 98304, 128000, 200000, 2000000} {
		o, o2 := SizesFor(w)
		if o.ChunkBytes > DefaultChunkBytes || o2.BriefBytes > defaultBriefBytes || o2.StepsPerPass > defaultStepsPerPass {
			t.Errorf("SizesFor(%d) = %+v %+v, above the defaults", w, o, o2)
		}
	}
}

func TestSizesForIsMonotonic(t *testing.T) {
	// Zero options mean the defaults, so compare what each result means, not
	// its raw fields. Every window from 1 is compared with its neighbour, so a
	// tiny positive window cannot silently jump to the largest sizes.
	check := func(w int, prevO Options, prevO2 Options2) (Options, Options2) {
		o, o2 := SizesFor(w)
		a, a2 := o.WithDefaults(), o2.WithDefaults()
		if a.ChunkBytes < prevO.ChunkBytes || a2.BriefBytes < prevO2.BriefBytes || a2.StepsPerPass < prevO2.StepsPerPass {
			t.Fatalf("SizesFor(%d) = %+v %+v is smaller than the window below it (%+v %+v)", w, a, a2, prevO, prevO2)
		}
		return a, a2
	}
	prev, prev2 := SizesFor(1)
	prev, prev2 = prev.WithDefaults(), prev2.WithDefaults()
	if prev.ChunkBytes != floorChunkBytes || prev2.BriefBytes != floorBriefBytes || prev2.StepsPerPass != floorStepsPass {
		t.Fatalf("SizesFor(1) = %+v %+v, want the floors", prev, prev2)
	}
	for w := 2; w <= 4000; w++ {
		prev, prev2 = check(w, prev, prev2)
	}
	for w := 4001; w <= 200000; w += 137 {
		prev, prev2 = check(w, prev, prev2)
	}
}

func TestPromptBudgetBytesDoesNotOverflow(t *testing.T) {
	for _, w := range []int{math.MaxInt, math.MaxInt32, math.MaxInt32 + 1, maxWindowTokens, maxWindowTokens + 1} {
		if got := PromptBudgetBytes(w); got <= 0 || got != PromptBudgetBytes(maxWindowTokens) && w > maxWindowTokens {
			t.Errorf("PromptBudgetBytes(%d) = %d", w, got)
		}
		o, o2 := SizesFor(w)
		if o.ChunkBytes > DefaultChunkBytes || o2.StepsPerPass > defaultStepsPerPass {
			t.Errorf("SizesFor(%d) = %+v %+v, above the defaults", w, o, o2)
		}
	}
}

func TestFitsWindow(t *testing.T) {
	for _, w := range []int{0, -1} {
		if !FitsWindow(w) {
			t.Errorf("FitsWindow(%d) = false; an unknown window must report true", w)
		}
	}
	if !FitsWindow(8192) {
		t.Error("the floors must fit an 8192 token window")
	}
	for _, w := range []int{1, 500, 2000, 4096} {
		if FitsWindow(w) {
			t.Errorf("FitsWindow(%d) = true, the floors cannot fit that window", w)
		}
	}
	// FitsWindow agrees with the sizes SizesFor gives for the same window.
	for _, w := range []int{1, 5000, 6000, 7000, 8192, 32768} {
		o, o2 := SizesFor(w)
		if got, want := FitsWindow(w), WorstPromptBytes(o, o2) <= PromptBudgetBytes(w); got != want {
			t.Errorf("FitsWindow(%d) = %v but SizesFor's worst prompt fitting is %v", w, got, want)
		}
	}
}

func TestSizesForUnknownWindowGivesTheDefaults(t *testing.T) {
	for _, w := range []int{0, -1, -98304} {
		o, o2 := SizesFor(w)
		if (o != Options{}) || (o2 != Options2{}) {
			t.Errorf("SizesFor(%d) = %+v %+v, want the zero options, which mean the defaults", w, o, o2)
		}
		if got := WorstPromptBytes(o, o2); got > 27000 {
			t.Errorf("the fallback sizes are %d bytes, above the 32k-safe pin", got)
		}
	}
}

// TestSizesForFitsTheWindow is the table the milestone promises: at each of
// these windows the worst-case prompt, in tokens, leaves room for the reply.
func TestSizesForFitsTheWindow(t *testing.T) {
	for _, w := range []int{8192, 16384, 32768, 98304, 128000} {
		o, o2 := SizesFor(w)
		worst := WorstPromptBytes(o, o2)
		budget := PromptBudgetBytes(w)
		if worst > budget {
			t.Errorf("SizesFor(%d) = chunk %d, brief %d, steps %d: worst prompt %d bytes over the %d byte budget",
				w, o.ChunkBytes, o2.BriefBytes, o2.StepsPerPass, worst, budget)
		}
		t.Logf("window %6d: chunk %5d, brief %4d, steps %d -> worst %d bytes of %d (%d prompt tokens of %d)",
			w, o.ChunkBytes, o2.BriefBytes, o2.StepsPerPass, worst, budget, worst/bytesPerToken, w)
		if err := o.Validate(); err != nil {
			t.Errorf("SizesFor(%d) returned options it rejects: %v", w, err)
		}
		if err := o2.Validate(); err != nil {
			t.Errorf("SizesFor(%d) returned options it rejects: %v", w, err)
		}
	}
}

// TestSizesForAtTheDefaultsIsTheDefaults keeps the common case honest: a
// model big enough changes nothing at all.
func TestSizesForAtTheDefaultsIsTheDefaults(t *testing.T) {
	o, o2 := SizesFor(32768)
	if o.ChunkBytes != DefaultChunkBytes || o2.BriefBytes != defaultBriefBytes || o2.StepsPerPass != defaultStepsPerPass {
		t.Errorf("SizesFor(32768) = %+v %+v, want the defaults", o, o2)
	}
}

func TestReconcileBatchNeverExceedsTheOrdinaryBatch(t *testing.T) {
	for steps := 1; steps <= 10; steps++ {
		got := reconcileBatch(steps)
		if got > steps || got > reconcileStepsPerPass || got < 1 {
			t.Errorf("reconcileBatch(%d) = %d", steps, got)
		}
	}
	if reconcileBatch(0) != reconcileStepsPerPass {
		t.Error("an unset StepsPerPass must leave the re-planning batch at its own size")
	}
}
