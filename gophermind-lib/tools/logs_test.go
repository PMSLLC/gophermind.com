package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeLog(t *testing.T) {
	dir := t.TempDir()
	log := strings.Join([]string{
		"2026-07-10 INFO starting up",
		"2026-07-10 WARN disk almost full",
		"2026-07-10 ERROR failed to connect to db",
		"2026-07-10 INFO retrying",
		"2026-07-10 error: timeout waiting for lock",
		"a plain line with no level",
	}, "\n") + "\n"
	os.WriteFile(filepath.Join(dir, "app.log"), []byte(log), 0o644)

	out, err := run(t, AnalyzeLog(dir), `{"path":"app.log"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lines: 6", "ERROR", "WARN", "INFO"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q; got:\n%s", want, out)
		}
	}
	// Error samples should surface the actual error lines.
	if !strings.Contains(out, "failed to connect to db") || !strings.Contains(out, "timeout waiting for lock") {
		t.Errorf("error samples missing; got:\n%s", out)
	}
}

func TestAnalyzeLogErrorSampleCap(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("ERROR something broke\n")
	}
	os.WriteFile(filepath.Join(dir, "big.log"), []byte(b.String()), 0o644)

	out, err := run(t, AnalyzeLog(dir), `{"path":"big.log","max_samples":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "something broke") > 3 {
		t.Errorf("more than 3 samples shown:\n%s", out)
	}
	if !strings.Contains(out, "ERROR") || !strings.Contains(out, "50") {
		t.Errorf("count summary wrong:\n%s", out)
	}
}

func TestAnalyzeLogMissing(t *testing.T) {
	if _, err := run(t, AnalyzeLog(t.TempDir()), `{"path":"nope.log"}`); err == nil {
		t.Error("missing file should error")
	}
}
