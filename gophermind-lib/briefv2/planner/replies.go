package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// saveReply keeps the text of each stage reply under _state/replies/ so a bad
// plan can be inspected afterwards. It follows the same setting as the
// executor's attempt artifacts (executor.artifacts, executor.artifact_max_bytes)
// and never fails the call: a write error is told on the notice stream only.
// Prompts are not saved, and planner prompts carry no secret value, so a reply
// has none to echo.
func (p *Planner) saveReply(r *run, stage, text string) {
	cfg := p.d.Settings
	if cfg == nil || cfg.Executor.Artifacts != "on" || text == "" {
		return
	}
	dir := filepath.Join(r.dir, "_state", "replies")
	name := dumpNameRE.ReplaceAllString(stage, "_")
	n := 1
	for {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, n))); os.IsNotExist(err) {
			break
		}
		n++
	}
	if max := cfg.Executor.ArtifactMaxBytes; max > 0 && len(text) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + fmt.Sprintf("\n[truncated: %d bytes cut]\n", len(text)-cut)
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, n)), []byte(text)); err != nil {
		fmt.Fprintf(debugOut, "stage reply not saved: %v\n", err)
	}
}
