package agent

import "gophermind/internal/safety"

// autoCheckpointName is the rolling checkpoint captured before a gated mutation,
// so the conversation can be restored to just before the agent's last change.
const autoCheckpointName = "auto-before-mutation"

// SetAutoCheckpoint enables snapshotting the conversation before each gated
// (mutating) tool call, making an agent's change trivially revertible.
func (a *Agent) SetAutoCheckpoint(on bool) { a.autoCheckpoint = on }

// maybeAutoCheckpoint snapshots the conversation before a gated mutation when
// auto-checkpointing is enabled. It is a no-op for read-only tools.
func (a *Agent) maybeAutoCheckpoint(tool string) {
	if a.autoCheckpoint && safety.Gated(tool) {
		a.Snapshot(autoCheckpointName)
	}
}
