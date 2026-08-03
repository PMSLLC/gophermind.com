package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

// maxBody bounds a single HTTP reply, so a misbehaving or hostile server cannot
// exhaust memory.
const maxBody = 8 * 1024 * 1024

// httpTransport speaks MCP over Streamable HTTP: each message is POSTed, and
// the reply arrives either as one JSON document or as an SSE stream. Servers
// choose per response, so both must be handled.
type httpTransport struct {
	url    string
	auth   Authenticator
	client *http.Client

	mu        sync.Mutex
	sessionID string // from the Mcp-Session-Id header, echoed on later requests
	version   string // negotiated protocol version
}

func newHTTPTransport(cfg ServerConfig, client *http.Client) *httpTransport {
	return &httpTransport{
		url:    cfg.URL,
		auth:   newStaticHeaders(cfg.Headers),
		client: client,
	}
}

func (t *httpTransport) setProtocolVersion(v string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.version = v
}

func (t *httpTransport) Send(ctx context.Context, msg []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	t.mu.Lock()
	session, version := t.sessionID, t.version
	t.mu.Unlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	// Auth last, so a configured header can override anything set above.
	if t.auth != nil {
		if err := t.auth.Apply(req); err != nil {
			return nil, err
		}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		t.mu.Lock()
		t.sessionID = id
		t.mu.Unlock()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("http %s: %s", resp.Status, summarize(string(body)))
	}
	// 202 with no content is how a server acknowledges a notification.
	if resp.StatusCode == http.StatusAccepted || isNotification(msg) {
		return nil, nil
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "text/event-stream" {
		return readSSE(resp.Body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	return bytes.TrimSpace(body), nil
}

func (t *httpTransport) Close() error { return nil }

// readSSE pulls the first JSON-RPC payload out of an SSE stream.
//
// Only "data:" lines carry content; event/id/retry lines and comments are
// framing. A single event's data may span several lines, which are joined with
// newlines per the SSE spec. The first complete event is the reply we asked
// for, so we stop there rather than draining a stream the server may hold open.
func readSSE(r io.Reader) ([]byte, error) {
	sc := bufio.NewScanner(io.LimitReader(r, maxBody))
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)

	var data []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" { // blank line terminates an event
			if len(data) > 0 {
				return []byte(strings.Join(data, "\n")), nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") { // comment / keepalive
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if field == "data" {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(data) > 0 { // stream ended without a trailing blank line
		return []byte(strings.Join(data, "\n")), nil
	}
	return nil, fmt.Errorf("event stream carried no data")
}

// summarize trims a server error body down to something loggable.
func summarize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(empty body)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
