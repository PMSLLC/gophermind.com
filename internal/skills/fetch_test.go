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
