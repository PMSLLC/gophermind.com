package agent

import (
	"strings"
	"testing"
)

func TestLoadHistoryThenExportRoundTrips(t *testing.T) {
	a := New(nil, nil, 1, nil, nil)
	jsonl := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"hi"}` + "\n" +
		`{"role":"assistant","content":"hello"}` + "\n"
	if err := a.LoadHistory(strings.NewReader(jsonl)); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	var b strings.Builder
	if err := a.ExportJSONL(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`"content":"sys"`, `"content":"hi"`, `"content":"hello"`} {
		if !strings.Contains(out, want) {
			t.Errorf("round-trip lost %q; got:\n%s", want, out)
		}
	}
	if lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; lines != 3 {
		t.Errorf("want 3 messages after load, got %d lines", lines)
	}
}

func TestLoadHistoryEmptyErrors(t *testing.T) {
	a := New(nil, nil, 1, nil, nil)
	if err := a.LoadHistory(strings.NewReader("  \n")); err == nil {
		t.Error("want error loading an empty session history")
	}
}

func TestAppendSystemPrompt(t *testing.T) {
	a := New(nil, nil, 1, nil, nil)
	a.AppendSystemPrompt("EXTRA INSTRUCTIONS")
	var b strings.Builder
	if err := a.ExportJSONL(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "EXTRA INSTRUCTIONS") {
		t.Errorf("system prompt not appended:\n%s", b.String())
	}
	// empty is a no-op (no panic, still one system message)
	a.AppendSystemPrompt("")
}
