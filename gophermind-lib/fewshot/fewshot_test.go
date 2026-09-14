package fewshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectRanksBySimilarity(t *testing.T) {
	bank := []Example{
		{Input: "how do I read a file in go", Output: "use os.ReadFile"},
		{Input: "sort a slice of integers", Output: "use sort.Ints"},
		{Input: "parse json into a struct", Output: "use json.Unmarshal"},
	}
	got := Select(bank, "how do I read a file", 1)
	if len(got) != 1 {
		t.Fatalf("expected 1 example, got %d", len(got))
	}
	if !strings.Contains(got[0].Input, "read a file") {
		t.Errorf("most similar example should be the file one, got %q", got[0].Input)
	}
}

func TestSelectTopK(t *testing.T) {
	bank := []Example{
		{Input: "alpha beta", Output: "1"},
		{Input: "beta gamma", Output: "2"},
		{Input: "gamma delta", Output: "3"},
		{Input: "totally unrelated", Output: "4"},
	}
	got := Select(bank, "beta gamma delta", 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(got))
	}
	// The unrelated example must not be chosen.
	for _, e := range got {
		if e.Output == "4" {
			t.Errorf("unrelated example should not be selected: %+v", got)
		}
	}
}

func TestSelectNoOverlapReturnsNothing(t *testing.T) {
	bank := []Example{{Input: "alpha beta", Output: "1"}}
	if got := Select(bank, "xyz qrs", 3); len(got) != 0 {
		t.Errorf("no term overlap should select nothing, got %+v", got)
	}
}

func TestLoadBank(t *testing.T) {
	dir := t.TempDir()
	content := `[
		{"input": "q1", "output": "a1"},
		{"input": "q2", "output": "a2"}
	]`
	path := filepath.Join(dir, "examples.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	bank, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) != 2 || bank[0].Input != "q1" {
		t.Errorf("unexpected bank: %+v", bank)
	}
}

func TestFormat(t *testing.T) {
	out := Format([]Example{{Input: "q", Output: "a"}})
	if !strings.Contains(out, "q") || !strings.Contains(out, "a") {
		t.Errorf("formatted examples missing content: %q", out)
	}
}
