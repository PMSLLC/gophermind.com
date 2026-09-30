package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain gives the whole test process one GOCACHE and one config directory.
// A cold go build in a fresh module dominates the package's time; with the
// shared cache the standard library and the greeter packages compile once.
// Both directories are made under os.TempDir and removed at exit through
// removeTestDir, which refuses anything else.
func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "gm-exectest-cache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: no cache directory")
		os.Exit(2)
	}
	cfg, err := os.MkdirTemp("", "gm-exectest-cfg-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: no config directory")
		os.Exit(2)
	}
	goCacheOverride = cache
	os.Setenv("GOPHERMIND_CONFIG_DIR", cfg)
	// Go's default -timeout is 10 minutes per package and its panic names
	// whichever test happens to be running. A package that is slow looks like a
	// hang (a full run at 34ba65b took 874s and "hung" in TestStartResumeSeam,
	// which takes 6s alone). Say so before the limit, so a slow package is not
	// mistaken for a stuck test.
	watchdog := time.AfterFunc(8*time.Minute, func() {
		fmt.Fprintln(os.Stderr, "executor tests: 8 minutes elapsed; the package is slow, not necessarily stuck (default -timeout is 10m)")
	})
	code := m.Run()
	watchdog.Stop()
	removeTestDir(cache)
	removeTestDir(cfg)
	os.Exit(code)
}

// removeTestDir deletes a directory TestMain made: its name must start with
// gm-exectest- and it must sit directly in os.TempDir().
func removeTestDir(dir string) {
	if dir == "" || !strings.HasPrefix(filepath.Base(dir), "gm-exectest-") || filepath.Dir(dir) != filepath.Clean(os.TempDir()) {
		return
	}
	makeTreeWritable(dir)
	_ = os.RemoveAll(dir)
}
