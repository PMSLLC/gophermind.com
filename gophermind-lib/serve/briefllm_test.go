//go:build !windows

package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const llmRun = "csvstat-v1-20261101-r001"

// llmRunFolder makes the run's ledger folder with two calls; text is kept for
// call 1 only, and the verbose marker is written when verbose is set.
func llmRunFolder(t *testing.T, e *briefEnv, verbose bool) {
	t.Helper()
	writeRun(t, e, llmRun, "csvstat", "v1", "done", "2026-11-01T10:00:00Z")
	state := filepath.Join(e.root, ".gophermind", llmRun, "_state")
	if err := os.MkdirAll(filepath.Join(state, "llm"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := `{"kind":"call","id":1,"stage":"implement:fn-a","node_id":"fn-a","provider":"local","model_served":"gemma","prompt_tokens":10,"completion_tokens":5,"duration_ms":1200,"outcome":"ok"}` + "\n" +
		`{"kind":"call","id":2,"stage":"plan","provider":"local","model_served":"gemma","duration_ms":50,"outcome":"error","error_kind":"timeout"}` + "\n"
	if err := os.WriteFile(filepath.Join(state, "calls.jsonl"), []byte(calls), 0o644); err != nil {
		t.Fatal(err)
	}
	text := `{"id":1,"stage":"implement:fn-a","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"write it"}],"response":"done"}`
	if err := os.WriteFile(filepath.Join(state, "llm", "1.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if verbose {
		if err := os.WriteFile(filepath.Join(state, "verbose"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type llmReply struct {
	Verbose bool `json:"verbose"`
	Calls   []struct {
		ID          int64  `json:"id"`
		Stage       string `json:"stage"`
		NodeID      string `json:"node_id"`
		ModelServed string `json:"model_served"`
		Outcome     string `json:"outcome"`
		ErrorKind   string `json:"error_kind"`
		HasText     bool   `json:"has_text"`
	} `json:"calls"`
}

func TestLLMListVerbose(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, true)
	w := e.get("/app/briefs/" + llmRun + "/llm")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got llmReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Verbose || len(got.Calls) != 2 {
		t.Fatalf("reply %+v", got)
	}
	c1, c2 := got.Calls[0], got.Calls[1]
	if c1.ID != 1 || c1.Stage != "implement:fn-a" || c1.NodeID != "fn-a" || c1.ModelServed != "gemma" || !c1.HasText {
		t.Errorf("call 1 %+v", c1)
	}
	if c2.ID != 2 || c2.HasText || c2.ErrorKind != "timeout" || c2.Outcome != "error" {
		t.Errorf("call 2 %+v", c2)
	}
}

func TestLLMListNotVerbose(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, false)
	w := e.get("/app/briefs/" + llmRun + "/llm")
	var got llmReply
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got.Verbose {
		t.Error("verbose without a marker")
	}
}

func TestLLMListSeesServerSideMarker(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, false)
	m := e.manager()
	if err := writeVerboseMarker(m.runDir(llmRun)); err != nil {
		t.Fatal(err)
	}
	var got llmReply
	w := e.get("/app/briefs/" + llmRun + "/llm")
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || !got.Verbose {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestLLMCallText(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, true)
	w := e.get("/app/briefs/" + llmRun + "/llm/1")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		ID       int64 `json:"id"`
		Messages []struct{ Role, Content string }
		Response string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 || len(got.Messages) != 2 || got.Messages[1].Content != "write it" || got.Response != "done" {
		t.Errorf("text %+v", got)
	}
}

func TestLLMBadRequests(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, true)
	for _, p := range []string{
		"/app/briefs/" + llmRun + "/llm/2",      // no text kept
		"/app/briefs/" + llmRun + "/llm/0",      // not positive
		"/app/briefs/" + llmRun + "/llm/-1",     //
		"/app/briefs/" + llmRun + "/llm/1.json", // not an integer
		"/app/briefs/" + llmRun + "/llm/..",
		"/app/briefs/" + llmRun + "/llm/abc",
		"/app/briefs/nope-v1-20261101-r009/llm",
		"/app/briefs/nope-v1-20261101-r009/llm/1",
		"/app/briefs/..%2f..%2fetc/llm",
		"/app/briefs/..%2fx/llm/1",
		"/app/briefs/BAD_ID/llm",
	} {
		if w := e.get(p); w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest && w.Code != http.StatusTemporaryRedirect { // the mux cleans ".." itself
			t.Errorf("GET %s = %d %s, want 404 or 400", p, w.Code, w.Body)
		}
	}
	if w := e.get("/app/briefs/" + llmRun + "/llm/2"); w.Code != http.StatusNotFound {
		t.Errorf("missing text = %d, want 404", w.Code)
	}
}

func TestLLMRoutesNeedSignIn(t *testing.T) {
	e := newBriefEnv(t)
	llmRunFolder(t, e, true)
	for _, p := range []string{"/llm", "/llm/1"} {
		w := do(e.h, "GET", "/app/briefs/"+llmRun+p, nil)
		if w.Code == http.StatusOK {
			t.Errorf("GET %s without a session = 200", p)
		}
	}
}

func TestLogPage(t *testing.T) {
	e := newBriefEnv(t)
	w := e.get("/app/briefs/" + llmRun + "/log")
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Content-Security-Policy", "started without the verbose option", `class="rail"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "innerHTML") {
		t.Error("page sets innerHTML")
	}
}

func TestRunPageLinksToTheVerboseLog(t *testing.T) {
	b, err := pagesAssets.ReadFile("assets/brief.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Verbose log", "run.verbose", "/log"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("run page lacks %q", want)
		}
	}
}

func TestStateCarriesVerbose(t *testing.T) {
	e := newBriefEnv(t)
	id := llmRun
	dir := filepath.Join(e.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(briefRecord{ID: id, Phase: "done", Verbose: true})
	if err := os.WriteFile(filepath.Join(dir, "run.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	w := e.get("/app/briefs/" + id + "/state")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"verbose":true`) {
		t.Errorf("%d %.300s", w.Code, w.Body)
	}
}
