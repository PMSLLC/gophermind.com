package session

import (
	"strings"
	"testing"
)

func TestDiffSummarizesActivity(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s",
		`{"role":"system","content":"sys"}`+"\n"+
			`{"role":"user","content":"do it"}`+"\n"+
			`{"role":"assistant","content":"","tool_calls":[{"id":"1","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.go\",\"content\":\"x\"}"}}]}`+"\n"+
			`{"role":"tool","tool_call_id":"1","name":"write_file","content":"wrote"}`+"\n"+
			`{"role":"assistant","content":"","tool_calls":[{"id":"2","type":"function","function":{"name":"run_shell","arguments":"{\"command\":\"go test ./...\"}"}}]}`+"\n"+
			`{"role":"tool","tool_call_id":"2","name":"run_shell","content":"ok"}`+"\n"+
			`{"role":"assistant","content":"done"}`+"\n")

	out, err := diffIn(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "write_file") || !strings.Contains(out, "a.go") {
		t.Errorf("diff missing file change:\n%s", out)
	}
	if !strings.Contains(out, "go test ./...") {
		t.Errorf("diff missing shell command:\n%s", out)
	}
	// Message counts by role.
	if !strings.Contains(out, "assistant") || !strings.Contains(out, "user") {
		t.Errorf("diff missing role counts:\n%s", out)
	}
}

func TestDiffNoMutations(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "ro",
		`{"role":"system","content":"s"}`+"\n"+
			`{"role":"user","content":"just read"}`+"\n"+
			`{"role":"assistant","content":"here"}`+"\n")
	out, err := diffIn(dir, "ro")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no file changes") {
		t.Errorf("expected a no-changes note:\n%s", out)
	}
}

func TestDiffMissingSession(t *testing.T) {
	if _, err := diffIn(t.TempDir(), "nope"); err == nil {
		t.Error("missing session should error")
	}
}
