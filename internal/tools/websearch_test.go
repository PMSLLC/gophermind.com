package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchParsesResults(t *testing.T) {
	var gotQuery, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotToken = r.Header.Get("X-Subscription-Token")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[
			{"title":"Go docs","url":"https://go.dev","description":"The Go language."},
			{"title":"Rust","url":"https://rust-lang.org","description":"Systems lang."}
		]}}`))
	}))
	defer srv.Close()

	tool := WebSearch(srv.URL, "secret-key", nil)
	out, err := run(t, tool, `{"query":"go generics","count":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "go generics" {
		t.Errorf("query forwarded = %q", gotQuery)
	}
	if gotToken != "secret-key" {
		t.Errorf("token header = %q", gotToken)
	}
	for _, want := range []string{"Go docs", "https://go.dev", "The Go language.", "Rust"} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q:\n%s", want, out)
		}
	}
}

func TestWebSearchRequiresKey(t *testing.T) {
	tool := WebSearch("https://api.example.com", "", nil)
	if _, err := run(t, tool, `{"query":"x"}`); err == nil {
		t.Error("missing API key should error")
	}
}

func TestWebSearchRequiresQuery(t *testing.T) {
	tool := WebSearch("https://api.example.com", "k", nil)
	if _, err := run(t, tool, `{"query":"  "}`); err == nil {
		t.Error("empty query should error")
	}
}

func TestWebSearchNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()
	out, err := run(t, WebSearch(srv.URL, "k", nil), `{"query":"zzz"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no results") {
		t.Errorf("expected a no-results message: %q", out)
	}
}

func TestWebSearchRerank(t *testing.T) {
	// Brave returns an off-topic result first, then the relevant one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"web":{"results":[
			{"title":"unrelated database stuff","url":"http://a","description":"database database"},
			{"title":"the network guide","url":"http://b","description":"network network"}
		]}}`))
	}))
	defer srv.Close()

	out, err := run(t, WebSearch(srv.URL, "k", fakeEmbed{}), `{"query":"network"}`)
	if err != nil {
		t.Fatal(err)
	}
	// After reranking for "network", http://b should come before http://a.
	ib := strings.Index(out, "http://b")
	ia := strings.Index(out, "http://a")
	if ib < 0 || ia < 0 || ib > ia {
		t.Errorf("network result should be ranked first after rerank:\n%s", out)
	}
}
