package session

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPushThenPull(t *testing.T) {
	var mu sync.Mutex
	store := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.URL.Path
		switch r.Method {
		case http.MethodPut:
			body := make([]byte, r.ContentLength)
			r.Body.Read(body)
			store[key] = body
			w.WriteHeader(200)
		case http.MethodGet:
			if data, ok := store[key]; ok {
				w.Write(data)
			} else {
				http.Error(w, "not found", 404)
			}
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeSessionLines(t, dir, "s1", `{"role":"user","content":"hello remote"}`)

	if err := pushRemoteIn(dir, "s1", srv.URL); err != nil {
		t.Fatal(err)
	}
	// Pull into a fresh local dir.
	dir2 := t.TempDir()
	if err := pullRemoteIn(dir2, "s1", srv.URL); err != nil {
		t.Fatal(err)
	}
	data := readLines(t, dir2, "s1")
	if !strings.Contains(strings.Join(data, "\n"), "hello remote") {
		t.Errorf("pulled session content wrong: %v", data)
	}
	_ = filepath.Join
}

func TestPullMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	if err := pullRemoteIn(t.TempDir(), "nope", srv.URL); err == nil {
		t.Error("pulling a missing session should error")
	}
}

func TestPushBadID(t *testing.T) {
	if err := pushRemoteIn(t.TempDir(), "../evil", "http://x"); err == nil {
		t.Error("bad id should be rejected")
	}
}
