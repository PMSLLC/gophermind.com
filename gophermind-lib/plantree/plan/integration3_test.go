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

// TestQuestionLoopEndToEndOverHTTP runs the whole question loop through the
// real completer against a fake model server: pass 1 asks a question, pass 2
// skips the held task, the owner answers, and a second pass 2 releases and
// specifies it with the decision and the project facts in its prompt.
func TestQuestionLoopEndToEndOverHTTP(t *testing.T) {
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
		reply, err := byChunkQ(0, body)
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

	res1, err := RunPass1(context.Background(), repo, threePartBrief, c, opts)
	if err != nil || res1.Questions != 1 {
		t.Fatalf("RunPass1: %+v, %v", res1, err)
	}
	if err := WriteFacts(repo, "FACTS: Go 1.25; test with go test ./..."); err != nil {
		t.Fatal(err)
	}
	res2, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil || res2.Steps != 2 || res2.Passes != 2 || res2.Released != 0 {
		t.Fatalf("first RunPass2: %+v, %v", res2, err)
	}
	if _, err := AnswerQuestion(repo, "q-001", Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	res3, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil || res3.Steps != 1 || res3.Released != 1 || res3.Passes != 1 {
		t.Fatalf("second RunPass2: %+v, %v", res3, err)
	}
	if got := actionKinds(t, repo); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 6 {
		t.Fatalf("%d requests, want 6 (three of pass 1, two then one of pass 2)", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d carries tools", i)
		}
	}
	for _, i := range []int{3, 4, 5} {
		if !strings.Contains(bodies[i], "FACTS: Go 1.25") {
			t.Errorf("pass-2 request %d must carry the project facts", i)
		}
	}
	// The request body is JSON, which writes ">" as the six characters \u003e
	// and a quote as backslash-quote. A raw string does not interpret escapes,
	// so the replacements below match those characters literally.
	decided := func(body string) bool {
		plain := strings.NewReplacer(`\u003e`, ">", `\"`, `"`).Replace(body)
		return strings.Contains(plain, `"Which framework?" -> A`)
	}
	if decided(bodies[3]) || decided(bodies[4]) {
		t.Error("the decision does not exist yet when the first two pass-2 requests are made")
	}
	if !decided(bodies[5]) {
		t.Error("the last request must carry the owner's decision")
	}
	// The held step belongs to task 2 of phase 1, which only the last request is
	// about. The first two pass-2 requests are about other tasks, so the step id
	// must not appear in them at all, and it must be listed under "Steps to
	// specify now" in the last one.
	if strings.Contains(bodies[3], "phase-001.task-002.step-001:") || strings.Contains(bodies[4], "phase-001.task-002.step-001:") {
		t.Error("the held task must not be specified before the answer")
	}
	last := bodies[5]
	if i := strings.Index(last, "Steps to specify now"); i < 0 || !strings.Contains(last[i:], "phase-001.task-002.step-001:") {
		t.Error("the released step must be listed under the steps to specify in the last request")
	}
}
