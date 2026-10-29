package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStableSessionIDIsStablePerDirectory(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if StableSessionID(a) != StableSessionID(a) {
		t.Errorf("same directory produced different ids")
	}
	if StableSessionID(a) == StableSessionID(b) {
		t.Errorf("different directories produced the same id")
	}
	id := StableSessionID(a)
	if len(id) == 0 || len(id) > 128 {
		t.Errorf("id length %d out of range", len(id))
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			t.Errorf("id %q has non-printable or space rune %q", id, r)
		}
	}
}

func TestCloseSessionDeletesUnderChatRoot(t *testing.T) {
	cases := []struct {
		name, chatPath, wantPath string
	}{
		{"default chat path", "", "/v1/sessions/abc"},
		{"base url already has v1", "/chat/completions", "/sessions/abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			c := New(srv.URL, "k", "m", 0, false)
			c.ChatPath = tc.chatPath
			if err := c.CloseSession(context.Background(), "abc"); err != nil {
				t.Fatalf("CloseSession: %v", err)
			}
			if gotMethod != http.MethodDelete || gotPath != tc.wantPath {
				t.Errorf("got %s %s, want DELETE %s", gotMethod, gotPath, tc.wantPath)
			}
			if gotAuth != "Bearer k" {
				t.Errorf("Authorization = %q, want Bearer k", gotAuth)
			}
		})
	}
}

func TestCloseSessionReportsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := New(srv.URL, "", "m", 0, false)
	if err := c.CloseSession(context.Background(), "abc"); err == nil {
		t.Errorf("want an error for a 404, got nil")
	}
}
