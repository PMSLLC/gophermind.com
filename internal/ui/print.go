// Package ui renders agent progress and approval prompts to the terminal.
// All progress goes to stderr so the final answer on stdout stays pipeable.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"gophermind/internal/agent"
)

// Printer renders loop events. When Verbose is false only tool calls are shown
// (one line each); when true, assistant prose and tool results are shown too.
type Printer struct {
	Verbose bool
}

// Event implements the agent's event callback.
func (p Printer) Event(e agent.Event) {
	switch e.Type {
	case "assistant":
		if p.Verbose && strings.TrimSpace(e.Text) != "" {
			fmt.Fprintln(os.Stderr, "\n· "+oneLine(e.Text, 0))
		}
	case "tool_call":
		fmt.Fprintf(os.Stderr, "→ %s %s\n", e.Name, oneLine(e.Text, 120))
	case "tool_result":
		if p.Verbose {
			fmt.Fprintf(os.Stderr, "  %s\n", oneLine(e.Text, 200))
		}
	}
}

// Confirm prompts on stderr and reads a y/n answer from r. The caller passes a
// shared *bufio.Reader so the prompt and the main input loop don't both buffer
// stdin independently.
func Confirm(r *bufio.Reader, tool, argsJSON string) bool {
	fmt.Fprintf(os.Stderr, "\nApprove %s %s ? [y/N] ", tool, oneLine(argsJSON, 200))
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// oneLine collapses text to a single line and truncates to max runes (0 = no
// length limit, just collapse).
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max] + "…"
	}
	return s
}
