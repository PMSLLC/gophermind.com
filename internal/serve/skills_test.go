package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillsServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	root, cfgDir := t.TempDir(), t.TempDir()
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Skills: &SkillsDeps{Root: root, ConfigDir: cfgDir}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, root, cfgDir
}

func req(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	var r *http.Request
	var err error
	if body == "" {
		r, err = http.NewRequest(method, url, nil)
	} else {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
	}
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// These routes clone repositories and change what reaches an agent's system
// prompt. They must be behind the same auth as everything else.
func TestSkillRoutesRequireAToken(t *testing.T) {
	srv, _, _ := skillsServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/skills", ""},
		{"PATCH", "/skills", `{"key":"a/b:c","enabled":true}`},
		{"POST", "/skills/sources", `{"url":"https://github.com/a/b"}`},
		{"DELETE", "/skills/sources/a/b", ""},
	} {
		resp := req(t, tc.method, srv.URL+tc.path, "", tc.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s returned %d without a token, want 401", tc.method, tc.path, resp.StatusCode)
		}
	}
}

// A fresh install has no sources and nothing enabled.
func TestSkillsListIsEmptyOnAFreshInstall(t *testing.T) {
	srv, _, _ := skillsServer(t)
	resp := req(t, "GET", srv.URL+"/skills", "t", "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var got struct {
		Sources []any `json:"sources"`
		Skills  []any `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 0 || len(got.Skills) != 0 {
		t.Errorf("fresh install is not empty: %+v", got)
	}
}

// A repo-local pack is always on, and offering a toggle for it would put a
// control in the UI that does nothing on the next run.
func TestRepoLocalSkillCannotBeToggled(t *testing.T) {
	srv, root, _ := skillsServer(t)
	dir := filepath.Join(root, ".gophermind", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "house.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := req(t, "PATCH", srv.URL+"/skills", "t", `{"key":"house","enabled":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("got %d, want 400 for a repo-local toggle", resp.StatusCode)
	}
}

// The URL is validated before anything reaches the network, so a transport
// like ext:: never becomes a git invocation.
func TestAddSourceRejectsANonHTTPSURL(t *testing.T) {
	srv, _, _ := skillsServer(t)
	for _, bad := range []string{
		`{"url":"ext::sh -c 'touch /tmp/pwned'"}`,
		`{"url":"file:///etc"}`,
		`{"url":"git@github.com:a/b.git"}`,
		`{"url":"http://github.com/a/b"}`,
	} {
		resp := req(t, "POST", srv.URL+"/skills/sources", "t", bad)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", bad, resp.StatusCode)
		}
	}
}

// Toggling writes through and is visible on the next read.
func TestToggleRoundTrips(t *testing.T) {
	srv, _, cfgDir := skillsServer(t)
	cache := filepath.Join(cfgDir, "skill-cache", "acme", "pack@abc123")
	if err := os.MkdirAll(filepath.Join(cache, "tdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: tdd\ndescription: d\n---\nbody text\n"
	if err := os.WriteFile(filepath.Join(cache, "tdd", "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := req(t, "PATCH", srv.URL+"/skills", "t", `{"key":"acme/pack:tdd","enabled":true}`)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("toggle returned %d", resp.StatusCode)
	}

	resp = req(t, "GET", srv.URL+"/skills", "t", "")
	defer resp.Body.Close()
	var got struct {
		Skills []struct {
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || !got.Skills[0].Enabled || got.Skills[0].Key != "acme/pack:tdd" {
		t.Fatalf("toggle did not persist: %+v", got.Skills)
	}
}

// Nil Skills deps means the routes are simply absent, matching every other
// optional route's convention.
func TestSkillRoutesAbsentWhenUnconfigured(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp := req(t, "GET", srv.URL+"/skills", "t", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
}

// Traversal is refused at the route as well as in the package, so the API
// surface cannot be used to reach os.RemoveAll outside the cache.
func TestAddSourceRejectsTraversalURL(t *testing.T) {
	srv, _, _ := skillsServer(t)
	for _, bad := range []string{
		`{"url":"https://evil.com/../.."}`,
		`{"url":"https://evil.com/../../etc/passwd"}`,
	} {
		resp := req(t, "POST", srv.URL+"/skills/sources", "t", bad)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", bad, resp.StatusCode)
		}
	}
}
