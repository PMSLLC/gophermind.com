package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file is the question round: the whole set of questions the planning
// passes asked, shown at once as a list with a cursor, so the owner answers
// them in ONE sitting rather than one modal prompt after another. It is a
// self-contained component: it owns no repository, no agent and no goroutine,
// takes key messages and returns itself, so it can be driven directly from a
// test. questions.go hosts it inside the model.

const (
	// roundListRows is the most questions listed around the cursor at once,
	// so a round of forty questions still fits on a screen.
	roundListRows = 7
	// roundNoteRows is the tallest the free-text box grows before it scrolls
	// its own content, and roundNoteLimit the most text it accepts (the store
	// rejects an answer longer than 2,000 characters).
	roundNoteRows  = 5
	roundNoteLimit = 2000
	// roundWhyBytes bounds the brief excerpt shown behind a question.
	roundWhyBytes = 400
)

// roundMode says what the round is for.
type roundMode int

const (
	// roundAnswer collects answers to open questions.
	roundAnswer roundMode = iota
	// roundChange revisits answered questions so the owner can change their
	// mind. Steps already specified from a changed answer are flagged for
	// re-planning (see plan.ChangeAnswer).
	roundChange
)

// roundItem is one question and the answer being composed for it.
type roundItem struct {
	q       plan.Question
	why     string // brief excerpt behind the question, "" when there is none
	chosen  map[string]bool
	note    string
	skipped bool
	opt     int // the option the cursor is on
}

// answered reports whether this item now carries an answer the store accepts:
// at least one option or some text.
func (it roundItem) answered() bool {
	return len(it.chosen) > 0 || strings.TrimSpace(it.note) != ""
}

// answer renders the item as an answer for the store.
func (it roundItem) answer() plan.Answer {
	a := plan.Answer{Text: strings.TrimSpace(it.note)}
	for _, o := range it.q.Options { // option order, not map order, so it is stable
		if it.chosen[o.ID] {
			a.OptionIDs = append(a.OptionIDs, o.ID)
		}
	}
	return a
}

// roundAnswerOut is one answer the round collected.
type roundAnswerOut struct {
	ID     string
	Answer plan.Answer
	// Change is true when this replaces an answer the question already had,
	// so the caller must use plan.ChangeAnswer rather than AnswerQuestion.
	Change bool
}

// roundResult is what one key did to the round.
type roundResult struct {
	Submitted bool
	Cancelled bool
}

// questionRound is the component. Its zero value is not usable; build it with
// newQuestionRound.
type questionRound struct {
	mode    roundMode
	items   []roundItem
	cur     int
	editing bool
	note    textarea.Model
	width   int
}

// newQuestionRound builds a round over qs. why[i] is the brief excerpt behind
// qs[i] (see plan.ExcerptsFor); a shorter or nil slice simply means some
// questions show no excerpt. In roundChange mode each item starts from the
// answer the question already has, so leaving it alone changes nothing.
func newQuestionRound(mode roundMode, qs []plan.Question, why []string, width int) questionRound {
	ta := textarea.New()
	ta.Placeholder = "anything the options do not cover…"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = roundNoteLimit
	ta.SetHeight(1)
	r := questionRound{mode: mode, note: ta, width: width}
	for i, q := range qs {
		it := roundItem{q: q, chosen: map[string]bool{}}
		if i < len(why) {
			it.why = why[i]
		}
		if mode == roundChange && q.Answer != nil {
			for _, id := range q.Answer.OptionIDs {
				it.chosen[id] = true
			}
			it.note = q.Answer.Text
		}
		r.items = append(r.items, it)
	}
	r.setWidth(width)
	r.loadNote()
	return r
}

func (r *questionRound) setWidth(w int) {
	r.width = w
	inner := w - 6
	if inner < 20 {
		inner = 20
	}
	if w > 0 && inner > w { // a very narrow round never gets a wider note box
		inner = w
	}
	r.note.SetWidth(inner)
}

// loadNote points the text box at the current item's note.
func (r *questionRound) loadNote() {
	if len(r.items) == 0 {
		return
	}
	r.note.SetValue(r.items[r.cur].note)
	r.growNote()
}

// saveNote copies the text box back into the current item.
func (r *questionRound) saveNote() {
	if len(r.items) == 0 {
		return
	}
	r.items[r.cur].note = r.note.Value()
	if strings.TrimSpace(r.items[r.cur].note) != "" {
		r.items[r.cur].skipped = false
	}
}

// growNote resizes the text box to its content, up to roundNoteRows.
func (r *questionRound) growNote() {
	w := r.note.Width()
	if w < 1 {
		w = 1
	}
	rows := 1
	if v := r.note.Value(); v != "" {
		rows = wrappedRows(v, w)
	}
	if rows > roundNoteRows {
		rows = roundNoteRows
	}
	r.note.SetHeight(rows)
}

// count returns how many questions are in the round.
func (r questionRound) count() int { return len(r.items) }

