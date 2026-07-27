package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/internal/agent"
	"gophermind/internal/embed"
	"gophermind/internal/llm"
)

// stubEmbed returns a fixed vector, so a query always matches a stored vector
// with the same values. Hermetic: no network.
type stubEmbed struct{}

func (stubEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

// writeStore saves a one-vector store at path and returns it.
func writeStore(t *testing.T, path, id, text string) {
	t.Helper()
	idx := &embed.Index{Vectors: []embed.Vector{{ID: id, Text: text, Values: []float32{1, 0, 0}}}}
	if err := idx.Save(path); err != nil {
		t.Fatal(err)
	}
}

func testPaths(t *testing.T) retrievalPaths {
	t.Helper()
	dir := t.TempDir()
	return retrievalPaths{
		index:    filepath.Join(dir, "index.json"),
		memory:   filepath.Join(dir, "memory.json"),
		profile:  filepath.Join(dir, "profile.json"),
		episodes: filepath.Join(dir, "episodes.json"),
	}
}

// TestRetrievalBlocksDisabledByDefault keeps injection opt-in: with neither var
// set, a turn must be byte-identical to today's behavior.
func TestRetrievalBlocksDisabledByDefault(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	t.Setenv("GOPHERMIND_RAG", "")
	t.Setenv("GOPHERMIND_MEMORY", "")

	if got := retrievalBlocks(context.Background(), stubEmbed{}, p, "task"); got != "" {
		t.Errorf("expected no injection when disabled, got %q", got)
	}
}

// TestRetrievalBlocksNilProvider guards the unconfigured-embeddings case: the
// vars can be on while GOPHERMIND_EMBED_MODEL is unset, and that must not panic.
func TestRetrievalBlocksNilProvider(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	t.Setenv("GOPHERMIND_RAG", "1")
	t.Setenv("GOPHERMIND_MEMORY", "1")

	if got := retrievalBlocks(context.Background(), nil, p, "task"); got != "" {
		t.Errorf("expected no injection without a provider, got %q", got)
	}
}

func TestRetrievalBlocksIncludesIndexHits(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	t.Setenv("GOPHERMIND_RAG", "1")
	t.Setenv("GOPHERMIND_MEMORY", "")

	got := retrievalBlocks(context.Background(), stubEmbed{}, p, "task")
	if !strings.Contains(got, "<retrieved_context>") {
		t.Errorf("expected a retrieved_context block, got %q", got)
	}
	if !strings.Contains(got, "alpha content") {
		t.Errorf("expected the indexed chunk's text, got %q", got)
	}
}

func TestRetrievalBlocksIncludesMemory(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.memory, "fact-1", "the deploy host is 10.0.0.5")
	t.Setenv("GOPHERMIND_RAG", "")
	t.Setenv("GOPHERMIND_MEMORY", "1")

	got := retrievalBlocks(context.Background(), stubEmbed{}, p, "task")
	if !strings.Contains(got, "<long_term_memory>") {
		t.Errorf("expected a long_term_memory block, got %q", got)
	}
	if strings.Contains(got, "<retrieved_context>") {
		t.Error("GOPHERMIND_MEMORY must not enable index RAG")
	}
}

// TestRetrievalBlocksRagDoesNotEnableMemory is the mirror of the above: the two
// vars gate independent stores.
func TestRetrievalBlocksRagDoesNotEnableMemory(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	writeStore(t, p.memory, "fact-1", "remembered thing")
	t.Setenv("GOPHERMIND_RAG", "1")
	t.Setenv("GOPHERMIND_MEMORY", "")

	got := retrievalBlocks(context.Background(), stubEmbed{}, p, "task")
	if strings.Contains(got, "<long_term_memory>") {
		t.Error("GOPHERMIND_RAG must not enable long-term memory")
	}
}

// newTestAgent builds a real agent with a client that is never called (no Send).
func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	return agent.New(llm.New("http://127.0.0.1:1", "", "m", time.Second, false), nil, 1, nil, nil)
}

// TestInjectRetrievalRestoresSystemPrompt is the guard for the serve path.
// AppendSystemPrompt mutates msgs[0] in place and session.Save persists the whole
// history, so without a restore every session turn would permanently accumulate
// its own retrieved context and the stored prompt would grow without bound.
func TestInjectRetrievalRestoresSystemPrompt(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	t.Setenv("GOPHERMIND_RAG", "1")

	ag := newTestAgent(t)
	ag.SetSystemPrompt("base prompt")

	restore := injectRetrieval(context.Background(), ag, stubEmbed{}, p, "task")
	if !strings.Contains(ag.SystemPrompt(), "alpha content") {
		t.Fatal("expected the retrieved context to be injected for the turn")
	}
	restore()
	if got := ag.SystemPrompt(); got != "base prompt" {
		t.Errorf("system prompt not restored: %q", got)
	}
}

// TestInjectRetrievalRepeatedTurnsDoNotAccumulate simulates several session
// turns: the prompt must return to its original size every time.
func TestInjectRetrievalRepeatedTurnsDoNotAccumulate(t *testing.T) {
	p := testPaths(t)
	writeStore(t, p.index, "a.go#0", "alpha content")
	t.Setenv("GOPHERMIND_RAG", "1")

	ag := newTestAgent(t)
	ag.SetSystemPrompt("base prompt")
	for i := 0; i < 5; i++ {
		restore := injectRetrieval(context.Background(), ag, stubEmbed{}, p, "task")
		restore()
	}
	if got := ag.SystemPrompt(); got != "base prompt" {
		t.Errorf("system prompt grew across turns: %q", got)
	}
}

// TestInjectRetrievalNoopWhenDisabled asserts the restore func is always safe to
// call, even when nothing was injected.
func TestInjectRetrievalNoopWhenDisabled(t *testing.T) {
	p := testPaths(t)
	t.Setenv("GOPHERMIND_RAG", "")
	t.Setenv("GOPHERMIND_MEMORY", "")

	ag := newTestAgent(t)
	ag.SetSystemPrompt("base prompt")
	restore := injectRetrieval(context.Background(), ag, stubEmbed{}, p, "task")
	restore()
	if got := ag.SystemPrompt(); got != "base prompt" {
		t.Errorf("disabled injection changed the prompt: %q", got)
	}
}
