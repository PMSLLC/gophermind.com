package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gophermind/internal/safety"
)

// tsPatterns are the timestamp layouts log_metrics recognizes at line start.
var tsPatterns = []struct {
	re     *regexp.Regexp
	layout string
}{
	{regexp.MustCompile(`^\S*?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))`), time.RFC3339},
	{regexp.MustCompile(`^\S*?(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`), "2006-01-02 15:04:05"},
	{regexp.MustCompile(`^\S*?(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`), "2006/01/02 15:04:05"},
}

// bucketLayouts maps a bucket name to how its key is formatted (and thereby the
// granularity it truncates to).
var bucketLayouts = map[string]string{
	"minute": "2006-01-02 15:04",
	"hour":   "2006-01-02 15:00",
	"day":    "2006-01-02",
}

// LogMetrics returns a read-only tool that parses timestamps from a log file and
// emits time-bucketed counts (total and errors per bucket), so spikes are
// visible rather than only totals. Builds on the severity detection used by
// analyze_log.
func LogMetrics(root string) Tool {
	return Tool{
		Name:        "log_metrics",
		Description: "Parse timestamps from a log file and report time-bucketed counts (total and error lines per bucket). bucket = minute (default), hour, or day. Read-only.",
		Schema: object(map[string]any{
			"path":   str("Path to the log file, relative to the repo root."),
			"bucket": str("Time bucket: minute (default), hour, or day."),
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path   string `json:"path"`
				Bucket string `json:"bucket"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			bucket := a.Bucket
			if bucket == "" {
				bucket = "minute"
			}
			keyLayout, ok := bucketLayouts[bucket]
			if !ok {
				return "", fmt.Errorf("unknown bucket %q (want minute, hour, or day)", bucket)
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

			type counts struct{ total, errors int }
			buckets := map[string]*counts{}
			var order []string
			matched := 0

			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
			for sc.Scan() {
				line := sc.Text()
				ts, ok := parseTimestamp(line)
				if !ok {
					continue
				}
				matched++
				key := ts.UTC().Format(keyLayout)
				c := buckets[key]
				if c == nil {
					c = &counts{}
					buckets[key] = c
					order = append(order, key)
				}
				c.total++
				if lvl := logLevelRe.FindString(line); lvl != "" {
					switch strings.ToUpper(lvl) {
					case "ERROR", "FATAL", "PANIC":
						c.errors++
					}
				}
			}
			if err := sc.Err(); err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}
			if matched == 0 {
				return "", fmt.Errorf("no parseable timestamps found in %s", a.Path)
			}

			sort.Strings(order)
			var b strings.Builder
			fmt.Fprintf(&b, "bucket=%s  (lines with timestamps: %d)\n", bucket, matched)
			b.WriteString("time | total | errors\n")
			for _, key := range order {
				c := buckets[key]
				fmt.Fprintf(&b, "%s | %d | %d\n", key, c.total, c.errors)
			}
			return truncate(b.String()), nil
		},
	}
}

// parseTimestamp extracts and parses the leading timestamp of a log line.
func parseTimestamp(line string) (time.Time, bool) {
	for _, p := range tsPatterns {
		if m := p.re.FindStringSubmatch(line); m != nil {
			if t, err := time.Parse(p.layout, m[1]); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
