package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// httpMethods is the allowed set for the http_request tool.
var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
}

// HTTPRequest returns a gated tool for calling HTTP APIs with a method, headers,
// and a body. It shares fetch_url's egress controls: http/https only, optional
// host allowlist, and refusal of loopback/private/link-local targets (including
// on redirects) to prevent SSRF.
func HTTPRequest(allowHosts []string, budget *NetBudget) Tool {
	return httpTool(allowHosts, false, budget)
}

// httpTool is the constructor behind HTTPRequest; allowLoopback relaxes only the
// loopback block for tests.
func httpTool(allowHosts []string, allowLoopback bool, budget *NetBudget) Tool {
	return Tool{
		Name:        "http_request",
		Description: "Call an HTTP(S) API: choose method (GET/POST/PUT/PATCH/DELETE/HEAD), headers, and body; returns status, headers, and body. Egress-controlled and size-limited.",
		Schema: object(map[string]any{
			"url":       str("The http:// or https:// URL to call."),
			"method":    str("HTTP method (default GET)."),
			"headers":   map[string]any{"type": "object", "description": "Request headers as a string map.", "additionalProperties": map[string]any{"type": "string"}},
			"body":      str("Optional request body."),
			"max_bytes": map[string]any{"type": "integer", "description": "Maximum response body bytes to read (default 262144)."},
		}, "url"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				URL      string            `json:"url"`
				Method   string            `json:"method"`
				Headers  map[string]string `json:"headers"`
				Body     string            `json:"body"`
				MaxBytes int               `json:"max_bytes"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			method := strings.ToUpper(strings.TrimSpace(a.Method))
			if method == "" {
				method = http.MethodGet
			}
			if !httpMethods[method] {
				return "", fmt.Errorf("unsupported method %q", a.Method)
			}
			u, err := url.Parse(strings.TrimSpace(a.URL))
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return "", fmt.Errorf("invalid url %q: only http/https are allowed", a.URL)
			}
			if err := guardURL(u, allowHosts, allowLoopback); err != nil {
				return "", err
			}

			limit := a.MaxBytes
			if limit <= 0 {
				limit = defaultFetchLimit
			}

			var bodyReader io.Reader
			if a.Body != "" {
				bodyReader = strings.NewReader(a.Body)
			}
			req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "gophermind/http_request")
			for k, v := range a.Headers {
				req.Header.Set(k, v)
			}

			if err := budget.begin(); err != nil {
				return "", err
			}
			client := &http.Client{
				Timeout: 30 * time.Second,
				CheckRedirect: func(r *http.Request, via []*http.Request) error {
					if len(via) >= 10 {
						return fmt.Errorf("stopped after 10 redirects")
					}
					return guardURL(r.URL, allowHosts, allowLoopback)
				},
			}
			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("request %s: %w", u.Host, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)))
			if err != nil {
				return "", fmt.Errorf("read body: %w", err)
			}
			if err := budget.add(len(body)); err != nil {
				return "", err
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%s %s\n", method, u.String())
			fmt.Fprintf(&b, "status: %d\n", resp.StatusCode)
			writeSortedHeaders(&b, resp.Header)
			b.WriteString("\n")
			b.Write(body)
			if len(body) >= limit {
				fmt.Fprintf(&b, "\n\n[truncated at %d bytes]", limit)
			}
			return b.String(), nil
		},
	}
}

// writeSortedHeaders writes response headers in stable order for readability.
func writeSortedHeaders(b *strings.Builder, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s: %s\n", k, strings.Join(h[k], ", "))
	}
}
