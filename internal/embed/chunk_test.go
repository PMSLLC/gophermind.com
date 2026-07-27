package embed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// TestChunkTextCapsOversizedChunks covers the failure that broke indexing on the
// server: 50 lines of dense prose tokenized past the embedding model's context
// (2048), and llama.cpp rejects the whole batched request. Line count alone is
// not a bound on size, so chunkText must also cap chunk length.
func TestChunkTextCapsOversizedChunks(t *testing.T) {
	// 50 lines x 400 chars = 20000 chars in what used to be a single chunk.
	long := strings.Repeat(strings.Repeat("a", 399)+"\n", 50)
	for _, c := range chunkText(long, 50) {
		if len(c) > maxChunkChars {
			t.Errorf("chunk of %d chars exceeds cap %d", len(c), maxChunkChars)
		}
	}
}

// TestChunkTextSplitsSingleOversizedLine covers minified/generated files, where
// one line alone blows the cap and there is no line boundary to split on.
func TestChunkTextSplitsSingleOversizedLine(t *testing.T) {
	one := strings.Repeat("x", maxChunkChars*3)
	chunks := chunkText(one, 50)
	if len(chunks) < 3 {
		t.Errorf("expected the long line to split into >=3 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > maxChunkChars {
			t.Errorf("chunk of %d chars exceeds cap %d", len(c), maxChunkChars)
		}
	}
}

// TestChunkTextPreservesContent asserts chunking loses nothing: the concatenated
// chunks must contain every original line, so retrieval can still surface any
// part of the file.
func TestChunkTextPreservesContent(t *testing.T) {
	text := "alpha\n" + strings.Repeat("y", maxChunkChars+100) + "\nomega"
	joined := strings.Join(chunkText(text, 50), "")
	if !strings.Contains(joined, "alpha") || !strings.Contains(joined, "omega") {
		t.Error("chunking dropped content at a split boundary")
	}
	if want := strings.Count(text, "y"); strings.Count(joined, "y") != want {
		t.Errorf("chunking lost body characters: got %d, want %d", strings.Count(joined, "y"), want)
	}
}

// TestChunkTextSplitsOnRuneBoundaries guards against slicing a multi-byte rune
// in half when hard-splitting a long line, which would emit invalid UTF-8 and
// corrupt the JSON request body.
func TestChunkTextSplitsOnRuneBoundaries(t *testing.T) {
	// "世" is 3 bytes; a cap that does not divide evenly by 3 will land mid-rune
	// unless the split is rune-aware.
	for _, c := range chunkText(strings.Repeat("世", maxChunkChars), 50) {
		if !utf8.ValidString(c) {
			t.Fatal("chunk contains invalid UTF-8: split landed mid-rune")
		}
	}
}

// recordingProvider records the size of each Embed call so tests can assert on
// batching, and returns one distinct vector per text so ordering is checkable.
type recordingProvider struct {
	mu    sync.Mutex
	calls []int
	seq   float32
}

func (r *recordingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, len(texts))
	out := make([][]float32, len(texts))
	for i := range texts {
		r.seq++
		out[i] = []float32{r.seq}
	}
	return out, nil
}

// TestEmbedAllBatches covers the second half of the server failure: every chunk
// went out in ONE request, against a 60s client timeout, so a large repo could
// never finish. Requests must be split into bounded batches.
func TestEmbedAllBatches(t *testing.T) {
	texts := make([]string, embedBatchSize*2+5)
	for i := range texts {
		texts[i] = "chunk"
	}
	p := &recordingProvider{}
	vecs, err := embedAll(context.Background(), p, texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("got %d vectors for %d texts", len(vecs), len(texts))
	}
	if len(p.calls) != 3 {
		t.Errorf("expected 3 batched requests, got %d (sizes %v)", len(p.calls), p.calls)
	}
	for _, n := range p.calls {
		if n > embedBatchSize {
			t.Errorf("batch of %d exceeds max %d", n, embedBatchSize)
		}
	}
}

// TestEmbedAllPreservesOrder asserts batching keeps vectors aligned with their
// texts — a misalignment would silently attach every chunk's text to another
// chunk's vector and poison retrieval.
func TestEmbedAllPreservesOrder(t *testing.T) {
	texts := make([]string, embedBatchSize+3)
	for i := range texts {
		texts[i] = "chunk"
	}
	vecs, err := embedAll(context.Background(), &recordingProvider{}, texts)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vecs {
		if len(v) != 1 || v[0] != float32(i+1) {
			t.Fatalf("vector %d out of order: %v", i, v)
		}
	}
}

// TestBuildIndexBatchesLargeRepos is the end-to-end guard: a repo big enough to
// exceed one batch must still index, in bounded requests.
func TestBuildIndexBatchesLargeRepos(t *testing.T) {
	dir := t.TempDir()
	// Each file yields >=1 chunk; enough files to cross the batch boundary.
	for i := 0; i < embedBatchSize+10; i++ {
		os.WriteFile(filepath.Join(dir, "f"+itoa(i)+".txt"), []byte("alpha content"), 0o644)
	}
	p := &recordingProvider{}
	idx, err := BuildIndex(context.Background(), p, dir, []string{".txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Vectors) != embedBatchSize+10 {
		t.Errorf("expected %d vectors, got %d", embedBatchSize+10, len(idx.Vectors))
	}
	if len(p.calls) < 2 {
		t.Errorf("expected batched requests, got %d call(s)", len(p.calls))
	}
}

// TestBuildIndexCapsChunkSize is the regression test for the exact server
// failure: a docs-shaped file with long prose lines must produce only chunks the
// embedding endpoint can accept.
func TestBuildIndexCapsChunkSize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "doc.md"),
		[]byte(strings.Repeat(strings.Repeat("word ", 200)+"\n", 60)), 0o644)
	idx, err := BuildIndex(context.Background(), &recordingProvider{}, dir, []string{".md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Vectors) < 2 {
		t.Fatalf("expected the oversized doc to split, got %d vectors", len(idx.Vectors))
	}
	for _, v := range idx.Vectors {
		if len(v.Text) > maxChunkChars {
			t.Errorf("indexed chunk of %d chars exceeds cap %d", len(v.Text), maxChunkChars)
		}
	}
}

// TestUpdateIndexCapsAndBatches asserts the incremental path got the same
// treatment — it re-chunks changed files through the same code.
func TestUpdateIndexCapsAndBatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "big.md"),
		[]byte(strings.Repeat(strings.Repeat("word ", 200)+"\n", 60)), 0o644)
	p := &recordingProvider{}
	out, err := UpdateIndex(context.Background(), p, dir, []string{".md"}, &Index{}, []string{"big.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Vectors) < 2 {
		t.Fatalf("expected the oversized doc to split, got %d vectors", len(out.Vectors))
	}
	for _, v := range out.Vectors {
		if len(v.Text) > maxChunkChars {
			t.Errorf("chunk of %d chars exceeds cap %d", len(v.Text), maxChunkChars)
		}
	}
}
