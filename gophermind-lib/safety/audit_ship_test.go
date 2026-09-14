package safety

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAuditShipperCalled(t *testing.T) {
	var mu sync.Mutex
	var shipped []AuditEntry
	al := NewAuditLog(filepath.Join(t.TempDir(), "a.jsonl"))
	al.SetShipper(func(e AuditEntry) {
		mu.Lock()
		shipped = append(shipped, e)
		mu.Unlock()
	})
	al.Record("write_file", `{"path":"x"}`, "approved", "ok")
	al.Record("run_shell", `{"command":"ls"}`, "auto", "out")

	mu.Lock()
	defer mu.Unlock()
	if len(shipped) != 2 {
		t.Fatalf("expected 2 shipped entries, got %d", len(shipped))
	}
	if shipped[0].Tool != "write_file" {
		t.Errorf("wrong shipped entry: %+v", shipped[0])
	}
}

func TestHTTPShipperPosts(t *testing.T) {
	got := make(chan AuditEntry, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var e AuditEntry
		json.Unmarshal(body, &e)
		got <- e
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ship := HTTPShipper(srv.URL)
	ship(AuditEntry{Seq: 1, Tool: "write_file"})

	select {
	case e := <-got:
		if e.Tool != "write_file" {
			t.Errorf("shipped entry wrong: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("collector never received the shipped entry")
	}
}
