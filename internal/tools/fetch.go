package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gophermind/internal/safety"
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
// only http/https URLs are allowed, if allowHosts is non-empty the request host
// must match one of the entries, and requests (including every redirect hop) to
// loopback/private/link-local addresses — e.g. the cloud metadata endpoint
// 169.254.169.254 — are refused to prevent SSRF. Safe alternative to curl.
func FetchURL(allowHosts []string, budget *NetBudget) Tool {
	return fetchTool(allowHosts, false, budget)
}

// fetchTool is the constructor behind FetchURL. allowLoopback relaxes only the
// loopback (127.0.0.0/8, ::1) block so tests can target httptest servers; it
// never relaxes the private/link-local blocks, and production always passes
// false.
func fetchTool(allowHosts []string, allowLoopback bool, budget *NetBudget) Tool {
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
			if err := guardURL(u, allowHosts, allowLoopback); err != nil {
				return "", err
			}

			limit := a.MaxBytes
			if limit <= 0 {
				limit = defaultFetchLimit
			}

			// Offline docs cache: serve a previously fetched copy when enabled,
			// without touching the network (or the network budget).
			cacheDir := fetchCacheDir()
			if cacheDir != "" {
				if cached, ok := cacheGet(cacheDir, u.String()); ok {
					return cached, nil
				}
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "gophermind/fetch_url")

			if err := budget.begin(); err != nil {
				return "", err
			}
			client := &http.Client{
				Timeout: 30 * time.Second,
				// Re-apply the egress guard to every redirect target so an allowed
				// host cannot bounce us to a blocked host or an internal IP.
				CheckRedirect: func(r *http.Request, via []*http.Request) error {
					if len(via) >= 10 {
						return fmt.Errorf("stopped after 10 redirects")
					}
					return guardURL(r.URL, allowHosts, allowLoopback)
				},
			}
			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("fetch %s: %w", u.Host, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)))
			if err != nil {
				return "", fmt.Errorf("read body: %w", err)
			}
			if err := budget.add(len(body)); err != nil {
				return "", err
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
			result := b.String()
			// Persist for offline reuse (only cache successful responses).
			if cacheDir != "" && resp.StatusCode < 400 {
				cachePut(cacheDir, u.String(), result)
			}
			// Opt-in prompt-injection defense: wrap fetched content that looks like
			// it is trying to hijack instructions in an untrusted-data marker.
			if injectionGuardEnabled() {
				result = safety.NeutralizeInjection(result)
			}
			return result, nil
		},
	}
}

// guardURL enforces the egress policy for a single URL (used for the initial
// request and each redirect hop): host allowlist, then a resolved-IP check that
// blocks loopback/private/link-local/unspecified targets to prevent SSRF into
// the local host, internal networks, or the cloud metadata service.
func guardURL(u *url.URL, allow []string, allowLoopback bool) error {
	host := u.Hostname()
	if !hostAllowed(host, allow) {
		return fmt.Errorf("host %q is not in the fetch allowlist", host)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if disallowedIP(ip, allowLoopback) {
			return fmt.Errorf("refusing to fetch %q: resolves to a blocked address %s (loopback/private/link-local)", host, ip)
		}
	}
	return nil
}

// disallowedIP reports whether ip must be refused. Private (RFC1918/ULA),
// link-local (incl. 169.254.169.254 metadata), unspecified, and multicast are
// always blocked; loopback is blocked unless allowLoopback (tests only).
func disallowedIP(ip net.IP, allowLoopback bool) bool {
	if ip.IsLoopback() {
		return !allowLoopback
	}
	return ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
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
