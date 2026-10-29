package planner

import (
	"slices"
	"strings"
	"testing"
)

func TestQualifyCrossPackageTypes(t *testing.T) {
	doc := map[string]any{
		"module": "example.com/slugify",
		"types":  []any{map[string]any{"id": "slug-config", "package": "internal/slug", "decl": "// slugConfig holds settings.\ntype slugConfig struct {\n\tmaxLen int\n}\n"}},
		"functions": []any{
			map[string]any{"id": "fn-slugify-input", "package": "internal/slug", "signature": "func SlugifyInput(r io.Reader, cfg slugConfig) (string, error)", "uses": []any{}},
			map[string]any{"id": "fn-parse-flags", "package": "internal/cli", "signature": "func parseFlags(args []string) (slugConfig, error)", "uses": []any{}},
			map[string]any{"id": "fn-other", "package": "internal/cli", "signature": "func other(s slug.Thing) string", "uses": []any{}},
		},
	}
	got := qualifyCrossPackageTypes(doc)
	if !slices.Equal(got, []string{"fn-parse-flags"}) {
		t.Fatalf("changed %v", got)
	}
	fns := objects(doc["functions"])
	if s := fns[1]["signature"]; s != "func parseFlags(args []string) (slug.SlugConfig, error)" {
		t.Errorf("cli signature = %v", s)
	}
	if s := fns[0]["signature"]; s != "func SlugifyInput(r io.Reader, cfg SlugConfig) (string, error)" {
		t.Errorf("slug signature = %v", s)
	}
	if u := strList(fns[1]["uses"]); !slices.Contains(u, "slug-config") {
		t.Errorf("uses = %v", u)
	}
	decl := objects(doc["types"])[0]["decl"].(string)
	if !strings.Contains(decl, "type SlugConfig struct") || !strings.Contains(decl, "// SlugConfig holds") {
		t.Errorf("decl = %q", decl)
	}
	if again := qualifyCrossPackageTypes(doc); len(again) != 0 {
		t.Errorf("not idempotent: %v", again)
	}
}
