package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	if got := cosine(a, []float32{1, 0, 0}); math.Abs(float64(got)-1) > 1e-6 {
		t.Errorf("identical vectors cosine = %f, want 1", got)
	}
	if got := cosine(a, []float32{0, 1, 0}); math.Abs(float64(got)) > 1e-6 {
		t.Errorf("orthogonal cosine = %f, want 0", got)
	}
	if got := cosine(a, []float32{-1, 0, 0}); math.Abs(float64(got)+1) > 1e-6 {
		t.Errorf("opposite cosine = %f, want -1", got)
	}
	// Zero vector must not divide by zero.
	if got := cosine(a, []float32{0, 0, 0}); got != 0 {
		t.Errorf("zero vector cosine = %f, want 0", got)
	}
}

func TestTopK(t *testing.T) {
	query := []float32{1, 0}
	items := []Vector{
		{ID: "a", Values: []float32{1, 0}},     // sim 1
		{ID: "b", Values: []float32{0.9, 0.1}}, // high
		{ID: "c", Values: []float32{0, 1}},     // sim 0
	}
	hits := TopK(query, items, 2)
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "a" {
		t.Errorf("top hit = %q, want a", hits[0].ID)
	}
	if hits[1].ID != "b" {
		t.Errorf("second hit = %q, want b", hits[1].ID)
	}
}

func TestHTTPProviderEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		// Return a deterministic embedding per input (length = number of inputs).
		resp := map[string]any{"data": []map[string]any{}}
		var data []map[string]any
		for i := range req.Input {
			data = append(data, map[string]any{"embedding": []float32{float32(i), 1, 0}})
		}
		resp["data"] = data
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, "", "test-model")
	vecs, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 {
		t.Fatalf("unexpected embeddings shape: %v", vecs)
	}
	if vecs[1][0] != 1 {
		t.Errorf("second embedding wrong: %v", vecs[1])
	}
}
