package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gophermind/internal/embed"
)

// BraveSearchEndpoint is the default Brave Search API web endpoint.
const BraveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"

// searchResult is one web result.
type searchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// WebSearch returns a tool that queries the Brave Search API and returns the top
// results (title, url, description). It requires an API subscription token; the
// endpoint is overridable for testing. When an embeddings provider is given,
// results are re-ranked by semantic similarity to the query so the top hit is
// the most relevant, not just Brave's default order. Read-only.
func WebSearch(endpoint, apiKey string, p embed.Provider) Tool {
	return Tool{
		Name:        "web_search",
		Description: "Search the web via the Brave Search API and return the top results (title, URL, snippet). Use for current information.",
		Schema: object(map[string]any{
			"query": str("The search query."),
			"count": map[string]any{"type": "integer", "description": "Number of results (1-20, default 5)."},
		}, "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Query string `json:"query"`
				Count int    `json:"count"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if apiKey == "" {
				return "", fmt.Errorf("web_search is not configured: set GOPHERMIND_BRAVE_API_KEY")
			}
			q := strings.TrimSpace(a.Query)
			if q == "" {
				return "", fmt.Errorf("empty query")
			}
			count := a.Count
			if count <= 0 {
				count = 5
			}
			if count > 20 {
				count = 20
			}

			u, err := url.Parse(endpoint)
			if err != nil {
				return "", fmt.Errorf("bad endpoint: %w", err)
			}
			qs := u.Query()
			qs.Set("q", q)
			qs.Set("count", strconv.Itoa(count))
			u.RawQuery = qs.Encode()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("X-Subscription-Token", apiKey)

			resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
			if err != nil {
				return "", fmt.Errorf("search request: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("search failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}

			var parsed struct {
				Web struct {
					Results []searchResult `json:"results"`
				} `json:"web"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return "", fmt.Errorf("parse search response: %w", err)
			}
			results := parsed.Web.Results
			if len(results) == 0 {
				return fmt.Sprintf("(no results for %q)", q), nil
			}
			// Re-rank by embedding similarity when a provider is configured.
			if p != nil {
				if reranked, err := rerankResults(ctx, p, q, results); err == nil {
					results = reranked
				}
			}
			var b strings.Builder
			for i, r := range results {
				fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Description)
			}
			return truncate(b.String()), nil
		},
	}
}

// rerankResults reorders results by cosine similarity of each result's
// title+description to the query, most-relevant first. It reuses the embeddings
// provider and the tested TopK ranking.
func rerankResults(ctx context.Context, p embed.Provider, query string, results []searchResult) ([]searchResult, error) {
	texts := make([]string, 0, len(results)+1)
	texts = append(texts, query)
	for _, r := range results {
		texts = append(texts, r.Title+" "+r.Description)
	}
	vecs, err := p.Embed(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		return nil, fmt.Errorf("embed for rerank: %w", err)
	}
	items := make([]embed.Vector, len(results))
	for i := range results {
		items[i] = embed.Vector{ID: strconv.Itoa(i), Values: vecs[i+1]}
	}
	hits := embed.TopK(vecs[0], items, len(items))
	out := make([]searchResult, 0, len(results))
	for _, h := range hits {
		if idx, err := strconv.Atoi(h.ID); err == nil && idx >= 0 && idx < len(results) {
			out = append(out, results[idx])
		}
	}
	return out, nil
}
