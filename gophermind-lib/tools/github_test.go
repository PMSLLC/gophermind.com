package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubListIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header: %q", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.URL.Path, "/repos/o/r/issues") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`[{"number":7,"title":"a bug","state":"open","html_url":"http://x/7"}]`))
	}))
	defer srv.Close()

	out, err := run(t, GitHubTool(srv.URL, "tok"), `{"action":"list_issues","repo":"o/r"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#7", "a bug", "open"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestGitHubGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"number":7,"title":"a bug","state":"open","body":"details here","html_url":"http://x/7"}`))
	}))
	defer srv.Close()

	out, err := run(t, GitHubTool(srv.URL, "tok"), `{"action":"get_issue","repo":"o/r","number":7}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "details here") {
		t.Errorf("issue body missing:\n%s", out)
	}
}

func TestGitHubNoToken(t *testing.T) {
	if _, err := run(t, GitHubTool("https://api.github.com", ""), `{"action":"list_issues","repo":"o/r"}`); err == nil {
		t.Error("missing token should error")
	}
}

func TestGitHubBadRepo(t *testing.T) {
	if _, err := run(t, GitHubTool("https://api.github.com", "t"), `{"action":"list_issues","repo":"notaslug"}`); err == nil {
		t.Error("invalid repo slug should error")
	}
}