// answeredCount returns how many carry an answer.
func (r questionRound) answeredCount() int {
	n := 0
	for _, it := range r.items {
		if it.answered() {
			n++
		}
	}
	return n
}

// progressCount is what the progress line counts: answers given in a round
// that collects them, changes made in a round that revisits them.
func (r questionRound) progressCount() int {
	if r.mode == roundChange {
		return len(r.answers())
	}
	return r.answeredCount()
}

// canSubmit reports whether the round may be submitted: every question is
// answered or explicitly skipped. Skipping leaves a question open, so an
// owner is never trapped by one they cannot decide. A round that revisits
// answers can always be submitted; leaving everything alone submits nothing.
func (r questionRound) canSubmit() bool {
	if r.mode == roundChange {
		return true
	}
	for _, it := range r.items {
		if !it.answered() && !it.skipped {
			return false
		}
	}
	return true
}

// answers returns what to write to the store: the questions the owner
// answered, skipping the ones they skipped and, in a round that revisits
// answers, the ones they left exactly as they were.
func (r questionRound) answers() []roundAnswerOut {
	var out []roundAnswerOut
	for _, it := range r.items {
		if it.skipped || !it.answered() {
			continue
		}
		a := it.answer()
		change := r.mode == roundChange || it.q.Status == plan.QuestionAnswered
		if change && it.q.Answer != nil && sameRoundAnswer(*it.q.Answer, a) {
			continue
		}
		out = append(out, roundAnswerOut{ID: it.q.ID, Answer: a, Change: change})
	}
	return out
}

// sameRoundAnswer reports whether an answer is the one already stored. Option
// ids are produced in option order on both sides, so a plain comparison is
// enough.
func sameRoundAnswer(stored, now plan.Answer) bool {
	if strings.TrimSpace(stored.Text) != strings.TrimSpace(now.Text) || len(stored.OptionIDs) != len(now.OptionIDs) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range stored.OptionIDs {
		seen[id] = true
	}
	for _, id := range now.OptionIDs {
		if !seen[id] {
			return false
		}
	}
	return true
}

// Update handles one key. Every key belongs to the round while it is showing,
// except the ones the session handles first (Ctrl-C, and Esc, which reaches
// here only to leave the note box). The returned result says whether the
// round is finished.
func (r questionRound) Update(msg tea.KeyMsg) (questionRound, roundResult) {
	if len(r.items) == 0 {
		if msg.Type == tea.KeyEsc {
			return r, roundResult{Cancelled: true}
		}
		return r, roundResult{}
	}
	if r.editing {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyTab:
			r.saveNote()
			r.editing = false
			r.note.Blur()
			return r, roundResult{}
		}
		var cmd tea.Cmd
		r.note, cmd = r.note.Update(msg)
		_ = cmd // the round is driven by the session's own tick, not its own commands
		r.saveNote()
		r.growNote()
		return r, roundResult{}
	}

	switch msg.Type {
	case tea.KeyEsc:
		return r, roundResult{Cancelled: true}
	case tea.KeyUp:
		r.move(-1)
		return r, roundResult{}
	case tea.KeyDown:
		r.move(1)
		return r, roundResult{}
	case tea.KeyLeft:
		r.moveOption(-1)
		return r, roundResult{}
	case tea.KeyRight:
		r.moveOption(1)
		return r, roundResult{}
	case tea.KeyEnter, tea.KeySpace:
		r.toggle()
		return r, roundResult{}
	case tea.KeyCtrlS:
		if r.canSubmit() {
			return r, roundResult{Submitted: true}
		}
		return r, roundResult{}
	}

	switch strings.ToLower(msg.String()) {
	case "e":
		r.editing = true
		r.loadNote()
		r.note.Focus()
		r.note.CursorEnd()
	case "s":
		r.skip()
	case "k":
		r.move(-1)
	case "j":
		r.move(1)
	}
	return r, roundResult{}
}

func (r *questionRound) move(d int) {
	r.cur = (r.cur + d + len(r.items)) % len(r.items)
	r.loadNote()
}

func (r *questionRound) moveOption(d int) {
	it := &r.items[r.cur]
	if len(it.q.Options) == 0 {
		return
	}
	it.opt = (it.opt + d + len(it.q.Options)) % len(it.q.Options)
}

// toggle selects or clears the option under the cursor. A question that takes
// one answer clears the others, which is how a radio group behaves; a
// multi-select question accumulates. A recommendation is only ever a marker,
// so nothing is ever selected until the owner does it here.
func (r *questionRound) toggle() {
	it := &r.items[r.cur]
	if len(it.q.Options) == 0 {
		return
	}
	id := it.q.Options[it.opt].ID
	on := it.chosen[id]
	if !it.q.MultiSelect {
		it.chosen = map[string]bool{}
	}
	if on {
		delete(it.chosen, id)
	} else {
		it.chosen[id] = true
	}
	it.skipped = false
}

