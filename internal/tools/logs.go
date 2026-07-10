package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gophermind/internal/safety"
)

// logLevelRe matches a severity token as a whole word, case-insensitively.
var logLevelRe = regexp.MustCompile(`(?i)\b(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL|PANIC)\b`)

// defaultLogSamples is how many error/fatal sample lines the analyzer prints.
const defaultLogSamples = 5

// AnalyzeLog returns a read-only tool that summarizes a log file: total lines,
// a per-severity count, and a few sample error/fatal lines — so the model can
// triage a log without the whole file entering context.
func AnalyzeLog(root string) Tool {
	return Tool{
		Name:        "analyze_log",
		Description: "Summarize a log file: total lines, counts per severity (INFO/WARN/ERROR/...), and sample error lines. Read-only.",
		Schema: object(map[string]any{
			"path":        str("Path to the log file, relative to the repo root."),
			"max_samples": map[string]any{"type": "integer", "description": "Max error/fatal sample lines to show (default 5)."},
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path       string `json:"path"`
				MaxSamples int    `json:"max_samples"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			f, err := os.Open(full)
			if err != nil {
				return "", fmt.Errorf("open %s: %w", a.Path, err)
			}
			defer f.Close()

			maxSamples := a.MaxSamples
			if maxSamples <= 0 {
				maxSamples = defaultLogSamples
			}

			counts := map[string]int{}
			var total int
			var samples []string

			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
			for sc.Scan() {
				line := sc.Text()
				total++
				level := "NONE"
				if m := logLevelRe.FindString(line); m != "" {
					level = strings.ToUpper(m)
					if level == "WARNING" {
						level = "WARN"
					}
				}
				counts[level]++
				if (level == "ERROR" || level == "FATAL" || level == "PANIC") && len(samples) < maxSamples {
					samples = append(samples, strings.TrimSpace(line))
				}
			}
			if err := sc.Err(); err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "lines: %d\n", total)
			b.WriteString("by level:\n")
			for _, lvl := range []string{"FATAL", "PANIC", "ERROR", "WARN", "INFO", "DEBUG", "TRACE", "NONE"} {
				if n := counts[lvl]; n > 0 {
					fmt.Fprintf(&b, "  %-6s %d\n", lvl, n)
				}
			}
			if len(samples) > 0 {
				b.WriteString("error samples:\n")
				for _, s := range samples {
					b.WriteString("  " + s + "\n")
				}
			}
			return truncate(b.String()), nil
		},
	}
}
