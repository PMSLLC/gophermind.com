package main

import (
	"fmt"
	"strings"
)

// completionSubcommands are the top-level subcommands offered by tab-completion.
var completionSubcommands = []string{
	"chat", "resume", "config", "sessions", "doctor", "status", "prompt-tokens",
	"audit", "policy", "persona", "version", "run", "ask", "queue", "serve", "ab", "print", "completion",
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
compdef _gophermind gophermind
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
