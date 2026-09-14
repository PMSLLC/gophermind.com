// Package tips provides short, rotating "did you know" hints shown under the
// startup banner to help users discover features progressively.
package tips

import "math/rand"

// all is the set of tips. Each is a single line; keep them short and point at a
// concrete flag or command.
var all = []string{
	"Use `gophermind ask \"…\"` for a read-only question that never modifies files.",
	"`--read-only` denies every mutating tool — safe exploration by construction.",
	"`gophermind doctor` checks your endpoint, model, ripgrep, and git in one shot.",
	"Resume a session with `--resume <id>`; list saved ones via `gophermind sessions`.",
	"`--persona reviewer|architect|tester` tunes the system prompt for the task.",
	"`--think low|medium|high` sends a reasoning-effort hint to capable models.",
	"`--output-format json` makes `run`/`ask` emit a machine-readable result.",
	"Drop a `.gophermind/policy` file to set per-tool approval (always/ask/never).",
	"Set GOPHERMIND_REDACT_TRANSCRIPT=1 to scrub secrets/PII from saved transcripts.",
	"`--no-banner` / `--quiet` give clean output in scripts and CI.",
	"A `.gophermind/prompt.md` file adds repo-specific instructions to the prompt.",
	"`gophermind sessions gc 30` prunes sessions older than 30 days.",
}

// Random returns a random tip.
func Random() string {
	return all[rand.Intn(len(all))]
}
