package tools

import (
	"strings"
	"testing"
)

func TestScratchpadAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	tool := Scratchpad(dir)

	// empty read
	out, err := run(t, tool, `{"action":"read"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("fresh scratchpad should read empty: %q", out)
	}

	// append twice
	if _, err := run(t, tool, `{"action":"append","text":"first note"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"action":"append","text":"second note"}`); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, tool, `{"action":"read"}`)
	if !strings.Contains(out, "first note") || !strings.Contains(out, "second note") {
		t.Errorf("notes not persisted: %q", out)
	}
}

func TestScratchpadClear(t *testing.T) {
	dir := t.TempDir()
	tool := Scratchpad(dir)
	run(t, tool, `{"action":"append","text":"temp"}`)
	if _, err := run(t, tool, `{"action":"clear"}`); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, tool, `{"action":"read"}`)
	if strings.Contains(out, "temp") {
		t.Errorf("clear did not empty the scratchpad: %q", out)
	}
}

func TestScratchpadRejectsBadAction(t *testing.T) {
	if _, err := run(t, Scratchpad(t.TempDir()), `{"action":"delete"}`); err == nil {
		t.Error("unknown action should error")
	}
}

func TestScratchpadAppendRequiresText(t *testing.T) {
	if _, err := run(t, Scratchpad(t.TempDir()), `{"action":"append"}`); err == nil {
		t.Error("append without text should error")
	}
}
