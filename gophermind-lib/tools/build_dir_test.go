package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTree makes build/x.go (root level) and a/build/x.go (nested).
func buildTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{"build/x.go", "a/build/x.go"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package x\n\nfunc "+strings.ToUpper(p[:1])+"Sym() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFindSymbolBuildOnlyIgnoredAtRoot(t *testing.T) {
	dir := buildTree(t)
	out, err := run(t, FindSymbol(dir), `{"name":"ASym"}`)
	if err != nil || !strings.Contains(out, "a/build/x.go") {
		t.Errorf("nested build symbol not found: %q %v", out, err)
	}
	out, _ = run(t, FindSymbol(dir), `{"name":"BSym"}`)
	if strings.Contains(out, "build/x.go") {
		t.Errorf("root build must be ignored: %q", out)
	}
}

func TestListFilesBuildOnlyIgnoredAtRoot(t *testing.T) {
	dir := buildTree(t)
	for name, tool := range map[string]Tool{"list_files": ListFiles(dir), "glob": ListFilesGlob(dir)} {
		out, err := run(t, tool, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, filepath.Join("a", "build", "x.go")) {
			t.Errorf("%s: nested build missing: %q", name, out)
		}
		if strings.Contains(out, "\nbuild/x.go") || strings.HasPrefix(out, "build/x.go") {
			t.Errorf("%s: root build must be excluded: %q", name, out)
		}
	}
}
