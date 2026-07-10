package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyPostsMessage(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var j map[string]any
		json.NewDecoder(r.Body).Decode(&j)
		if txt, _ := j["text"].(string); txt != "" {
			gotBody = txt
		}
		if txt, _ := j["content"].(string); txt != "" {
			gotBody = txt
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	out, err := run(t, Notify(srv.URL), `{"message":"deploy finished"}`)
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != "deploy finished" {
		t.Errorf("posted message = %q", gotBody)
	}
	if !strings.Contains(strings.ToLower(out), "sent") {
		t.Errorf("expected confirmation, got %q", out)
	}
}

func TestNotifyNoWebhook(t *testing.T) {
	if _, err := run(t, Notify(""), `{"message":"x"}`); err == nil {
		t.Error("missing webhook URL should error")
	}
}

func TestNotifyEmptyMessage(t *testing.T) {
	if _, err := run(t, Notify("https://hooks.example.com/x"), `{"message":"  "}`); err == nil {
		t.Error("empty message should error")
	}
}
