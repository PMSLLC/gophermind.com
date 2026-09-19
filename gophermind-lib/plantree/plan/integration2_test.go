package plan

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// TestPass1ThenPass2EndToEndOverHTTP runs both passes through the real
// completer against a fake model server: the brief becomes a skeleton, every
// step gets a specification, and only approval is left.
func TestPass1ThenPass2EndToEndOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if strings.Contains(body, "Steps to specify now") { // pass 2 bodies carry brief text, so test them first
			writeSSE(w, "```json\n"+pass2Reply(body)+"\n```")
			return
		}
		reply, err := byChunk(0, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSSE(w, reply)
	}))
	defer srv.Close()

	dir := t.TempDir()
	repo := plantree.Open(dir)
	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	if _, err := RunPass1(context.Background(), repo, threePartBrief, c, opts); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result2{Tasks: 3, Steps: 3, Passes: 3}) {
		t.Errorf("Result2 = %+v", res)
	}
	if got := actionKinds(t, repo); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-002.step-001", "phase-002.task-001.step-001"} {
		n, err := repo.Get(id)
		if err != nil || n.Work == nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %+v, %v", id, n, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 6 {
		t.Fatalf("%d requests, want 6 (three per pass)", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d carries tools", i)
		}
	}
	first := bodies[3] // the first pass-2 request: task 1, whose brief is chunk 1
	if !strings.Contains(first, "first part text") || strings.Contains(first, "second part text") || strings.Contains(first, "third part text") {
		t.Error("the first pass-2 request must carry only the brief chunk that produced its task")
	}
}
