package session

import (
	"strings"
	"testing"

	"gophermind/internal/agent"
)

func TestValidIDRejectsTraversalAndEmpties(t *testing.T) {
	bad := []string{"", ".", "..", "../evil", "a/b", "a\\b", "a b", "évil"}
	for _, id := range bad {
		if _, err := Path(id); err == nil {
			t.Errorf("Path(%q) should be rejected", id)
		}
	}
	for _, id := range []string{"abc", "sess-123", "a.b_c-D9"} {
		if _, err := Path(id); err != nil {
			t.Errorf("Path(%q) should be valid, got %v", id, err)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())

	// Seed an agent with a known conversation and save it.
	a := agent.New(nil, nil, 1, nil, nil)
	jsonl := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"remember 42"}` + "\n" +
		`{"role":"assistant","content":"ok, 42"}` + "\n"
	if err := a.LoadHistory(strings.NewReader(jsonl)); err != nil {
		t.Fatal(err)
	}
	const id = "sess-abc"
	if Exists(id) {
		t.Fatal("session should not exist yet")
	}
	if err := Save(id, a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists(id) {
		t.Fatal("session should exist after Save")
	}

	// Load into a fresh agent and confirm the history came back.
	b := agent.New(nil, nil, 1, nil, nil)
	if err := Load(id, b); err != nil {
		t.Fatalf("Load: %v", err)
	}
	var sb strings.Builder
	if err := b.ExportJSONL(&sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "remember 42") || !strings.Contains(sb.String(), "ok, 42") {
		t.Errorf("resumed history missing content:\n%s", sb.String())
	}
}

func TestLoadMissingSessionErrors(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	if err := Load("nope", agent.New(nil, nil, 1, nil, nil)); err == nil {
		t.Error("loading a missing session should error")
	}
}
