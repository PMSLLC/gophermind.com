package planner

// SetBeforeTestWrite installs a hook run after a reply is parsed and before
// its test file is checked and written, and returns the previous one.
func SetBeforeTestWrite(f func()) func() {
	old := beforeTestWrite
	beforeTestWrite = f
	return old
}

// ParseOutline exposes the outline parser to the external tests.
func ParseOutline(text, briefID string) (map[string]any, []Dependency, error) {
	return parseOutline(text, briefID)
}
