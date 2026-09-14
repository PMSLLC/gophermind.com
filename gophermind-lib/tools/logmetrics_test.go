package tools

import (
	"strings"
	"testing"
)

const bucketLog = `2026-01-02T10:00:01Z INFO started
2026-01-02T10:00:30Z ERROR boom
2026-01-02T10:00:45Z ERROR bang
2026-01-02T10:01:05Z INFO ok
2026-01-02T10:01:10Z ERROR oops
2026-01-02T11:00:00Z WARN slow
`

func TestLogMetricsByMinute(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "app.log", bucketLog)
	out, err := run(t, LogMetrics(dir), `{"path":"app.log","bucket":"minute"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Two errors in the 10:00 minute, one in 10:01.
	if !strings.Contains(out, "10:00") || !strings.Contains(out, "10:01") {
		t.Errorf("expected per-minute buckets:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var found bool
	for _, ln := range lines {
		if strings.Contains(ln, "10:00") {
			found = true
			if !strings.Contains(ln, "2") { // 2 errors
				t.Errorf("10:00 minute should show 2 errors:\n%s", ln)
			}
		}
	}
	if !found {
		t.Errorf("no 10:00 bucket line:\n%s", out)
	}
}

func TestLogMetricsByHour(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "app.log", bucketLog)
	out, err := run(t, LogMetrics(dir), `{"path":"app.log","bucket":"hour"}`)
	if err != nil {
		t.Fatal(err)
	}
	// 10:00 hour has 3 errors total; 11:00 hour has 0.
	if !strings.Contains(out, "10:00") || !strings.Contains(out, "11:00") {
		t.Errorf("expected per-hour buckets:\n%s", out)
	}
}

func TestLogMetricsNoTimestamps(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "plain.log", "no timestamps here\njust text\n")
	if _, err := run(t, LogMetrics(dir), `{"path":"plain.log"}`); err == nil {
		t.Error("a log with no parseable timestamps should error")
	}
}
