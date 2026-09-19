package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"gophermind/gophermind-lib/plantree/plan"
)

// roundQuestion builds a question with two options and a recommendation.
func roundQuestion(id, text string, multi bool) plan.Question {
	return plan.Question{
		ID: id, Question: text, Why: "it changes the schema",
		Options: []plan.Option{
			{ID: "opt-1", Label: "SQLite", Description: "embedded"},
			{ID: "opt-2", Label: "Postgres", Description: "server"},
		},
		MultiSelect:   multi,
		AllowFreeText: true,
		Recommended:   &plan.Recommendation{OptionIDs: []string{"opt-1"}, Rationale: "simplest to run"},
		Affects:       []string{"phase-001.task-001"},
		Status:        plan.QuestionOpen,
	}
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// pressRound sends each key in order and returns the round and the last result.
func pressRound(r questionRound, keys ...tea.KeyMsg) (questionRound, roundResult) {
	var res roundResult
	for _, k := range keys {
		r, res = r.Update(k)
	}
	return r, res
}

func TestQuestionRoundChoosesOneOptionUnlessMultiSelect(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which database?", false)}, nil, 80)
	if r.answeredCount() != 0 {
		t.Fatal("a fresh round starts with nothing chosen")
	}
	r, _ = pressRound(r, key(tea.KeySpace))
	if got := r.answers(); len(got) != 1 || len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-1" {
		t.Fatalf("answers = %+v, want the option under the cursor", got)
	}
	// A single-answer question replaces rather than accumulates.
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-2" {
		t.Fatalf("answers = %+v, want only the second option", got)
	}
	// Choosing the same option again clears it.
	r, _ = pressRound(r, key(tea.KeySpace))
	if len(r.answers()) != 0 || r.answeredCount() != 0 {
		t.Errorf("a second press did not clear the choice: %+v", r.answers())
	}
}

func TestQuestionRoundMultiSelectAccumulatesInOptionOrder(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which caches?", true)}, nil, 80)
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyLeft), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || strings.Join(got[0].Answer.OptionIDs, ",") != "opt-1,opt-2" {
		t.Errorf("answers = %+v, want both options in option order", got)
	}
}

func TestQuestionRoundNeverChoosesTheRecommendationForYou(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	r := newQuestionRound(roundAnswer, []plan.Question{q}, nil, 80)
	if len(r.answers()) != 0 {
		t.Error("the recommendation was taken as an answer")
	}
	v := r.View()
	if !strings.Contains(v, "SQLite - embedded (recommended)") {
		t.Errorf("the recommendation is not marked:\n%s", v)
	}
	if !strings.Contains(v, "recommended because: simplest to run") {
		t.Errorf("the rationale is missing:\n%s", v)
	}
	if strings.Contains(v, "[x]") {
		t.Errorf("something is pre-selected:\n%s", v)
	}
}

func TestQuestionRoundFreeTextIsAlwaysAvailableAndGrows(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	q.Options = nil // a question may have no options at all
	r := newQuestionRound(roundAnswer, []plan.Question{q}, nil, 80)
	if !strings.Contains(r.View(), "answer in your own words") {
		t.Error("a question with no options must say the answer is free text")
	}
	r, _ = pressRound(r, runes("e"))
	if !r.editing {
		t.Fatal("e did not open the note")
	}
	before := r.note.Height()
	r, _ = pressRound(r, runes("neither: use the existing store"))
	if !strings.Contains(r.View(), "neither: use the existing store") {
		t.Error("the note is not shown while it is being typed")
	}
	// Enough text to wrap several times must grow the box, up to its cap.
	r, _ = pressRound(r, runes(strings.Repeat("more words ", 60)))
	if r.note.Height() <= before || r.note.Height() > roundNoteRows {
		t.Errorf("note height = %d, want it grown to at most %d", r.note.Height(), roundNoteRows)
	}
	r, _ = pressRound(r, key(tea.KeyEsc))
	if r.editing {
		t.Fatal("esc did not leave the note")
	}
	got := r.answers()
	if len(got) != 1 || !strings.HasPrefix(got[0].Answer.Text, "neither: use the existing store") {
		t.Errorf("answers = %+v, want the typed note", got)
	}
	if got[0].Change {
		t.Error("an open question is answered, not changed")
	}
}

