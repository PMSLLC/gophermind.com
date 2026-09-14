package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// repoSlugRe validates an "owner/repo" slug.
var repoSlugRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// GitHubTool returns a read-only GitHub tool (list issues/PRs, get an issue) via
// the REST API, authenticated with a token. apiBase is the API root (overridable
// for testing); it works against api.github.com when a token is configured.
func GitHubTool(apiBase, token string) Tool {
	return Tool{
		Name:        "github",
		Description: "Read GitHub repository data via the API: list_issues, list_prs, or get_issue for an owner/repo. Read-only; requires a token.",
		Schema: object(map[string]any{
			"action": str("One of: list_issues, list_prs, get_issue."),
			"repo":   str("Repository as owner/repo."),
			"number": map[string]any{"type": "integer", "description": "Issue/PR number (for get_issue)."},
		}, "action", "repo"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Action string `json:"action"`
				Repo   string `json:"repo"`
				Number int    `json:"number"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if token == "" {
				return "", fmt.Errorf("github is not configured: set GITHUB_TOKEN")
			}
			if !repoSlugRe.MatchString(a.Repo) {
				return "", fmt.Errorf("repo must be owner/repo, got %q", a.Repo)
			}
			base := strings.TrimRight(apiBase, "/")

			switch a.Action {
			case "list_issues", "list_prs":
				path := "/issues?state=open&per_page=20"
				if a.Action == "list_prs" {
					path = "/pulls?state=open&per_page=20"
				}
				body, err := githubGet(ctx, base+"/repos/"+a.Repo+path, token)
				if err != nil {
					return "", err
				}
				var items []struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
					State  string `json:"state"`
					URL    string `json:"html_url"`
				}
				if err := json.Unmarshal(body, &items); err != nil {
					return "", fmt.Errorf("parse response: %w", err)
				}
				var b strings.Builder
				for _, it := range items {
					fmt.Fprintf(&b, "#%d [%s] %s\n   %s\n", it.Number, it.State, it.Title, it.URL)
				}
				if b.Len() == 0 {
					b.WriteString("(none)\n")
				}
				return b.String(), nil
			case "get_issue":
				if a.Number <= 0 {
					return "", fmt.Errorf("get_issue requires a positive number")
				}
				body, err := githubGet(ctx, fmt.Sprintf("%s/repos/%s/issues/%d", base, a.Repo, a.Number), token)
				if err != nil {
					return "", err
				}
				var it struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
					State  string `json:"state"`
					Body   string `json:"body"`
					URL    string `json:"html_url"`
				}
				if err := json.Unmarshal(body, &it); err != nil {
					return "", fmt.Errorf("parse response: %w", err)
				}
				return fmt.Sprintf("#%d [%s] %s\n%s\n\n%s", it.Number, it.State, it.Title, it.URL, it.Body), nil
			default:
				return "", fmt.Errorf("unknown action %q (use list_issues, list_prs, or get_issue)", a.Action)
			}
		},
	}
}

// githubGet performs an authenticated GET and returns the body (size-limited).
func githubGet(ctx context.Context, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gophermind")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}
	return body, nil
}
