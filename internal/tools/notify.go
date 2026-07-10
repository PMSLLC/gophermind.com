package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Notify returns a tool that posts a message to a configured Slack or Discord
// incoming-webhook URL, for run results / approvals in a channel. The webhook
// URL is fixed by the operator (not chosen by the model), so it is not an SSRF
// vector. Sends both "text" (Slack) and "content" (Discord) so one tool works
// for either service.
func Notify(webhookURL string) Tool {
	return Tool{
		Name:        "notify",
		Description: "Post a short message to the configured Slack/Discord channel webhook (e.g. run results or approval requests).",
		Schema:      object(map[string]any{"message": str("The message to post.")}, "message"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if strings.TrimSpace(webhookURL) == "" {
				return "", fmt.Errorf("notify is not configured: set GOPHERMIND_NOTIFY_WEBHOOK")
			}
			var a struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			msg := strings.TrimSpace(a.Message)
			if msg == "" {
				return "", fmt.Errorf("message is empty")
			}
			// Both keys so a Slack ("text") or Discord ("content") webhook accepts it.
			body, _ := json.Marshal(map[string]string{"text": msg, "content": msg})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
			if err != nil {
				return "", err
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
			if err != nil {
				return "", fmt.Errorf("notify request: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return "", fmt.Errorf("notify webhook returned %d", resp.StatusCode)
			}
			return "message sent", nil
		},
	}
}