func TestQuestionRoundSkipLeavesAQuestionOpen(t *testing.T) {
	qs := []plan.Question{roundQuestion("q-001", "Which database?", false), roundQuestion("q-002", "Which framework?", false)}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	if r.canSubmit() {
		t.Fatal("an unanswered round must not be submittable")
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown), runes("s"))
	if !r.canSubmit() {
		t.Fatal("answered plus skipped must be submittable")
	}
	got := r.answers()
	if len(got) != 1 || got[0].ID != "q-001" {
		t.Errorf("answers = %+v, want only the answered question", got)
	}
	if !strings.Contains(r.View(), "this question stays open") {
		t.Error("the view does not say a skipped question stays open")
	}
}

func TestQuestionRoundSubmitsOnlyWhenItMay(t *testing.T) {
	qs := []plan.Question{roundQuestion("q-001", "Which database?", false), roundQuestion("q-002", "Which framework?", false)}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	r, res := pressRound(r, key(tea.KeyCtrlS))
	if res.Submitted {
		t.Fatal("submitted with nothing answered")
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown), key(tea.KeySpace))
	_, res = pressRound(r, key(tea.KeyCtrlS))
	if !res.Submitted {
		t.Fatal("a complete round did not submit")
	}
}

func TestQuestionRoundEscapeLeavesTheNoteBeforeItCancels(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which database?", false)}, nil, 80)
	r, _ = pressRound(r, runes("e"))
	r, res := pressRound(r, key(tea.KeyEsc))
	if res.Cancelled || r.editing {
		t.Fatalf("the first esc must only leave the note: cancelled=%v editing=%v", res.Cancelled, r.editing)
	}
	_, res = pressRound(r, key(tea.KeyEsc))
	if !res.Cancelled {
		t.Error("the second esc must cancel the round")
	}
}

func TestQuestionRoundViewShowsProgressTheCursorAndTheBriefExcerpt(t *testing.T) {
	qs := []plan.Question{
		roundQuestion("q-001", "Which database?", false),
		roundQuestion("q-002", "Which framework?", false),
		roundQuestion("q-003", "Which cache?", false),
	}
	r := newQuestionRound(roundAnswer, qs, []string{"[part 1 of 3]\nthe brief said storage matters"}, 80)
	v := r.View()
	if !strings.Contains(v, "Questions: 0 of 3 answered") {
		t.Errorf("no progress line:\n%s", v)
	}
	if !strings.Contains(v, "the brief said storage matters") {
		t.Errorf("the brief excerpt behind the question is missing:\n%s", v)
	}
	if !strings.Contains(v, "why: it changes the schema") {
		t.Errorf("the reason the question was asked is missing:\n%s", v)
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown))
	v = r.View()
	if !strings.Contains(v, "Questions: 1 of 3 answered") || !strings.Contains(v, "Q2: Which framework?") {
		t.Errorf("progress or cursor did not move:\n%s", v)
	}
	// The second question has no excerpt, so nothing is shown for it.
	if strings.Contains(v, "the brief said storage matters") {
		t.Errorf("another question's excerpt leaked:\n%s", v)
	}
}

func TestQuestionRoundListWindowFollowsTheCursor(t *testing.T) {
	var qs []plan.Question
	for i := 0; i < roundListRows+5; i++ {
		qs = append(qs, roundQuestion("q-00"+string(rune('1'+i)), "Question "+string(rune('a'+i)), false))
	}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	if !strings.Contains(r.View(), "below)") {
		t.Error("a long round must say how many questions are below the window")
	}
	for i := 0; i < len(qs)-1; i++ {
		r, _ = pressRound(r, key(tea.KeyDown))
	}
	v := r.View()
	if !strings.Contains(v, "above)") || !strings.Contains(v, "Question "+string(rune('a'+len(qs)-1))) {
		t.Errorf("the window did not follow the cursor:\n%s", v)
	}
}

func TestQuestionRoundChangeModeStartsFromTheStoredAnswer(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	q.Status = plan.QuestionAnswered
	q.Answer = &plan.Answer{OptionIDs: []string{"opt-1"}, Text: "for now"}
	other := roundQuestion("q-002", "Which framework?", false)
	other.Status = plan.QuestionAnswered
	other.Answer = &plan.Answer{OptionIDs: []string{"opt-2"}}

	r := newQuestionRound(roundChange, []plan.Question{q, other}, nil, 80)
	if !strings.Contains(r.View(), "0 of 2 changed") {
		t.Errorf("a revisiting round counts changes:\n%s", r.View())
	}
	if len(r.answers()) != 0 {
		t.Fatalf("leaving every answer alone must change nothing: %+v", r.answers())
	}
	if !r.canSubmit() {
		t.Fatal("a revisiting round may always be submitted")
	}
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || got[0].ID != "q-001" || !got[0].Change {
		t.Fatalf("answers = %+v, want one change", got)
	}
	if len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-2" || got[0].Answer.Text != "for now" {
		t.Errorf("the changed answer = %+v, want the new option and the kept note", got[0].Answer)
	}
}

