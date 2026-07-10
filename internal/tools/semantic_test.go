package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbed is a hermetic embedding provider: term counts as the vector.
type fakeEmbed struct{}

func (fakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lt := strings.ToLower(t)
		out[i] = []float32{
			float32(strings.Count(lt, "parser")),
			float32(strings.Count(lt, "database")),
			float32(strings.Count(lt, "network")) + 0.01,
		}
	}
	return out, nil
}

func TestEmbedIndexThenSemanticSearch(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "parser.txt"), []byte("parser parser parser tokens"), 0o644)
	os.WriteFile(filepath.Join(root, "db.txt"), []byte("database database queries"), 0o644)
	indexPath := filepath.Join(root, ".gophermind", "index.json")

	// Build the index.
	out, err := run(t, EmbedIndex(root, fakeEmbed{}, indexPath), `{"exts":[".txt"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "index") {
		t.Errorf("index build output unexpected: %s", out)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("index file not written: %v", err)
	}

	// Search for parser content.
	out, err = run(t, SemanticSearch(root, fakeEmbed{}, indexPath, filepath.Join(root, "packs")), `{"query":"parser","k":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "parser.txt") {
		t.Errorf("semantic search should surface parser.txt:\n%s", out)
	}
	if strings.Contains(out, "db.txt") {
		t.Errorf("top-1 for parser query should not include db.txt:\n%s", out)
	}
}

func TestSemanticSearchNoIndex(t *testing.T) {
	root := t.TempDir()
	if _, err := run(t, SemanticSearch(root, fakeEmbed{}, filepath.Join(root, "missing.json"), ""), `{"query":"x"}`); err == nil {
		t.Error("search without an index should error with guidance")
	}
}

func TestEmbedIndexNilProvider(t *testing.T) {
	root := t.TempDir()
	if _, err := run(t, EmbedIndex(root, nil, filepath.Join(root, "i.json")), `{}`); err == nil {
		t.Error("nil provider should error explaining configuration")
	}
}

func TestImportPackThenSearch(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "net.md"), []byte("network network retries"), 0o644)
	packsDir := filepath.Join(root, ".gophermind", "packs")

	out, err := run(t, ImportPack(root, fakeEmbed{}, packsDir), `{"name":"kb","dir":"docs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "kb") {
		t.Errorf("import output should name the pack: %s", out)
	}

	// Search the pack.
	out, err = run(t, SemanticSearch(root, fakeEmbed{}, filepath.Join(root, "index.json"), packsDir), `{"query":"network","pack":"kb","k":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "net.md") {
		t.Errorf("pack search should surface net.md:\n%s", out)
	}
}

func TestImportPackBadName(t *testing.T) {
	root := t.TempDir()
	if _, err := run(t, ImportPack(root, fakeEmbed{}, filepath.Join(root, "packs")), `{"name":"../evil","dir":"."}`); err == nil {
		t.Error("bad pack name should be rejected")
	}
}
