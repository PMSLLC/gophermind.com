package tools

import (
	"strings"
	"testing"
	"time"
)

func TestUlimitPrefix(t *testing.T) {
	empty := ulimitPrefix(ShellLimits{})
	if empty != "" {
		t.Errorf("no limits should produce empty prefix, got %q", empty)
	}

	p := ulimitPrefix(ShellLimits{CPUSeconds: 5, MaxMemoryMB: 512, MaxProcs: 256})
	if !strings.Contains(p, "ulimit -t 5") {
		t.Errorf("missing cpu limit: %q", p)
	}
	if !strings.Contains(p, "ulimit -v 524288") { // 512 MiB in KiB
		t.Errorf("missing memory limit (KiB): %q", p)
	}
	if !strings.Contains(p, "ulimit -u 256") {
		t.Errorf("missing process limit: %q", p)
	}
	if !strings.HasSuffix(p, "; ") {
		t.Errorf("prefix should end with '; ' so the real command follows: %q", p)
	}
}

func TestRunShellWithLimitsStillRuns(t *testing.T) {
	tool := RunShellEnhanced(t.TempDir(), 10*time.Second, ShellLimits{CPUSeconds: 10, MaxMemoryMB: 1024})
	out, err := run(t, tool, `{"command":"echo hello-limited"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-limited") || !strings.Contains(out, "[exit 0]") {
		t.Errorf("limited shell should still run normal commands: %q", out)
	}
}
