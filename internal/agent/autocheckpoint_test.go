package agent

import (
	"testing"

	"gophermind/internal/llm"
)

func TestAutoCheckpointBeforeGatedMutation(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	a.SetAutoCheckpoint(true)
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: "hello"}) // some state

	// A gated (mutating) tool triggers an auto-checkpoint.
	a.maybeAutoCheckpoint("write_file")
	if _, ok := a.checkpoints.Get(autoCheckpointName); !ok {
		t.Error("a gated mutation should create the auto checkpoint")
	}
}

func TestAutoCheckpointSkipsReadOnly(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	a.SetAutoCheckpoint(true)
	a.maybeAutoCheckpoint("read_file")
	if _, ok := a.checkpoints.Get(autoCheckpointName); ok {
		t.Error("a read-only tool should not create the auto checkpoint")
	}
}

func TestAutoCheckpointDisabled(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	// Not enabled.
	a.maybeAutoCheckpoint("write_file")
	if _, ok := a.checkpoints.Get(autoCheckpointName); ok {
		t.Error("disabled auto-checkpoint should not snapshot")
	}
}
