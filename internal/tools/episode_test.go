package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordEpisode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "episodes.json")
	tool := RecordEpisode(fakeEmbed{}, path)
	if tool.Name != "record_episode" {
		t.Fatalf("name = %q", tool.Name)
	}
	_, err := run(t, tool, `{"task":"fix the parser","outcome":"success","lesson":"the bug was in template.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("episodes not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "fix the parser") || !strings.Contains(s, "template.go") {
		t.Errorf("episode not persisted:\n%s", s)
	}
}

func TestRecordEpisodeNilProvider(t *testing.T) {
	if _, err := run(t, RecordEpisode(nil, "x"), `{"task":"t","outcome":"o"}`); err == nil {
		t.Error("nil provider should error")
	}
}

func TestRecordEpisodeEmptyTask(t *testing.T) {
	if _, err := run(t, RecordEpisode(fakeEmbed{}, filepath.Join(t.TempDir(), "e.json")), `{"task":"  ","outcome":"o"}`); err == nil {
		t.Error("empty task should error")
	}
}
