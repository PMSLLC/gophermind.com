package plan

// This file turns a model's context window into pass sizes. It exists
// because the incident that started this design was a 98,304-token window
// overflowing at iteration 11: the caps were fixed constants and nothing
// asked the model how much room there was.

// Measured worst-case prompt parts, in bytes. Each is pinned by a test that
// builds the largest prompt the code can produce, so a change that grows a
// prompt fails there rather than silently eating a window.
//
//   - pass1FixedBytes: everything in a pass-1 prompt except the brief chunk
//     (the capped overview, the capped outline, and the instructions).
//   - pass2BaseBytes / pass2PerStepBytes: an ordinary pass-2 prompt without
//     its brief excerpts, and the cost of one more step in its batch.
//   - pass2ReplanBaseBytes / pass2ReplanPerStepBytes: the same for a
//     re-planning pass, whose every step also carries its previous
//     specification and the reason it is being redone. A re-planning batch is
//     smaller (reconcileBatch), which is what keeps the two totals close.
const (
	pass1FixedBytes = 12044

	pass2BaseBytes    = 18047
	pass2PerStepBytes = 739

	pass2ReplanBaseBytes    = 18225
	pass2ReplanPerStepBytes = 1505
)

// Floors. Below these a size stops meaning what its name says: a chunk
// smaller than a few paragraphs cannot carry a section of a brief, and one
// step per pass is the smallest batch there is.
const (
	floorChunkBytes = 500
	floorBriefBytes = minBriefBytes
	floorStepsPass  = 1
)

// How a context window in tokens becomes a prompt budget in bytes.
//
//   - bytesPerToken is the pessimistic end of the usual 3 to 4 bytes per token
//     for mostly-ASCII text, so a prompt of mostly short words still fits. It
//     is not conservative for CJK or other 4-byte-rune text, which tokenizes
//     at roughly 1.3 to 2 bytes per token, and a ratio that would cover it
//     would leave even the floors unable to fit an 8k window. At 8k the margin
//     is thin (see the table in TestSizesForFitsTheWindow). The chat template
//     the server wraps around the prompt is not counted either. Callers that
//     want to warn the owner rather than overflow should ask FitsWindow.
//   - the reply needs room inside the same window: a window/replyReserveDiv
//     slice is kept for it, never less than minReplyTokens.
const (
	bytesPerToken   = 3
	replyReserveDiv = 8
	minReplyTokens  = 1024

	// maxWindowTokens clamps a reported window before it is multiplied, so a
	// bogus huge value cannot overflow (32-bit ints included). It is far above
	// any real window and well above the size at which the defaults fit.
	maxWindowTokens = 1 << 24
)

// PromptBudgetBytes is the number of prompt bytes SizesFor will fit inside a
// context window of contextTokens tokens, after leaving room for the reply. A
// window of zero or less (unknown) has no budget, and neither has one so
// small that the reply reserve takes all of it. A window larger than
// maxWindowTokens is treated as maxWindowTokens.
func PromptBudgetBytes(contextTokens int) int {
	if contextTokens <= 0 {
		return 0
	}
	if contextTokens > maxWindowTokens {
		contextTokens = maxWindowTokens
	}
	reply := contextTokens / replyReserveDiv
	if reply < minReplyTokens {
		reply = minReplyTokens
	}
	if reply >= contextTokens {
		return 0
	}
	return (contextTokens - reply) * bytesPerToken
}

// WorstPromptBytes is the largest prompt these options can produce, for
// either pass. It is what SizesFor fits into the budget and what the tests
// pin against the measured constants above.
func WorstPromptBytes(o Options, o2 Options2) int {
	o, o2 = o.WithDefaults(), o2.WithDefaults()
	chunk, brief, steps := o.ChunkBytes, o2.BriefBytes, o2.StepsPerPass
	worst := chunk + pass1FixedBytes
	// An ordinary batch is the caller's size; a re-planning batch is capped
	// by reconcileBatch but each of its steps costs more. Neither dominates
	// the other at every size, so both are computed.
	if n := pass2BaseBytes + brief + steps*pass2PerStepBytes; n > worst {
		worst = n
	}
	if n := pass2ReplanBaseBytes + brief + reconcileBatch(steps)*pass2ReplanPerStepBytes; n > worst {
		worst = n
	}
	return worst
}

// SizesFor returns the pass sizes to use with a model whose context window is
// contextTokens tokens: the largest ChunkBytes, BriefBytes and StepsPerPass
// whose worst-case prompt still leaves room for the reply.
//
// Three properties hold, and each is pinned by a test:
//
//   - it never returns a size above today's defaults, so a huge window
//     changes nothing and the measured worst cases stay the measured worst
//     cases;
//   - it is monotonic: a larger window never gives a smaller size;
//   - an unknown window (zero or less, which is what a probe that failed
//     reports) gives the defaults, which are safe on a 32k window.
//
// OverviewCap is left at its default. Pass 2 reads the overview through the
// same OverviewCapBytes constant whatever pass 1 was told, so shrinking one
// without the other would only make the two disagree; the overview is part of
// the fixed cost measured in pass1FixedBytes and pass2BaseBytes.
//
// The budget assumes mostly-ASCII text: at 8k the margin is thin, CJK-heavy
// briefs can still overflow, and the chat template is not counted (see
// bytesPerToken).
//
// Below about 7,000 tokens no setting fits. For any positive window SizesFor
// then returns its floor sizes, never the defaults, and FitsWindow reports
// false so the caller can warn; otherwise the run reports the server's own
// context-limit error, which RunPass1 and RunPass2 already translate into
// "lower these sizes".
func SizesFor(contextTokens int) (Options, Options2) {
	if contextTokens <= 0 {
		return Options{}, Options2{}
	}
	budget := PromptBudgetBytes(contextTokens)
	for scale := 100; scale > 0; scale-- {
		o, o2 := sizesAt(scale)
		if WorstPromptBytes(o, o2) <= budget {
			return o, o2
		}
	}
	return floorSizes()
}

// FitsWindow reports whether even the smallest sizes (the floors) produce a
// worst-case prompt inside the budget of a window of contextTokens tokens.
// False means no setting is safe and the caller should warn the owner. An
// unknown window (zero or less) reports true: there is nothing to warn about.
func FitsWindow(contextTokens int) bool {
	if contextTokens <= 0 {
		return true
	}
	o, o2 := floorSizes()
	return WorstPromptBytes(o, o2) <= PromptBudgetBytes(contextTokens)
}

func floorSizes() (Options, Options2) {
	return Options{ChunkBytes: floorChunkBytes},
		Options2{BriefBytes: floorBriefBytes, StepsPerPass: floorStepsPass}
}

// sizesAt scales the three defaults by scale percent, never below the floors
// and never above the defaults. It is non-decreasing in scale, which is what
// makes SizesFor monotonic in the window.
func sizesAt(scale int) (Options, Options2) {
	if scale > 100 {
		scale = 100
	}
	if scale < 0 {
		scale = 0
	}
	atLeast := func(v, floor int) int {
		if v < floor {
			return floor
		}
		return v
	}
	return Options{
			ChunkBytes: atLeast(DefaultChunkBytes*scale/100, floorChunkBytes),
		}, Options2{
			BriefBytes:   atLeast(defaultBriefBytes*scale/100, floorBriefBytes),
			StepsPerPass: atLeast(defaultStepsPerPass*scale/100, floorStepsPass),
		}
}
