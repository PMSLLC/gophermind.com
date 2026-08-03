package main

import (
	"strings"
	"testing"
)

// TestDetectShellFromSHELL: `gophermind autocomplete` with no argument has to
// pick the dialect itself, and $SHELL is the only reliable signal.
func TestDetectShellFromSHELL(t *testing.T) {
	cases := []struct{ env, want string }{
		{"/bin/zsh", "zsh"},
		{"/bin/bash", "bash"},
		{"/opt/homebrew/bin/fish", "fish"},
		{"/usr/local/bin/zsh", "zsh"},
	}
	for _, c := range cases {
		t.Setenv("SHELL", c.env)
		got, err := detectShell()
		if err != nil {
			t.Errorf("SHELL=%s: %v", c.env, err)
			continue
		}
		if got != c.want {
			t.Errorf("SHELL=%s -> %q, want %q", c.env, got, c.want)
		}
	}
}

// TestDetectShellRefusesToGuess: emitting the wrong dialect into an rc file is
// worse than failing, so an unknown or unset $SHELL must error and say what to
// pass instead.
func TestDetectShellRefusesToGuess(t *testing.T) {
	for _, env := range []string{"", "/bin/tcsh", "/usr/bin/nu"} {
		t.Setenv("SHELL", env)
		_, err := detectShell()
		if err == nil {
			t.Errorf("SHELL=%q was accepted", env)
			continue
		}
		for _, want := range []string{"bash", "zsh", "fish"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("SHELL=%q: error does not name %s: %v", env, want, err)
			}
		}
	}
}

// TestZshScriptIsSafeToSourceFromRC: appending to ~/.zshrc is the documented
// use, and an unguarded compdef errors on every shell start when compinit has
// not run yet.
func TestZshScriptIsSafeToSourceFromRC(t *testing.T) {
	script, err := generateCompletion("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "$+functions[compdef]") {
		t.Errorf("zsh script calls compdef unguarded:\n%s", script)
	}
}

// TestGeneratedScriptsOfferTheNewCommand keeps tab-completion aware of the
// command that generates it.
func TestGeneratedScriptsOfferTheNewCommand(t *testing.T) {
	for _, shell := range supportedShells {
		script, err := generateCompletion(shell)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		if !strings.Contains(script, "autocomplete") {
			t.Errorf("%s script does not offer `autocomplete`", shell)
		}
	}
}
