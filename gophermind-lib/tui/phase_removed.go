package tui

// PhaseRemovedIn is the release that removed `gophermind phase` and /phase,
// and the v1 planner commands. The stubs that print these notices exist for
// that one release; a later change deletes them.
const PhaseRemovedIn = "0.10.0"

// PhaseRemovedNotice is what `gophermind phase ...` (exit code 2) and the /phase
// slash command print. One place, so the CLI and the TUI cannot drift apart.
func PhaseRemovedNotice() string {
	return "gophermind phase was removed in " + PhaseRemovedIn + ". " +
		"Plan and build a brief with \"gophermind brief plan\" and \"gophermind brief run\", or with /project. " +
		"Existing .planning/ folders are not read."
}

// retiredCommands are the v1 planner slash commands removed in PhaseRemovedIn.
var retiredCommands = map[string]bool{
	"/plan-v1":         true,
	"/plan-v1-execute": true,
	"/questions":       true,
	"/project-execute": true,
}

// RetiredCommandNotice is the one-line notice printed when a removed v1
// planner command is typed. name is the command as typed, e.g. "/questions".
func RetiredCommandNotice(name string) string {
	return name + " was removed in " + PhaseRemovedIn + "; use /project <brief>"
}
