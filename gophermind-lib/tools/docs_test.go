package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsLookupFetchesAndCaches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.URL.Path, "chi") || !strings.Contains(r.URL.RawQuery+r.URL.Path, "5.0.0") {
			t.Errorf("unexpected docs URL: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte("<html><body>chi router docs: NewRouter()</body></html>"))
	}))
	defer srv.Close()

	tmpl := srv.URL + "/{lib}/{version}"
	cacheDir := t.TempDir()

	out, err := run(t, DocsLookup(tmpl, cacheDir), `{"library":"chi","version":"5.0.0"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NewRouter") {
		t.Errorf("docs content missing:\n%s", out)
	}
	// Second lookup should be served from cache (no new hit).
	if _, err := run(t, DocsLookup(tmpl, cacheDir), `{"library":"chi","version":"5.0.0"}`); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("second lookup should hit cache; server hits=%d", hits)
	}
}

func TestDocsLookupDefaultsVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("docs latest"))
	}))
	defer srv.Close()
	out, err := run(t, DocsLookup(srv.URL+"/{lib}/{version}", t.TempDir()), `{"library":"chi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "docs") {
		t.Errorf("expected docs content:\n%s", out)
	}
}

func TestDocsLookupEmptyLibrary(t *testing.T) {
	if _, err := run(t, DocsLookup("http://x/{lib}", t.TempDir()), `{"library":""}`); err == nil {
		t.Error("empty library should error")
	}
}
