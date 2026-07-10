package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReciprocalRankFusion(t *testing.T) {
	// id "a" is top of list1 and 2nd of list2 -> should win overall.
	list1 := []string{"a", "b", "c"}
	list2 := []string{"x", "a", "b"}
	fused := reciprocalRankFusion([][]string{list1, list2}, 60)
	if fused[0] != "a" {
		t.Errorf("fusion should rank 'a' first, got %v", fused)
	}
	// Every id appears exactly once.
	seen := map[string]int{}
	for _, id := range fused {
		seen[id]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("id %q appears %d times", id, n)
		}
	}
}

func TestHybridSearch(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "parser.txt"), []byte("parser handles tokens and grammar"), 0o644)
	os.WriteFile(filepath.Join(root, "db.txt"), []byte("database stores rows in tables"), 0o644)
	indexPath := filepath.Join(root, "index.json")
	if _, err := run(t, EmbedIndex(root, fakeEmbed{}, indexPath), `{"exts":[".txt"]}`); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, HybridSearch(root, fakeEmbed{}, indexPath), `{"query":"parser","k":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "parser.txt") {
		t.Errorf("hybrid search should surface parser.txt:\n%s", out)
	}
}

func TestHybridSearchNoIndex(t *testing.T) {
	if _, err := run(t, HybridSearch(t.TempDir(), fakeEmbed{}, filepath.Join(t.TempDir(), "none.json")), `{"query":"x"}`); err == nil {
		t.Error("missing index should error")
	}
}
