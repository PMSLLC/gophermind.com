package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "Basic ") {
			t.Errorf("expected basic auth, got %q", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.URL.Path, "/rest/api/3/issue/ABC-1") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`{"key":"ABC-1","fields":{"summary":"login broken","status":{"name":"In Progress"}}}`))
	}))
	defer srv.Close()

	out, err := run(t, JiraTool(srv.URL, "me@x.com", "tok"), `{"action":"get_issue","key":"ABC-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ABC-1", "login broken", "In Progress"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestJiraNotConfigured(t *testing.T) {
	if _, err := run(t, JiraTool("https://x.atlassian.net", "", ""), `{"action":"get_issue","key":"A-1"}`); err == nil {
		t.Error("missing credentials should error")
	}
}

func TestJiraBadKey(t *testing.T) {
	if _, err := run(t, JiraTool("https://x.atlassian.net", "e", "t"), `{"action":"get_issue","key":"../evil"}`); err == nil {
		t.Error("invalid issue key should be rejected")
	}
}
