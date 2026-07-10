package tui

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"gophermind/internal/agent"
	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// streamingServer returns an httptest server that streams the given fragments
// as SSE prose and then finishes.
func streamingServer(t *testing.T, frags ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // /v1/models discovery (unused here)
			fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frag := range frags {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", frag)
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
}

func TestTUIEndToEnd(t *testing.T) {
	srv := streamingServer(t, "Hello ", "from ", "the ", "model.")
	defer srv.Close()

	m := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent {
		client := llm.New(srv.URL, "", "m", 5*time.Second, false)
		reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
		approve := func(tool, args string) bool { return true }
		onEvent := func(e agent.Event) {
			if msg := eventToMsg(e); msg != nil {
				sub <- msg
			}
		}
		return agent.New(client, reg, 25, approve, onEvent)
	}, "m", "auto", "dark", false)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Type("do something")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The streamed answer must appear in the rendered output.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Hello from the model."))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
