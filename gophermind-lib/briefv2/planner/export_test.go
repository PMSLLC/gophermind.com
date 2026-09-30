package planner

// SetBeforeTestWrite installs a hook run after a reply is parsed and before
// its test file is checked and written, and returns the previous one.
func SetBeforeTestWrite(f func()) func() {
	old := beforeTestWrite
	beforeTestWrite = f
	return old
}
