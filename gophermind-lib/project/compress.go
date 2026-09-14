package project

import "strings"

// CompressContext reduces text to fit roughly maxTokens by keeping the most
// informative lines whole (headings, list items, and other non-blank lines, in
// order, de-duplicated) rather than hard-truncating mid-line like CapContext.
// This preserves meaning while dropping bytes. A non-positive budget or already-
// fitting text is returned unchanged.
func CompressContext(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return text
	}
	maxBytes := maxTokens * bytesPerToken
	if len(text) <= maxBytes {
		return text
	}

	// Rank lines: headings/list markers first, then other non-blank lines, in
	// original order; blank lines and duplicates are dropped.
	type line struct {
		text string
		pri  int
		idx  int
	}
	seen := map[string]bool{}
	var lines []line
	for i, raw := range strings.Split(text, "\n") {
		t := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(t)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		pri := 1
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			pri = 0 // keep structure first
		}
		lines = append(lines, line{t, pri, i})
	}
	// Select by priority, keeping original order among selected.
	selected := map[int]string{}
	used := 0
	for pass := 0; pass <= 1; pass++ {
		for _, l := range lines {
			if l.pri != pass {
				continue
			}
			cost := len(l.text) + 1
			if used+cost > maxBytes {
				continue
			}
			selected[l.idx] = l.text
			used += cost
		}
	}
	order := make([]int, 0, len(selected))
	for idx := range selected {
		order = append(order, idx)
	}
	// idx already reflects original order; sort ascending.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && order[j] < order[j-1]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var out []string
	for _, idx := range order {
		out = append(out, selected[idx])
	}
	return strings.Join(out, "\n") + "\n… [context compressed to fit the token budget]"
}
