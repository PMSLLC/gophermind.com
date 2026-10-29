package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// StableSessionID returns a session id derived from the absolute path of dir,
// so every run in the same directory names the same server-side session. A
// server that keeps per-session KV state then reuses the cached prompt prefix
// across runs, and a run that dies without closing its session leaves nothing
// stranded: the next run in that directory picks the same session up again.
// The result is printable ASCII within the 128-character limit servers such as
// ExecuTorch's enforce.
func StableSessionID(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	sum := sha256.Sum256([]byte(abs))
	return "gm-" + hex.EncodeToString(sum[:8])
}

// CloseSession asks the server to drop the named session (DELETE
// {chat root}/sessions/{id}), freeing its slot and its cached KV. The chat
// root is the chat path with its trailing "/chat/completions" removed, so it
// follows ChatPath whether or not BaseURL already ends in the version segment.
// A server without sessions answers 404, which is reported as an error the
// caller may ignore.
func (c *Client) CloseSession(ctx context.Context, id string) error {
	root := strings.TrimSuffix(c.chatPath(), "/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+root+"/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("close session: %s", resp.Status)
	}
	return nil
}
