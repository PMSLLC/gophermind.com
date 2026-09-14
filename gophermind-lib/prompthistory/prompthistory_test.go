package prompthistory

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", tmp)
	s, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func TestRoundTripOrder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", tmp)

	s, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prompts := []string{"first prompt", "second prompt", "third prompt"}
	for _, p := range prompts {
		if err := s.Append(p); err != nil {
			t.Fatalf("Append(%q) error = %v", p, err)
		}
	}
	if got := s.All(); !equalSlices(got, prompts) {
		t.Fatalf("All() before reload = %v, want %v", got, prompts)
	}

	// Reload via a fresh Store to prove persistence.
	s2, err := New()
	if err != nil {
		t.Fatalf("New() (reload) error = %v", err)
	}
	if got := s2.All(); !equalSlices(got, prompts) {
		t.Fatalf("All() after reload = %v, want %v", got, prompts)
	}
}

func TestConsecutiveDupCollapsed(t *testing.T) {
	s := newTestStore(t)

	mustAppend(t, s, "alpha")
	mustAppend(t, s, "alpha") // consecutive dup, should be skipped
	mustAppend(t, s, "beta")
	mustAppend(t, s, "alpha") // non-consecutive repeat, should be kept
	mustAppend(t, s, "alpha") // consecutive dup again, should be skipped

	want := []string{"alpha", "beta", "alpha"}
	if got := s.All(); !equalSlices(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}

	// Verify persisted state matches too.
	s2, err := New()
	if err != nil {
		t.Fatalf("New() (reload) error = %v", err)
	}
	if got := s2.All(); !equalSlices(got, want) {
		t.Fatalf("All() after reload = %v, want %v", got, want)
	}
}

func TestCapDropsOldest(t *testing.T) {
	s := newTestStore(t)

	total := maxEntries + 50
	for i := 0; i < total; i++ {
		// Use distinct, non-consecutive-duplicate values.
		mustAppend(t, s, "prompt-"+strconv.Itoa(i))
	}

	got := s.All()
	if len(got) != maxEntries {
		t.Fatalf("All() len = %d, want %d", len(got), maxEntries)
	}
	// Oldest 50 (prompt-0 .. prompt-49) should have been dropped.
	wantFirst := "prompt-" + strconv.Itoa(total-maxEntries)
	wantLast := "prompt-" + strconv.Itoa(total-1)
	if got[0] != wantFirst {
		t.Fatalf("All()[0] = %q, want %q", got[0], wantFirst)
	}
	if got[len(got)-1] != wantLast {
		t.Fatalf("All()[last] = %q, want %q", got[len(got)-1], wantLast)
	}

	// Cap must also hold after reload (persisted trimmed).
	s2, err := New()
	if err != nil {
		t.Fatalf("New() (reload) error = %v", err)
	}
	got2 := s2.All()
	if len(got2) != maxEntries {
		t.Fatalf("reload All() len = %d, want %d", len(got2), maxEntries)
	}
	if !equalSlices(got, got2) {
		t.Fatalf("reload All() = %v, want %v", got2, got)
	}
}

func TestMultiLinePromptRoundTrips(t *testing.T) {
	s := newTestStore(t)

	multi := "line one\nline two\nline three"
	if err := s.Append(multi); err != nil {
		t.Fatalf("Append(multiline) error = %v", err)
	}
	if err := s.Append("unrelated single line"); err != nil {
		t.Fatalf("Append error = %v", err)
	}

	s2, err := New()
	if err != nil {
		t.Fatalf("New() (reload) error = %v", err)
	}
	got := s2.All()
	want := []string{multi, "unrelated single line"}
	if !equalSlices(got, want) {
		t.Fatalf("All() after reload = %#v, want %#v", got, want)
	}

	// Confirm the file is really JSONL (one JSON-encoded string per line),
	// not raw newline-per-entry, by checking the line count on disk.
	path, err := historyFilePath()
	if err != nil {
		t.Fatalf("historyFilePath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	lines := splitLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("on-disk line count = %d, want 2 (JSONL, one entry per line); raw content:\n%s", len(lines), data)
	}
}

func TestDisabledModeNoFileNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", tmp)
	t.Setenv("GOPHERMIND_HISTORY", "off")

	s, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Append("should not persist"); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if got := s.All(); len(got) != 0 {
		t.Fatalf("All() = %v, want empty", got)
	}

	path, err := historyFilePath()
	if err != nil {
		t.Fatalf("historyFilePath() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no history file at %s when disabled, stat err = %v", path, err)
	}
}

func TestMalformedLineSkippedWithoutFailingLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", tmp)

	path := filepath.Join(tmp, "history")
	content := "\"good one\"\nnot valid json\n\n\"good two\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	s, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := []string{"good one", "good two"}
	if got := s.All(); !equalSlices(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
}

func mustAppend(t *testing.T, s *Store, prompt string) {
	t.Helper()
	if err := s.Append(prompt); err != nil {
		t.Fatalf("Append(%q) error = %v", prompt, err)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func splitLines(s string) []string {
	var lines []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
