package embed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProvider returns a deterministic embedding derived from the text so tests
// are hermetic (no network). Similar texts map to similar vectors.
type fakeProvider struct{}

func (fakeProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lt := strings.ToLower(t)
		out[i] = []float32{
			float32(strings.Count(lt, "alpha")),
			float32(strings.Count(lt, "beta")),
			float32(strings.Count(lt, "gamma")) + 0.01, // avoid all-zero vectors
		}
	}
	return out, nil
}

func TestBuildAndSearchIndex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha alpha alpha"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta beta beta"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("gamma gamma gamma"), 0o644)

	idx, err := BuildIndex(context.Background(), fakeProvider{}, dir, []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Vectors) < 3 {
		t.Fatalf("expected >=3 vectors, got %d", len(idx.Vectors))
	}

	// Query similar to the alpha file.
	q, _ := fakeProvider{}.Embed(context.Background(), []string{"alpha"})
	hits := TopK(q[0], idx.Vectors, 1)
	if len(hits) != 1 || !strings.Contains(hits[0].ID, "a.txt") {
		t.Errorf("alpha query should match a.txt, got %+v", hits)
	}
}

func TestIndexSaveLoad(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha beta gamma"), 0o644)
	idx, err := BuildIndex(context.Background(), fakeProvider{}, dir, []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "index.json")
	if err := idx.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Vectors) != len(idx.Vectors) {
		t.Errorf("save/load vector count mismatch: %d vs %d", len(loaded.Vectors), len(idx.Vectors))
	}
}

func TestChunkText(t *testing.T) {
	// A long line-based text should split into multiple chunks.
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line of content here\n")
	}
	chunks := chunkText(sb.String(), 50)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
}
