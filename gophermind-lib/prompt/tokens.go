package prompt

import (
	"fmt"
	"strings"
)

// SectionCost is the estimated token cost of one rendered prompt section.
type SectionCost struct {
	Name   string
	Tokens int
}

// estimateTokens is a lightweight byte-to-token heuristic (~4 bytes/token),
// matching the estimator the LLM client uses for budgeting. It overestimates
// slightly, which is the safe direction for a budget report.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// Accounting returns the estimated per-section token cost of the built prompt,
// in build order, plus the total — so callers can see what is consuming the
// context window. Section cost includes the wrapping <tag> overhead.
func (b *Builder) Accounting() ([]SectionCost, int) {
	sections := OrderedSections(b.template, b.context)
	costs := make([]SectionCost, 0, len(sections))
	total := 0
	for _, s := range sections {
		wrapped := fmt.Sprintf("<%s>\n%s\n</%s>", s.Name, s.Content, s.Name)
		n := estimateTokens(wrapped)
		costs = append(costs, SectionCost{Name: s.Name, Tokens: n})
		total += n
	}
	return costs, total
}

// RenderAccounting formats the per-section token accounting as an aligned table.
func (b *Builder) RenderAccounting() string {
	costs, total := b.Accounting()
	width := len("total")
	for _, c := range costs {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-*s  tokens (est.)\n", width, "section")
	for _, c := range costs {
		fmt.Fprintf(&sb, "%-*s  %d\n", width, c.Name, c.Tokens)
	}
	fmt.Fprintf(&sb, "%-*s  %d\n", width, "total", total)
	return sb.String()
}
