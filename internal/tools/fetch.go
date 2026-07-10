package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// defaultFetchLimit caps how many bytes fetch_url reads from a response body.
const defaultFetchLimit = 256 * 1024

var (
	scriptStyleRE = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagRE         = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRE          = regexp.MustCompile(`[ \t]+`)
	blankLinesRE  = regexp.MustCompile(`\n{3,}`)
)

// FetchURL returns a gated tool that performs an HTTP(S) GET and returns the
// response body as readable text (HTML tags stripped). It is egress-controlled:
// only http/https URLs are allowed, and if allowHosts is non-empty the request
// host (ignoring port) must match one of the entries, otherwise it is refused.
// This is the safe alternative to shelling out to curl.
func FetchURL(allowHosts []string) Tool {
	return Tool{
		Name:        "fetch_url",
		Description: "Fetch an http(s) URL with a GET request and return the response body as readable text (HTML is stripped). Egress-controlled and size-limited.",
		Schema: object(map[string]any{
			"url":       str("The http:// or https:// URL to fetch."),
			"max_bytes": map[string]any{"type": "integer", "description": "Maximum number of body bytes to read (default 262144)."},
		}, "url"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				URL      string `json:"url"`
				MaxBytes int    `json:"max_bytes"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			u, err := url.Parse(strings.TrimSpace(a.URL))
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return "", fmt.Errorf("invalid url %q: only http/https URLs are allowed", a.URL)
			}
			if !hostAllowed(u.Hostname(), allowHosts) {
				return "", fmt.Errorf("host %q is not in the fetch allowlist", u.Hostname())
			}

			limit := a.MaxBytes
			if limit <= 0 {
				limit = defaultFetchLimit
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "gophermind/fetch_url")

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("fetch %s: %w", u.Host, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)))
			if err != nil {
				return "", fmt.Errorf("read body: %w", err)
			}
			truncated := len(body) >= limit

			text := string(body)
			if isHTML(resp.Header.Get("Content-Type"), text) {
				text = htmlToText(text)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d %s\n", resp.StatusCode, u.String())
			b.WriteString(text)
			if truncated {
				fmt.Fprintf(&b, "\n\n[truncated at %d bytes]", limit)
			}
			return b.String(), nil
		},
	}
}

// hostAllowed reports whether host passes the allowlist. An empty allowlist
// permits any host (egress restriction is opt-in via #68).
func hostAllowed(host string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	host = strings.ToLower(host)
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" && (host == a || strings.HasSuffix(host, "."+a)) {
			return true
		}
	}
	return false
}

func isHTML(contentType, body string) bool {
	if strings.Contains(strings.ToLower(contentType), "html") {
		return true
	}
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.Contains(strings.ToLower(head), "<html") || strings.Contains(strings.ToLower(head), "<!doctype html")
}

// htmlToText strips scripts, styles, and tags, then collapses whitespace into
// a compact, readable approximation of the page's text.
func htmlToText(s string) string {
	s = scriptStyleRE.ReplaceAllString(s, " ")
	s = tagRE.ReplaceAllString(s, " ")
	s = htmlUnescape(s)
	s = wsRE.ReplaceAllString(s, " ")
	// Trim trailing spaces on each line, then collapse runs of blank lines.
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	s = strings.Join(lines, "\n")
	s = blankLinesRE.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// htmlUnescape resolves the handful of entities common in body text.
func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&apos;", "'", "&nbsp;", " ",
	)
	return r.Replace(s)
}
