// Package update implements an opt-in "a newer release is available" check that
// compares the running version to the latest GitHub release. Every failure path
// is silent so the check can never block or clutter startup.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Check compares current against the latest release tag from fetchLatest. It
// returns a one-line upgrade notice and true when latest is strictly newer.
// Dev builds and any fetch/parse failure yield ("", false).
func Check(current string, fetchLatest func() (string, error)) (string, bool) {
	if current == "" || current == "dev" {
		return "", false
	}
	latest, err := fetchLatest()
	if err != nil || latest == "" {
		return "", false
	}
	if compareSemver(latest, current) <= 0 {
		return "", false
	}
	notice := fmt.Sprintf("A new gophermind release is available: %s → %s\n  upgrade: brew upgrade gophermind  (or see the GitHub releases)",
		normalize(current), normalize(latest))
	return notice, true
}

// normalize strips a leading "v" from a version/tag.
func normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// compareSemver compares dotted numeric versions (ignoring a leading "v" and any
// pre-release suffix). Missing components count as 0. Returns -1, 0, or 1.
func compareSemver(a, b string) int {
	pa := parts(a)
	pb := parts(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parts extracts up to three numeric version components.
func parts(v string) [3]int {
	v = normalize(v)
	// Drop any pre-release/build metadata (e.g. 1.2.3-rc1+meta).
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, seg := range strings.Split(v, ".") {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(seg))
		out[i] = n
	}
	return out
}

// LatestFromGitHub returns the latest release tag for a "owner/repo" using the
// public GitHub API, with a short timeout so it never stalls startup.
func LatestFromGitHub(repo string) (string, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.TagName, nil
}
