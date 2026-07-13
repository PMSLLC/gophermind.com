package phaseflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedAndLoadCatalog(t *testing.T) {
	root := t.TempDir()
	n, err := SeedCatalog(root)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if n < 20 {
		t.Fatalf("expected many seeded agents, got %d", n)
	}

	cat, found, err := LoadCatalog(root)
	if err != nil || !found {
		t.Fatalf("load: %v found=%v", err, found)
	}
	byName := map[string]CatalogAgent{}
	for _, a := range cat {
		byName[a.Name] = a
	}
	// A core role should be present, seeded with the "phase-" prefix stripped.
	planner, ok := byName["planner"]
	if !ok {
		t.Fatalf("expected a 'planner' catalog agent; got %d agents", len(cat))
	}
	if planner.Description == "" || planner.Body == "" {
		t.Errorf("planner catalog agent missing description/body: %+v", planner)
	}
	if planner.DefaultModel != ModelStrong {
		t.Errorf("planner default model = %q, want strong", planner.DefaultModel)
	}
	// A designated speed role should default to speed.
	if dc, ok := byName["doc-classifier"]; ok && dc.DefaultModel != ModelSpeed {
		t.Errorf("doc-classifier default model = %q, want speed", dc.DefaultModel)
	}
}

func TestSeedCatalogPreservesEdits(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedCatalog(root); err != nil {
		t.Fatal(err)
	}
	// Overwrite one agent, then re-seed; the edit must survive.
	p := filepath.Join(CatalogDir(root), "planner.prompt.md")
	custom := "---\nname: planner\ndefault_model: speed\n---\nMY CUSTOM PROMPT"
	if err := os.WriteFile(p, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SeedCatalog(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != custom {
		t.Error("re-seeding overwrote a user-edited catalog file")
	}
}

func TestLoadCatalogMissing(t *testing.T) {
	_, found, err := LoadCatalog(t.TempDir())
	if err != nil || found {
		t.Errorf("missing catalog: found=%v err=%v", found, err)
	}
}
