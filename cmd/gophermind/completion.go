package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// completionSubcommands are the top-level subcommands offered by tab-completion.
var completionSubcommands = []string{
	"commands", "mcp", "telemetry", "benchmark", "plugins", "bundle", "upgrade", "chat", "resume", "config", "sessions", "doctor", "status", "prompt-tokens",
	"audit", "policy", "persona", "prompts", "usage", "version", "run", "ask", "queue", "serve", "ab", "print", "completion", "autocomplete", "phase", "free",
}

// completionFlags are the user-facing flags offered by tab-completion.
var completionFlags = []string{
	"--think", "--speed", "--no-banner", "--quiet", "--prompt-template",
	"--schema", "--resume", "--fleet", "--verify", "--plan", "--read-only",
}

// hasArg reports whether args contains want.
func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// supportedShells are the shells generateCompletion can emit a script for.
var supportedShells = []string{"bash", "zsh", "fish"}

// detectShell infers which shell to generate a script for from $SHELL, which
// login sets to the user's shell. It deliberately does not guess from anything
// else: emitting the wrong dialect into an rc file is worse than asking, so an
// unrecognized or unset $SHELL is an error naming the supported values.
func detectShell() (string, error) {
	sh := strings.TrimSpace(os.Getenv("SHELL"))
	if sh == "" {
		return "", fmt.Errorf("cannot detect your shell: $SHELL is not set; pass one explicitly (%s)", strings.Join(supportedShells, ", "))
	}
	base := filepath.Base(sh)
	for _, known := range supportedShells {
		if base == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("cannot detect your shell: $SHELL is %q; pass one explicitly (%s)", sh, strings.Join(supportedShells, ", "))
}

// generateCompletion returns a shell-completion script for bash, zsh, or fish.
func generateCompletion(shell string) (string, error) {
	subs := strings.Join(completionSubcommands, " ")
	flags := strings.Join(completionFlags, " ")
	switch shell {
	case "bash":
		return fmt.Sprintf(`# bash completion for gophermind
_gophermind() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    local subcommands="%s"
    local flags="%s"
    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
    else
        COMPREPLY=( $(compgen -W "$subcommands" -- "$cur") )
    fi
}
complete -F _gophermind gophermind
`, subs, flags), nil
	case "zsh":
		return fmt.Sprintf(`#compdef gophermind
# zsh completion for gophermind
_gophermind() {
    local -a subcommands flags
    subcommands=(%s)
    flags=(%s)
    if [[ "$words[$CURRENT]" == -* ]]; then
        compadd -- $flags
    else
        compadd -- $subcommands
    fi
}
# Guarded so the script is safe to source from .zshrc as well as to drop into
# an fpath completion directory: compdef only exists once compinit has run, and
# an unguarded call errors on every shell start when it has not.
(( $+functions[compdef] )) && compdef _gophermind gophermind
`, subs, flags), nil
	case "fish":
		var b strings.Builder
		b.WriteString("# fish completion for gophermind\n")
		for _, s := range completionSubcommands {
			fmt.Fprintf(&b, "complete -c gophermind -n __fish_use_subcommand -a %s\n", s)
		}
		for _, f := range completionFlags {
			fmt.Fprintf(&b, "complete -c gophermind -l %s\n", strings.TrimLeft(f, "-"))
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q: use bash, zsh, or fish", shell)
	}
}
