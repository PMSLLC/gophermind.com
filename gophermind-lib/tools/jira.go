package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// jiraKeyRe validates a Jira issue key like ABC-123.
var jiraKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]+-[0-9]+$`)

// JiraTool returns a read-only Jira tool (get an issue's summary/status) via the
// REST API with basic auth (email + API token). base is the site URL. Works
// against a real Jira Cloud site when credentials are configured.
func JiraTool(base, email, token string) Tool {
	return Tool{
		Name:        "jira",
		Description: "Read a Jira issue (summary, status, description) by key. Read-only; requires site URL + email + API token.",
		Schema: object(map[string]any{
			"action": str("Currently: get_issue."),
			"key":    str("The Jira issue key, e.g. ABC-123."),
		}, "action", "key"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Action string `json:"action"`
				Key    string `json:"key"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if email == "" || token == "" {
				return "", fmt.Errorf("jira is not configured: set GOPHERMIND_JIRA_EMAIL and GOPHERMIND_JIRA_TOKEN")
			}
			if a.Action != "get_issue" && a.Action != "" {
				return "", fmt.Errorf("unknown action %q (use get_issue)", a.Action)
			}
			if !jiraKeyRe.MatchString(a.Key) {
				return "", fmt.Errorf("invalid Jira key %q (want e.g. ABC-123)", a.Key)
			}
			url := strings.TrimRight(base, "/") + "/rest/api/3/issue/" + a.Key + "?fields=summary,status,description"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(email+":"+token)))
			req.Header.Set("Accept", "application/json")
			resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
			if err != nil {
				return "", fmt.Errorf("jira request: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if resp.StatusCode >= 400 {
				return "", fmt.Errorf("jira API returned %d", resp.StatusCode)
			}
			var it struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  struct {
						Name string `json:"name"`
					} `json:"status"`
				} `json:"fields"`
			}
			if err := json.Unmarshal(body, &it); err != nil {
				return "", fmt.Errorf("parse response: %w", err)
			}
			return fmt.Sprintf("%s [%s] %s", it.Key, it.Fields.Status.Name, it.Fields.Summary), nil
		},
	}
}