// skip leaves the current question unanswered and open. Nothing is written
// for it, so the next round asks it again.
func (r *questionRound) skip() {
	it := &r.items[r.cur]
	it.chosen = map[string]bool{}
	it.note = ""
	it.skipped = true
	r.loadNote()
}

// View renders the round: the progress line, a window of the list around the
// cursor, then the current question in full with its options, the brief
// excerpt behind it and its free-text box.
//
// Every line is cut, with an ellipsis, to the round's width (display columns,
// wide-rune aware); a width of 0 or less means unbounded. Height is never
// cut: the worst case is 23 rows plus one row per option of the current
// question (title 1, window 7 plus 2 markers, blank 1, question 1, why 1,
// brief 1, recommendation 1, note label 1, note box up to roundNoteRows,
// skipped 1, keys 1). A caller budgeting height should reserve that.
func (r questionRound) View() string {
	out := r.render()
	if r.width < 1 {
		return out
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, r.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (r questionRound) render() string {
	if len(r.items) == 0 {
		return roundTitleStyle.Render("No questions.")
	}
	var b strings.Builder
	verb := "answered"
	if r.mode == roundChange {
		verb = "changed"
	}
	b.WriteString(roundTitleStyle.Render(fmt.Sprintf("Questions: %d of %d %s", r.progressCount(), len(r.items), verb)))
	b.WriteString("\n")

	first, last := listWindow(r.cur, len(r.items), roundListRows)
	if first > 0 {
		b.WriteString(fmt.Sprintf("   (%d above)\n", first))
	}
	for i := first; i < last; i++ {
		it := r.items[i]
		mark := " "
		switch {
		case it.skipped:
			mark = "-"
		case it.answered():
			mark = "x"
		}
		line := fmt.Sprintf("  [%s] %d. %s", mark, i+1, oneLine(it.q.Question))
		if i == r.cur {
			line = roundCursorStyle.Render(">" + line[1:])
		}
		b.WriteString(line + "\n")
	}
	if last < len(r.items) {
		b.WriteString(fmt.Sprintf("   (%d below)\n", len(r.items)-last))
	}

	it := r.items[r.cur]
	b.WriteString("\n" + roundQuestionStyle.Render(fmt.Sprintf("Q%d: %s", r.cur+1, oneLine(it.q.Question))) + "\n")
	if why := oneLine(it.q.Why); why != "" {
		b.WriteString("  why: " + why + "\n")
	}
	if it.why != "" {
		b.WriteString("  from the brief: " + oneLine(cutRoundBytes(it.why, roundWhyBytes)) + "\n")
	}
	for i, o := range it.q.Options {
		box := " "
		if it.chosen[o.ID] {
			box = "x"
		}
		line := fmt.Sprintf("  [%s] %s", box, oneLine(o.Label))
		if o.Description != "" {
			line += " - " + oneLine(o.Description)
		}
		if recommends(it.q, o.ID) {
			line += " (recommended)"
		}
		if i == it.opt {
			line = roundCursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(it.q.Options) == 0 {
		b.WriteString("  (no options: answer in your own words)\n")
	}
	if it.q.Recommended != nil && strings.TrimSpace(it.q.Recommended.Rationale) != "" {
		b.WriteString("  recommended because: " + oneLine(it.q.Recommended.Rationale) + "\n")
	}
	b.WriteString("  note: ")
	if r.editing {
		b.WriteString("\n" + r.note.View() + "\n")
	} else if n := oneLine(it.note); n != "" {
		b.WriteString(n + "\n")
	} else {
		b.WriteString("(none)\n")
	}
	if it.skipped {
		b.WriteString("  skipped: this question stays open\n")
	}
	b.WriteString(roundKeysStyle.Render(r.keyHelp()))
	return b.String()
}

func (r questionRound) keyHelp() string {
	if r.editing {
		return "typing a note · esc when done"
	}
	s := "up/down question · left/right option · space choose · e note · s skip · esc cancel"
	if r.canSubmit() {
		return s + " · ctrl+s submit"
	}
	return s + " · ctrl+s submit (answer or skip them all first)"
}

// recommends reports whether the question recommends this option. A
// recommendation is shown, never chosen.
func recommends(q plan.Question, optionID string) bool {
	if q.Recommended == nil {
		return false
	}
	for _, id := range q.Recommended.OptionIDs {
		if id == optionID {
			return true
		}
	}
	return false
}

// listWindow returns the half-open range of list rows to show so the cursor
// is inside it.
func listWindow(cur, n, rows int) (int, int) {
	if n <= rows {
		return 0, n
	}
	first := cur - rows/2
	if first < 0 {
		first = 0
	}
	if first+rows > n {
		first = n - rows
	}
	return first, first + rows
}

// cutRoundBytes shortens s to at most max bytes without splitting a rune.
func cutRoundBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

var (
	roundTitleStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"})
	roundQuestionStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#5AA6BC"})
	roundCursorStyle = lipgloss.NewStyle().Bold(true)
	roundKeysStyle   = lipgloss.NewStyle().Faint(true)
	roundDialogStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}).
				Padding(0, 1)
)
