package safety

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink inside the repo that points outside it must not be a path to
// escape the root, even though the lexical path is "contained".
func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// repo/evil -> outside
	if err := os.Symlink(outside, filepath.Join(root, "evil")); err != nil {
		t.Fatal(err)
	}

	// Reading through the symlink (existing target file) must be rejected.
	if _, err := SafeJoin(root, "evil/secret"); err == nil {
		t.Error("SafeJoin(evil/secret) should be rejected: escapes root via symlink")
	}
	// Writing a not-yet-existing file through the symlink must also be rejected.
	if _, err := SafeJoin(root, "evil/newfile"); err == nil {
		t.Error("SafeJoin(evil/newfile) should be rejected: escapes root via symlink")
	}
}

// A symlink that stays inside the repo is fine, and ordinary contained paths
// (existing or not) still resolve.
func TestSafeJoinAllowsContained(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// repo/link -> repo/sub  (stays inside root)
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"sub/ok.go", "link/inside.txt"} {
		if _, err := SafeJoin(root, rel); err != nil {
			t.Errorf("SafeJoin(%q) should be allowed, got %v", rel, err)
		}
	}
}

func TestCheckCommandWhitespaceBypass(t *testing.T) {
	blocked := []string{
		"rm -rf /",
		"rm  -rf /",         // extra space
		"rm\t-rf /tmp/x",    // tab
		"rm -fr /tmp/x",     // reordered flags
		"rm -r -f build",    // split flags
		"sudo\trm x",        // tab after sudo
		"echo x >/etc/cron", // no-space redirect to root
		"echo x > /etc/y",   // spaced redirect to root
		"dd if=/dev/zero of=/dev/sda",
	}
	for _, c := range blocked {
		if err := CheckCommand(c); err == nil {
			t.Errorf("CheckCommand(%q) should be blocked", c)
		}
	}

	allowed := []string{
		"go test ./...",
		"git status",
		"rm tmpfile", // non-recursive single file
		"grep -rn foo .",
	}
	for _, c := range allowed {
		if err := CheckCommand(c); err != nil {
			t.Errorf("CheckCommand(%q) should be allowed, got %v", c, err)
		}
	}
}