func TestOneLineIsRuneSafe(t *testing.T) {
	for _, unit := range []string{"😀", "漢", "é", "a"} {
		for extra := 0; extra < 4; extra++ {
			s := strings.Repeat("x", extra) + strings.Repeat(unit, 200)
			got := oneLine(s)
			if !utf8.ValidString(got) {
				t.Errorf("oneLine cut %q+%d into invalid UTF-8", unit, extra)
			}
		}
	}
	if got := oneLine(strings.Repeat("a", 200)); got != strings.Repeat("a", 160)+"…" {
		t.Error("ASCII behaviour changed")
	}
	if got := oneLine("a\n b\t c"); got != "a b c" {
		t.Errorf("oneLine = %q", got)
	}
}

// checkWidth fails if any line of v is wider than w.
func checkWidth(t *testing.T, v string, w int) {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("width %d: line is %d wide: %q", w, got, line)
		}
	}
}

func TestQuestionRoundEmptyListIsInert(t *testing.T) {
	r := newQuestionRound(roundAnswer, nil, nil, 40)
	_ = r.View()
	for _, k := range []tea.KeyMsg{key(tea.KeyUp), key(tea.KeyDown), key(tea.KeyLeft), key(tea.KeyRight),
		key(tea.KeySpace), key(tea.KeyEnter), key(tea.KeyCtrlS), key(tea.KeyTab), runes("e"), runes("s"), runes("j"), runes("k")} {
		var res roundResult
		r, res = r.Update(k)
		if res.Cancelled || res.Submitted {
			t.Errorf("key %v finished an empty round", k)
		}
	}
	if _, res := r.Update(key(tea.KeyEsc)); !res.Cancelled {
		t.Error("esc must still cancel an empty round")
	}
	if r.count() != 0 || len(r.answers()) != 0 {
		t.Error("an empty round has nothing")
	}
}

func TestQuestionRoundViewNeverExceedsItsWidth(t *testing.T) {
	q := roundQuestion("q-001", "A very long question\nspanning lines and 漢字漢字漢字漢字漢字漢字 😀😀😀 words words words words", true)
	q.Options = []plan.Option{
		{ID: "opt-1", Label: "line one\nline two " + strings.Repeat("long ", 40), Description: strings.Repeat("d", 100)},
		{ID: "opt-2", Label: strings.Repeat("漢", 60), Description: "😀 wide"},
	}
	q.Recommended = &plan.Recommendation{OptionIDs: []string{"opt-2"}, Rationale: "because\n" + strings.Repeat("漢字 reasons ", 20)}
	long := roundQuestion("q-002", strings.Repeat("Q", 300), false)
	for _, w := range []int{1, 5, 10, 25, 40, 80} {
		r := newQuestionRound(roundAnswer, []plan.Question{q, long}, []string{"brief\n" + strings.Repeat("漢字 ", 200)}, w)
		checkWidth(t, r.View(), w)
		r, _ = pressRound(r, key(tea.KeySpace), runes("e"), runes(strings.Repeat("note 漢字 😀 ", 30)))
		checkWidth(t, r.View(), w)
		r, _ = pressRound(r, key(tea.KeyEsc), runes("s"), key(tea.KeyDown), key(tea.KeyRight), key(tea.KeySpace))
		checkWidth(t, r.View(), w)
		if w < 26 && r.note.Width() > w {
			t.Errorf("width %d: note box is %d wide", w, r.note.Width())
		}
	}
}

func TestQuestionRoundWidthZeroBeforeSetWidth(t *testing.T) {
	r := questionRound{}
	_ = r.View()
	r, _ = r.Update(key(tea.KeyDown))
	r = newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Q?", false)}, nil, 0)
	_ = r.View()
	r, _ = pressRound(r, runes("e"), runes("hi"), key(tea.KeyEsc))
	_ = r.View()
}
