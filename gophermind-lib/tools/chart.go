package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// sparkGlyphs are the eight block-elevation runes, low to high.
var sparkGlyphs = []rune("▁▂▃▄▅▆▇█")

// barMaxWidth caps the width of a bar in the bar-chart renderer.
const barMaxWidth = 40

// Chart returns a tool that renders a compact Unicode chart (sparkline or
// horizontal bar chart) from a numeric series, for at-a-glance trends in the
// terminal. It is pure/offline: no filesystem or network access.
func Chart() Tool {
	return Tool{
		Name:        "chart",
		Description: "Render a compact Unicode chart from numbers. type=spark (default) draws a one-line sparkline; type=bar draws a horizontal bar chart with optional labels.",
		Schema: object(map[string]any{
			"values": map[string]any{"type": "array", "description": "The numeric series to chart.", "items": map[string]any{"type": "number"}},
			"type":   str("Chart type: 'spark' (default) or 'bar'."),
			"labels": map[string]any{"type": "array", "description": "Optional per-value labels (bar charts only).", "items": map[string]any{"type": "string"}},
		}, "values"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Values []float64 `json:"values"`
				Type   string    `json:"type"`
				Labels []string  `json:"labels"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if len(a.Values) == 0 {
				return "", fmt.Errorf("values must be non-empty")
			}
			switch a.Type {
			case "bar":
				return barChart(a.Values, a.Labels), nil
			case "", "spark":
				return sparkline(a.Values), nil
			default:
				return "", fmt.Errorf("unknown chart type %q (want spark or bar)", a.Type)
			}
		},
	}
}

// sparkline renders values as a single line of block glyphs.
func sparkline(values []float64) string {
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range values {
		idx := 0
		if span > 0 {
			idx = int((v - min) / span * float64(len(sparkGlyphs)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkGlyphs) {
			idx = len(sparkGlyphs) - 1
		}
		b.WriteRune(sparkGlyphs[idx])
	}
	return b.String()
}

// barChart renders values as labeled horizontal bars scaled to barMaxWidth.
func barChart(values []float64, labels []string) string {
	max := values[0]
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	labelW := 0
	for i := range values {
		if i < len(labels) && len(labels[i]) > labelW {
			labelW = len(labels[i])
		}
	}

	var b strings.Builder
	for i, v := range values {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		width := 0
		if max > 0 && v > 0 {
			width = int(v / max * float64(barMaxWidth))
			if width < 1 {
				width = 1
			}
		}
		fmt.Fprintf(&b, "%-*s %s %g\n", labelW, label, strings.Repeat("█", width), v)
	}
	return b.String()
}
