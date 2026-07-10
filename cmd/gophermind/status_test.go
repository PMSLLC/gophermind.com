package main

import (
	"strings"
	"testing"
)

func TestPromptLine(t *testing.T) {
	got := promptLine("qwen", "main")
	if !strings.Contains(got, "qwen") || !strings.Contains(got, "main") {
		t.Errorf("prompt line missing model/branch: %q", got)
	}
	if !strings.Contains(got, "🐹") {
		t.Errorf("prompt line missing gopher glyph: %q", got)
	}
}

func TestPromptLineNoBranch(t *testing.T) {
	got := promptLine("qwen", "")
	if strings.Contains(got, "⎇") {
		t.Errorf("no branch should omit the branch glyph: %q", got)
	}
	if !strings.Contains(got, "qwen") {
		t.Errorf("model missing: %q", got)
	}
}

func TestPromptLineAutoModel(t *testing.T) {
	if got := promptLine("", "main"); !strings.Contains(got, "auto") {
		t.Errorf("empty model should render as auto: %q", got)
	}
}
