// Package embed provides text embeddings via an OpenAI-compatible endpoint and
// pure-Go vector similarity, so the agent can build a local semantic index
// without any CGO or native vector-store dependency.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Provider turns text into embedding vectors.
type Provider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Vector is a stored embedding keyed by an id (and optional payload text).
type Vector struct {
	ID     string    `json:"id"`
	Text   string    `json:"text,omitempty"`
	Values []float32 `json:"values"`
}

// Hit is a search result: a vector plus its similarity to the query.
type Hit struct {
	ID    string
	Text  string
	Score float32
}

// cosine returns the cosine similarity of two equal-length vectors, or 0 when
// either has zero magnitude (or lengths differ).
func cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// TopK returns the k items most similar to query, ranked best-first.
func TopK(query []float32, items []Vector, k int) []Hit {
	hits := make([]Hit, 0, len(items))
	for _, it := range items {
		hits = append(hits, Hit{ID: it.ID, Text: it.Text, Score: cosine(query, it.Values)})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k < len(hits) {
		hits = hits[:k]
	}
	return hits
}

// HTTPProvider calls an OpenAI-compatible POST {base}/v1/embeddings endpoint.
type HTTPProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewHTTPProvider builds a provider for the given endpoint. baseURL may or may
// not already include the /v1 path; /v1/embeddings is appended if absent.
func NewHTTPProvider(baseURL, apiKey, model string) *HTTPProvider {
	return &HTTPProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed requests embeddings for texts.
func (p *HTTPProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	url := p.baseURL
	if !strings.Contains(url, "/embeddings") {
		if !strings.Contains(url, "/v1") {
			url += "/v1"
		}
		url += "/embeddings"
	}
	body, _ := json.Marshal(map[string]any{"model": p.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embeddings endpoint returned %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
