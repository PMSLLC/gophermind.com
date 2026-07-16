package bubblecomplete

// Candidate is one suggested completion.
type Candidate struct {
	Text    string // text to insert
	Display string // label shown in a menu (defaults to Text)
	Desc    string // optional right-column description in a menu
	Replace int    // runes to delete before the cursor before inserting Text
}

// Provider produces candidates for the current input state.
type Provider interface {
	Name() string
	Suggest(input string, cursor int) []Candidate
}
