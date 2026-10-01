package db_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

func TestOpenCreatesSchemaAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "bb.db")
	for i := 0; i < 2; i++ {
		d, err := db.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"rows", "events", "calls"} {
			var n int
			if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
				t.Fatalf("open %d: table %s missing (n=%d err=%v)", i, table, n, err)
			}
		}
		var v int
		if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 1 {
			t.Fatalf("user_version = %d, %v", v, err)
		}
		d.Close()
	}
}

func TestPragmas(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var mode string
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v", mode, err)
	}
	var timeout int
	if err := d.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
		t.Errorf("busy_timeout = %d, %v", timeout, err)
	}
}

func TestTimestampsSortAsText(t *testing.T) {
	a := time.Date(2026, 9, 29, 12, 0, 5, 500_000_000, time.UTC)
	b := time.Date(2026, 9, 29, 12, 0, 5, 123_000_000, time.UTC) // earlier, but more digits than a
	c := time.Date(2026, 9, 29, 12, 0, 6, 0, time.UTC)
	if !(db.TS(b) < db.TS(a) && db.TS(a) < db.TS(c)) {
		t.Errorf("timestamps do not sort as text: %s %s %s", db.TS(b), db.TS(a), db.TS(c))
	}
	back, err := db.ParseTS(db.TS(a))
	if err != nil || !back.Equal(a) {
		t.Errorf("round trip: %v %v", back, err)
	}
	if z, err := db.ParseTS(""); err != nil || !z.IsZero() {
		t.Errorf("empty string should parse to the zero time, got %v %v", z, err)
	}
}

func TestDefaultPathUsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	p, err := db.DefaultPath()
	if err != nil || p != filepath.Join(dir, "blackboard.db") {
		t.Errorf("DefaultPath = %q, %v", p, err)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return fi.Mode().Perm()
}

// The database holds run state and the model-call ledger: owner only, including
// the write-ahead log and shared-memory files SQLite creates beside it, in a
// config folder that is itself world-readable.
func TestDatabaseFilesAreOwnerOnly(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	path, err := db.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < 20; i++ {
		if _, err := d.Exec(`INSERT INTO events (run_id, node_id, kind, at) VALUES ('r','n','k',?)`, db.TS(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if m := modeOf(t, path+suffix); m != 0o600 {
			t.Errorf("blackboard.db%s mode = %o, want 600", suffix, m)
		}
	}
	// a second connection set (another process) and a reopen keep the modes
	d2, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if m := modeOf(t, path+suffix); m != 0o600 {
			t.Errorf("after reopen, blackboard.db%s mode = %o, want 600", suffix, m)
		}
	}
}

func TestOpenTightensAnExistingLooseDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bb.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	for _, s := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(path+s, mustRead(t, path+s), 0o644); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		_ = os.Chmod(path+s, 0o644)
	}
	d, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, s := range []string{"", "-wal", "-shm"} {
		if m := modeOf(t, path+s); m != 0o600 {
			t.Errorf("%q mode = %o, want 600", s, m)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return b
}
