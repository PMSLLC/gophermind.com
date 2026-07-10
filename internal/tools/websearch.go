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
)

// BraveSearchEndpoint is the default Brave Search API web endpoint.
const BraveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"

// WebSearch returns a tool that queries the Brave Search API and returns the top
// results (title, url, description). It requires an API subscription token; the
// endpoint is overridable for testing. Read-only information retrieval from a
// single fixed host, so it is not gated (but it is off unless a key is set).
func WebSearch(endpoint, apiKey string) Tool {
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
					Results []struct {
						Title       string `json:"title"`
						URL         string `json:"url"`
						Description string `json:"description"`
					} `json:"results"`
				} `json:"web"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return "", fmt.Errorf("parse search response: %w", err)
			}
			if len(parsed.Web.Results) == 0 {
				return fmt.Sprintf("(no results for %q)", q), nil
			}
			var b strings.Builder
			for i, r := range parsed.Web.Results {
				fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Description)
			}
			return truncate(b.String()), nil
		},
	}
}
