package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultDocsTemplate resolves Go module docs on pkg.go.dev. Override with
// GOPHERMIND_DOCS_URL_TEMPLATE using {lib} and {version} placeholders.
const DefaultDocsTemplate = "https://pkg.go.dev/{lib}@{version}"

// DocsLookup returns a tool that fetches library documentation (HTML → text) for
// a library@version from a URL template, caching results on disk keyed by
// library@version so accurate API docs are available offline afterwards.
func DocsLookup(urlTemplate, cacheDir string) Tool {
	return Tool{
		Name:        "docs_lookup",
		Description: "Fetch documentation for a library (optionally a specific version) and return it as text. Cached by library@version for accurate API usage over stale training data.",
		Schema: object(map[string]any{
			"library": str("The library/module to look up (e.g. github.com/go-chi/chi/v5)."),
			"version": str("Optional version (default 'latest')."),
		}, "library"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Library string `json:"library"`
				Version string `json:"version"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			lib := strings.TrimSpace(a.Library)
			if lib == "" {
				return "", fmt.Errorf("library is empty")
			}
			version := strings.TrimSpace(a.Version)
			if version == "" {
				version = "latest"
			}
			key := lib + "@" + version

			// Serve from cache when present.
			if cacheDir != "" {
				if cached, ok := cacheGet(cacheDir, key); ok {
					return cached, nil
				}
			}

			url := strings.ReplaceAll(urlTemplate, "{lib}", lib)
			url = strings.ReplaceAll(url, "{version}", version)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "gophermind/docs_lookup")
			resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
			if err != nil {
				return "", fmt.Errorf("docs request: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if resp.StatusCode >= 400 {
				return "", fmt.Errorf("docs for %q not found (status %d)", key, resp.StatusCode)
			}

			text := string(body)
			if isHTML(resp.Header.Get("Content-Type"), text) {
				text = htmlToText(text)
			}
			result := fmt.Sprintf("docs: %s\n\n%s", key, text)
			if cacheDir != "" {
				cachePut(cacheDir, key, result)
			}
			return truncate(result), nil
		},
	}
}
