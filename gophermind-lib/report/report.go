// Package report renders a self-contained HTML report of an agent run
// (task, answer, optional diff, and token/cost usage) for a shareable record.
package report

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Data is the content of a run report.
type Data struct {
	Task             string
	Answer           string
	Diff             string // optional unified diff of changes made
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	GeneratedAt      time.Time
}

// HTML renders the report as a single self-contained HTML document (inline CSS,
// no external assets). All user content is HTML-escaped.
func HTML(d Data) string {
	ts := d.GeneratedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\">\n")
	b.WriteString("<title>gophermind run report</title>\n")
	b.WriteString("<style>")
	b.WriteString("body{font-family:system-ui,sans-serif;max-width:900px;margin:2rem auto;padding:0 1rem;line-height:1.5;color:#1a1a1a}")
	b.WriteString("h1{font-size:1.4rem}h2{font-size:1.1rem;margin-top:1.5rem;border-bottom:1px solid #ddd;padding-bottom:.25rem}")
	b.WriteString("pre{background:#f5f5f5;padding:.75rem;border-radius:6px;overflow-x:auto;white-space:pre-wrap}")
	b.WriteString(".meta{color:#666;font-size:.85rem}table{border-collapse:collapse}td{padding:.2rem .75rem .2rem 0}")
	b.WriteString("</style></head><body>\n")

	fmt.Fprintf(&b, "<h1>gophermind run report</h1>\n<p class=\"meta\">%s</p>\n", html.EscapeString(ts.Format(time.RFC3339)))

	fmt.Fprintf(&b, "<h2>Task</h2>\n<pre>%s</pre>\n", html.EscapeString(d.Task))
	fmt.Fprintf(&b, "<h2>Answer</h2>\n<pre>%s</pre>\n", html.EscapeString(d.Answer))

	if strings.TrimSpace(d.Diff) != "" {
		fmt.Fprintf(&b, "<h2>Changes</h2>\n<pre>%s</pre>\n", html.EscapeString(d.Diff))
	}

	b.WriteString("<h2>Usage</h2>\n<table>\n")
	fmt.Fprintf(&b, "<tr><td>prompt tokens</td><td>%d</td></tr>\n", d.PromptTokens)
	fmt.Fprintf(&b, "<tr><td>completion tokens</td><td>%d</td></tr>\n", d.CompletionTokens)
	fmt.Fprintf(&b, "<tr><td>total tokens</td><td>%d</td></tr>\n", d.TotalTokens)
	fmt.Fprintf(&b, "<tr><td>estimated cost (USD)</td><td>%g</td></tr>\n", d.CostUSD)
	b.WriteString("</table>\n</body></html>\n")
	return b.String()
}
