package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListModelsReturnsAllIDs(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[{"id":"model-a"},{"id":"model-b"},{"id":"model-c"}]}`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"model-a", "model-b", "model-c"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestListModelsSkipsEmptyIDs(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[{"id":""},{"id":"real"},{"id":""}]}`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(ids) != 1 || ids[0] != "real" {
		t.Errorf("ids = %v, want [real] (empty ids skipped)", ids)
	}
}

func TestListModelsEmptyData(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[]}`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestListModelsParseError(t *testing.T) {
	srv, _ := newModelsServer(t, `not json`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	if _, err := c.ListModels(context.Background()); err == nil {
		t.Fatal("ListModels: want error on unparseable body, got nil")
	}
}

func TestDiscoverModelReturnsFirst(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[{"id":"first"},{"id":"second"}]}`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	got, err := c.DiscoverModel(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModel: %v", err)
	}
	if got != "first" {
		t.Errorf("DiscoverModel = %q, want first", got)
	}
}

func TestDiscoverModelNoneServed(t *testing.T) {
	srv, _ := newModelsServer(t, `{"data":[]}`)
	c := New(srv.URL, "", "", 5*time.Second, false)

	if _, err := c.DiscoverModel(context.Background()); err == nil {
		t.Fatal("DiscoverModel: want error when endpoint serves no models, got nil")
	}
}

// TestListModelsSendsAuth confirms the bearer token is forwarded so a gated
// /v1/models endpoint is reachable during discovery/validation.
func TestListModelsSendsAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "secret-token", "", 5*time.Second, false)
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token")
	}
}
