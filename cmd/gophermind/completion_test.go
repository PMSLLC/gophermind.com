package main

import (
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	out, err := generateCompletion("bash")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"complete", "gophermind", "sessions", "run"} {
		if !strings.Contains(out, want) {
			t.Errorf("bash completion missing %q", want)
		}
	}
}

func TestCompletionZsh(t *testing.T) {
	out, err := generateCompletion("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "#compdef gophermind") {
		t.Errorf("zsh completion missing compdef header:\n%s", out)
	}
	if !strings.Contains(out, "sessions") {
		t.Errorf("zsh completion missing subcommands")
	}
}

func TestCompletionFish(t *testing.T) {
	out, err := generateCompletion("fish")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "complete -c gophermind") {
		t.Errorf("fish completion missing complete -c:\n%s", out)
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	if _, err := generateCompletion("powershell"); err == nil {
		t.Error("unknown shell should error")
	}
}

func TestCompletionIncludesFlags(t *testing.T) {
	// A representative flag should appear in each shell's output (fish strips the
	// leading dashes in its idiomatic `-l think` form, so match the base name).
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out, _ := generateCompletion(sh)
		if !strings.Contains(out, "think") {
			t.Errorf("%s completion missing the think flag", sh)
		}
	}
}
