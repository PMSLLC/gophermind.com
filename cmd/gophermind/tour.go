package main

// welcomeTour returns a short guided intro shown once, right after the first-run
// setup wizard, so a new user knows the handful of commands worth trying first.
func welcomeTour() string {
	return `
🐹 Welcome to GopherMind! A few things to try:

  gophermind                 start an interactive chat session
  gophermind ask "..."       ask a read-only question about this repo
  gophermind run "..."       have it make a change (mutations need approval)
  gophermind doctor          check your endpoint, model, ripgrep, and git
  gophermind sessions        list, resume, branch, and diff saved sessions

Tips: --read-only for safe exploration, --persona reviewer|architect|tester to
tune behavior, and a .gophermind/policy file to set per-tool approvals.
Run 'gophermind -h' for the full list.
`
}
