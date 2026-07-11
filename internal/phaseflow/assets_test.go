package phaseflow

import (
	"strings"
	"testing"
)

func TestEmbeddedCommandsPresent(t *testing.T) {
	names := CommandNames()
	if len(names) < 40 {
		t.Fatalf("expected many embedded commands, got %d", len(names))
	}
	// Core loop commands must be present.
	for _, want := range []string{"new-project", "execute-phase", "verify-work", "complete-milestone"} {
		if _, ok := Command(want); !ok {
			t.Errorf("missing embedded command %q", want)
		}
	}
}

func TestCommandPrefixTolerant(t *testing.T) {
	a, ok := Command("phase:execute-phase")
	if !ok {
		t.Fatal("phase: prefix should resolve")
	}
	if a.Name != "execute-phase" || a.Kind != "command" {
		t.Errorf("unexpected asset: %+v", a)
	}
	if a.Description == "" {
		t.Error("expected a description from frontmatter")
	}
	if strings.HasPrefix(a.Body, "---") {
		t.Error("frontmatter should be stripped from body")
	}
}

func TestAgentPromptLookup(t *testing.T) {
	// Both bare and prefixed forms should resolve to the same agent.
	a, ok := AgentPrompt("executor")
	if !ok {
		t.Fatal("agent 'executor' should resolve to phase-executor")
	}
	b, ok := AgentPrompt("phase-executor")
	if !ok || b.Name != a.Name {
		t.Error("prefixed and bare agent lookups should match")
	}
}

func TestTemplateLookup(t *testing.T) {
	if _, ok := Template("roadmap"); !ok {
		t.Error("roadmap template should be embedded")
	}
}
