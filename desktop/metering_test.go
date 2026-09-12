package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/internal/freellm"
	"gophermind/internal/llm"
)

// Every turn this app serves spends a free-tier allowance, and until now
// nothing recorded it. The only production writer in the tree was the CLI's
// one-shot run/ask path, so the model picker read an odometer that was
// permanently zero: every model displayed its full allowance forever, and
// cycle-on-capacity could never fire because nothing ever approached
// capacity.
//
// This drives a real session turn through the embedded server and asserts
// the odometer moved.
func TestSessionTurnRecordsUsage(t *testing.T) {
	script := &scriptedLLM{turns: [][]byte{finalResp("turn complete")}}
	llmSrv := httptest.NewServer(script.handler())
	defer llmSrv.Close()

	odoPath := filepath.Join(t.TempDir(), "odometer.json")
	t.Setenv("GOPHERMIND_ODOMETER", odoPath)
	t.Setenv("GOPHERMIND_BASE_URL", llmSrv.URL)
	t.Setenv("GOPHERMIND_MODEL", "gpt-oss-120b")
	t.Setenv("GOPHERMIND_ROOT", t.TempDir())
	isolate(t)
	t.Setenv("GOPHERMIND_APPROVAL", "auto")

	// A populated holder is how this test names the profile the turn runs
	// on; startEmbeddedServer's env-driven resolution has no seam for it.
	holder := &clientHolder{}
	holder.Set(llm.New(llmSrv.URL, "", "gpt-oss-120b", 30*time.Second, false), "free-ovhcloud")

	srv := startTestServer(t, holder)

	createReq, _ := http.NewRequest(http.MethodPost, srv.BaseURL+"/session", nil)
	createReq.Header.Set("Authorization", "Bearer "+srv.Token)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	decErr := json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()
	if decErr != nil {
		t.Fatalf("decode /session: %v", decErr)
	}

	streamReq, _ := http.NewRequest(http.MethodPost,
		srv.BaseURL+"/session/"+created.ID+"/stream", strings.NewReader("say hello"))
	streamReq.Header.Set("Authorization", "Bearer "+srv.Token)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("POST /session/{id}/stream: %v", err)
	}
	// Drain the stream so the turn completes before the assertion.
	_, _ = io.Copy(io.Discard, streamResp.Body)
	streamResp.Body.Close()

	o, err := freellm.LoadOdometer(odoPath)
	if err != nil {
		t.Fatalf("load odometer: %v", err)
	}
	if o.Requests == 0 {
		t.Fatal("odometer recorded no request for a completed session turn; " +
			"the model picker's remaining-usage figures would stay at full " +
			"forever and cycle-on-capacity could never fire")
	}
	if got := o.ModelReading("free-ovhcloud", "gpt-oss-120b").Requests; got == 0 {
		t.Error("usage was not attributed to the model that served the turn")
	}
}
