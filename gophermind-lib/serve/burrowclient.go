package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// burrowMaxBody is the most of a Burrow response the client will read.
const burrowMaxBody = 1 << 20

// burrowError is a classified Burrow failure. Kind is one of unreachable,
// unauthorized, bad_request, server, timeout, other. It never carries the token.
type burrowError struct {
	Kind string
	Msg  string
}

func (e *burrowError) Error() string { return "burrow " + e.Kind + ": " + e.Msg }

// burrowLookup is the part of a legacy-mode POST /v1/lookup response the chat
// tool uses. Unknown fields are ignored.
type burrowLookup struct {
	Outcome   string          `json:"outcome"`
	Key       string          `json:"key"`
	Contract  json.RawMessage `json:"contract"`
	Rendering *struct {
		Lang       string `json:"lang"`
		Version    int    `json:"version"`
		Body       string `json:"body"`
		VerifiedAt string `json:"verified_at"`
	} `json:"rendering"`
	Eligible             bool `json:"eligible"`
	AwaitingConfirmation bool `json:"awaiting_confirmation"`
}

// burrowClient calls Burrow over HTTP. The token file is read on every call.
type burrowClient struct {
	baseURL   string
	tokenFile string
	hc        *http.Client
}

func newBurrowClient(baseURL, tokenFile string) *burrowClient {
	return &burrowClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		tokenFile: tokenFile,
		hc: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// lookup asks Burrow for an exact-key match of contract in lang (legacy mode:
// no vectors are sent).
func (c *burrowClient) lookup(ctx context.Context, contract map[string]any, lang string) (burrowLookup, error) {
	var zero burrowLookup
	raw, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return zero, &burrowError{"other", "cannot read the token file"}
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return zero, &burrowError{"other", "the token file is empty"}
	}
	// scrub keeps the token out of anything we surface.
	scrub := func(s string) string { return strings.ReplaceAll(s, token, "[redacted]") }

	payload, err := json.Marshal(map[string]any{"contract": contract, "lang": lang})
	if err != nil {
		return zero, &burrowError{"other", "cannot encode the request"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/lookup", bytes.NewReader(payload))
	if err != nil {
		return zero, &burrowError{"other", "bad Burrow URL"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return zero, &burrowError{"timeout", "the request timed out"}
		}
		return zero, &burrowError{"unreachable", scrub(err.Error())}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, burrowMaxBody+1))
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return zero, &burrowError{"timeout", "the response timed out"}
		}
		return zero, &burrowError{"other", "cannot read the response"}
	}
	if len(body) > burrowMaxBody {
		return zero, &burrowError{"other", fmt.Sprintf("response is over the %d byte limit", burrowMaxBody)}
	}

	if resp.StatusCode != http.StatusOK {
		msg := envelopeMessage(body)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return zero, &burrowError{"unauthorized", "Burrow rejected the token"}
		case resp.StatusCode == http.StatusBadRequest:
			if msg == "" {
				msg = "invalid request"
			}
			return zero, &burrowError{"bad_request", scrub(msg)}
		case resp.StatusCode >= 500:
			return zero, &burrowError{"server", fmt.Sprintf("status %d", resp.StatusCode)}
		default:
			return zero, &burrowError{"other", fmt.Sprintf("unexpected status %d", resp.StatusCode)}
		}
	}

	var out burrowLookup
	if err := json.Unmarshal(body, &out); err != nil {
		return zero, &burrowError{"other", "cannot decode the response"}
	}
	return out, nil
}

// envelopeMessage returns error.message from Burrow's error envelope, or "".
func envelopeMessage(body []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	return env.Error.Message
}
