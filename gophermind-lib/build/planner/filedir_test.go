package planner

import "testing"

func TestMoveFilesIntoPackageDir(t *testing.T) {
	doc := map[string]any{
		"module": "example.com/slugify",
		"functions": []any{
			map[string]any{"id": "fn-make", "package": "internal/slug", "file": "slug/make.go"},
			map[string]any{"id": "fn-run", "package": "internal/cli", "file": "internal/cli/run.go"},
			map[string]any{"id": "fn-full", "package": "example.com/slugify/internal/cli", "file": "internal/cli/x.go"},
			map[string]any{"id": "fn-root", "package": "", "file": "root.go"},
			map[string]any{"id": "fn-main", "package": "main", "file": "cmd/slugify/main.go"},
		},
		"types": []any{
			map[string]any{"id": "t-opts", "package": "internal/cli", "file": "cli/types.go"},
		},
	}
	ids := moveFilesIntoPackageDir(doc)
	if len(ids) != 2 || ids[0] != "fn-make" || ids[1] != "t-opts" {
		t.Fatalf("moved %v, want [fn-make t-opts]", ids)
	}
	fns := objects(doc["functions"])
	want := []string{"internal/slug/make.go", "internal/cli/run.go", "internal/cli/x.go", "root.go", "cmd/slugify/main.go"}
	for i, f := range fns {
		if f["file"] != want[i] {
			t.Errorf("%s file = %v, want %s", f["id"], f["file"], want[i])
		}
	}
	if f := objects(doc["types"])[0]["file"]; f != "internal/cli/types.go" {
		t.Errorf("type file = %v", f)
	}
	if again := moveFilesIntoPackageDir(doc); len(again) != 0 {
		t.Errorf("not idempotent: %v", again)
	}
}
