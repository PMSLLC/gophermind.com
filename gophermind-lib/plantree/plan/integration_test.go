package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// writeSSE streams reply as SSE events of at most 20 bytes of ASCII content.
func writeSSE(w http.ResponseWriter, reply string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for len(reply) > 0 {
		n := min(20, len(reply))
		piece, _ := json.Marshal(reply[:n])
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", piece)
		reply = reply[n:]
	}
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
}

func TestRunPass1EndToEndOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		switch {
		case strings.Contains(body, "Compress this project overview"):
			writeSSE(w, "short overview")
		case strings.Contains(body, "first part text"):
			writeSSE(w, "Sure, {here it is:\n```json\n"+reply1+"\n```\nHope that helps.")
		case strings.Contains(body, "second part text"):
			if strings.Contains(body, "rejected") {
				writeSSE(w, reply2)
			} else {
				writeSSE(w, "Sorry, no JSON here.")
			}
		case strings.Contains(body, "third part text"):
			writeSSE(w, strings.Replace(reply3, `"overview 3"`, `"`+strings.Repeat("o", 200)+`"`, 1))
		default:
			http.Error(w, "unknown chunk", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	repo := plantree.Open(t.TempDir())
	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	o := Options{ProjectName: "demo", ChunkBytes: 30, OverviewCap: 100}
	if _, err := RunPass1(context.Background(), repo, threePartBrief, c, o); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, repo); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree = %v", got)
	}
	if ov, _ := ReadOverview(repo.Dir()); ov != "short overview\n" {
		t.Errorf("overview = %q", ov)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 5 {
		t.Fatalf("%d requests, want 5", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d carries tools", i)
		}
	}
	// Order: chunk 1, chunk 2, chunk 2 retry, chunk 3, compress.
	if strings.Contains(bodies[1], "first part text") || strings.Contains(bodies[2], "first part text") {
		t.Error("a chunk-2 request carries chunk-1 text")
	}
	if !strings.Contains(bodies[2], "Sorry, no JSON here.") {
		t.Error("the retry request must quote the rejected reply")
	}
}
