package tui

import (
	"os"
	"testing"
	"time"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
)

// withWorkdir runs fn with the process working directory temporarily set to dir.
func withWorkdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	fn()
}

// stubAgent builds an agent pointed at a dead endpoint; its callers never
// let it make a call, they just need m.agent to be non-nil.
func stubAgent(t *testing.T) *agent.Agent {
	t.Helper()
	client := llm.New("http://127.0.0.1:1", "", "m", time.Second, false)
	return agent.New(client, tools.NewRegistry(), 5, safety.Auto, nil)
}
