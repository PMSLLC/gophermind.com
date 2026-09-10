package freellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// checkTimeout bounds a probe so a hung endpoint cannot stall the CLI.
const checkTimeout = 15 * time.Second

// Check probes an OpenAI-compatible endpoint by listing its models, returning
// the model IDs it advertises. It is how a user proves an endpoint works before
// trusting it, and how the sync script proves compat.go is still accurate.
//
// The API key is sent as a bearer token when non-empty and never appears in a
// returned error. hc may be nil, in which case a bounded default client is used.
func Check(ctx context.Context, baseURL, apiKey string, hc *http.Client) ([]string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: checkTimeout}
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Cap the body at 1 MiB so a misbehaving endpoint cannot exhaust memory.
	// A model list or an error body this large is not expected in practice; if
	// the cap is ever hit, the truncated bytes below can produce a confusing
	// "not an OpenAI model list" JSON error rather than one that says truncated.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is the provider's, and a provider that echoes request
		// headers could hand our own key back to us. Redact it rather than
		// trusting the endpoint: this function is used to probe un-vetted
		// third-party endpoints, and its errors get pasted into bug reports.
		safe := strings.TrimSpace(string(body))
		if apiKey != "" {
			safe = strings.ReplaceAll(safe, apiKey, "[redacted]")
		}
		return nil, fmt.Errorf("GET %s returned %d %s: %s",
			url, resp.StatusCode, http.StatusText(resp.StatusCode), safe)
	}

	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("GET %s returned a body that is not an OpenAI model list: %w", url, err)
	}
	ids := make([]string, 0, len(doc.Data))
	for _, m := range doc.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}
