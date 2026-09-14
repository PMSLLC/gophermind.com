package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fetch validates before it touches the network, so a bad URL never becomes a
// git invocation.
func TestFetchRejectsABadURLWithoutCloning(t *testing.T) {
	for _, bad := range []string{
		"ext::sh -c 'touch /tmp/pwned'",
		"file:///etc",
		"git@github.com:a/b.git",
		"/etc/passwd",
	} {
		if _, err := Fetch(context.Background(), t.TempDir(), bad, ""); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// Removing a source must also switch its skills off. Leaving them enabled
// would mean re-adding the repository later silently reactivated whatever the
// user had turned on before reviewing the new content.
func TestRemoveDropsEnablementToo(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "acme", "pack@abc123456789")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := Settings{
		Sources: []Source{{URL: "https://github.com/acme/pack", SHA: "abc123456789"}},
		Enabled: []string{"acme/pack:tdd", "other/repo:x"},
	}
	got, err := Remove(cache, s, "acme/pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 0 {
		t.Errorf("source survived removal: %+v", got.Sources)
	}
	if len(got.Enabled) != 1 || got.Enabled[0] != "other/repo:x" {
		t.Errorf("enabled = %v, want only the unrelated key", got.Enabled)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("cached content survived removal")
	}
}

// The cache holds reviewed content at a pinned commit, not a working repo.
// A .git directory there would let a later command pull or check out.
func TestFetchLeavesNoGitDirectory(t *testing.T) {
	if os.Getenv("LIVE_FETCH") == "" {
		t.Skip("set LIVE_FETCH=1 to exercise a real clone")
	}
	cache := t.TempDir()
	src, err := Fetch(context.Background(), cache, "https://github.com/blader/humanizer", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := sourceDir(cache, SourceID(src.URL), src.SHA)
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Error(".git survived into the cache")
	}
	if len(src.SHA) < 40 {
		t.Errorf("SHA %q is not a full commit id", src.SHA)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("cloned content missing: %v", err)
	}
	if !strings.Contains(dir, "@") {
		t.Error("cache directory is not pinned by sha")
	}
}

// A URL path beginning ".." produced a source id of "../..", which
// filepath.Join resolved to a directory ABOVE the cache. Fetch calls
// os.RemoveAll and os.Rename on that path, so a source pointed at a host the
// attacker controls (one that serves a clonable repo at that path, which
// GitHub would not but evil.com would) could delete and create outside the
// cache directory.
//
// Found by an automated security review and confirmed before fixing:
// ValidateSourceURL passed the URL, SourceID returned "../..", and the
// destination resolved to /tmp/..@<sha> rather than /tmp/cache/...
func TestSourceIDCannotEscapeTheCacheDirectory(t *testing.T) {
	for _, raw := range []string{
		"https://evil.com/../..",
		"https://evil.com/../../etc/passwd",
		"https://evil.com/./..",
		"https://evil.com/..%2f..",
	} {
		if err := ValidateSourceURL(raw); err == nil {
			t.Errorf("%q passed validation; it derives a traversing source id", raw)
		}
	}
}

// destFor is the second lock: whatever id it is handed, the result is either
// an error or a path inside the cache, never a path outside it.
//
// Note that only some of these actually escape. ".." and "/etc" are cleaned by
// filepath.Join into ordinary names inside the cache, because the "@<sha>"
// suffix makes the final element a literal filename. Asserting that every
// hostile-looking id must ERROR would be asserting the wrong thing; what
// matters is that none of them lands outside.
func TestSourceDirNeverLandsOutsideTheCache(t *testing.T) {
	cache := t.TempDir()
	root, err := filepath.Abs(cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../..", "..", "a/../../..", "/etc", "../../../tmp/x", "acme/pack"} {
		got, err := destFor(cache, id, "abc123def456")
		if err != nil {
			continue // refused outright, which is also fine
		}
		if !strings.HasPrefix(got, root+string(filepath.Separator)) {
			t.Errorf("id %q resolved to %q, outside %q", id, got, root)
		}
	}

	// And the ordinary case still works.
	got, err := destFor(cache, "acme/pack", "abc123def456")
	if err != nil {
		t.Fatalf("a normal id was rejected: %v", err)
	}
	if !strings.Contains(got, "acme") {
		t.Errorf("dest %q does not name the source", got)
	}
}

// The id that actually escaped is refused, not merely cleaned.
func TestSourceDirRefusesTheEscapingID(t *testing.T) {
	if _, err := destFor(t.TempDir(), "../..", "abc123def456"); err == nil {
		t.Error(`id "../.." was accepted; it resolves above the cache`)
	}
}
