package planner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fixture directory written before the outline was harness-driven holds one
// contract.outline.txt: it serves the shared pass, and later batches get an
// empty reply, so older fixture sets keep working.
func TestFixtureProviderServesALegacySingleOutline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "contract.outline.txt"), []byte(`{"components": [{"id": "a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake, err := FixtureProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	ask := func(stage string) string {
		resp, err := fake.Complete(context.Background(), request(stage, "p", 10))
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		return resp.Text
	}
	if got := ask("contract:outline:1"); !strings.Contains(got, `"a"`) {
		t.Errorf("shared pass = %q, want the legacy outline", got)
	}
	if got := ask("contract:outline:2"); !strings.Contains(got, `"components": []`) {
		t.Errorf("batch = %q, want an empty reply", got)
	}
	if _, err := fake.Complete(context.Background(), request("contract:other", "p", 10)); err == nil {
		t.Error("a stage with no fixture must still be an error")
	}
}
