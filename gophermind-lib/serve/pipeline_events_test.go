package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

type frame struct {
	id, event, data string
}

// readFrame reads one frame, skipping comment-only frames (keepalives).
func readFrame(t *testing.T, br *bufio.Reader) frame {
	t.Helper()
	var f frame
	has := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			if has {
				return f
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			f.id, has = strings.TrimPrefix(line, "id: "), true
		case strings.HasPrefix(line, "event: "):
			f.event, has = strings.TrimPrefix(line, "event: "), true
		case strings.HasPrefix(line, "data: "):
			if f.data != "" {
				f.data += "\n"
			}
			f.data += strings.TrimPrefix(line, "data: ")
			has = true
		}
	}
}

func openStream(t *testing.T, srv *httptest.Server, path string, hdr map[string]string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer t")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body)
}

func TestEventsReplayFromZeroWithIDs(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	v, _ := runview.Open(root, runviewtest.RunID)
	n := v.EventSeq()

	resp, br := openStream(t, srv, "/pipeline/events?since=0", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") || resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Fatalf("status %d headers %v", resp.StatusCode, resp.Header)
	}
	for i := int64(1); i <= n; i++ {
		f := readFrame(t, br)
		if f.id != strconv.FormatInt(i, 10) || !strings.HasPrefix(f.event, "node-") {
			t.Fatalf("frame %d = %+v", i, f)
		}
		if !json.Valid([]byte(f.data)) {
			t.Fatalf("frame %d data is not JSON: %q", i, f.data)
		}
	}
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	v, _ := runview.Open(root, runviewtest.RunID)
	n := v.EventSeq()
	_, br := openStream(t, srv, "/pipeline/events", map[string]string{"Last-Event-ID": strconv.FormatInt(n-1, 10)})
	f := readFrame(t, br)
	if f.id != strconv.FormatInt(n, 10) {
		t.Errorf("resumed at id %s, want %d (the line after Last-Event-ID)", f.id, n)
	}
}

func TestEventsWithoutSinceFollowFromTheEnd(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	oldPoll := pipelinePoll
	pipelinePoll = 10 * time.Millisecond
	t.Cleanup(func() { pipelinePoll = oldPoll })
	srv := apiServer(t, root)
	_, br := openStream(t, srv, "/pipeline/events", nil)
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	fr := readFrame(t, br)
	var d runview.NodeStatusData
	_ = json.Unmarshal([]byte(fr.data), &d)
	if fr.event != "node-status" || d.ID != "fn-bye" || d.Status != "verified" {
		t.Errorf("first live frame = %+v", fr)
	}
}

func TestEventsRejectBadSinceUnknownRunAndMissingToken(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	for path, want := range map[string]int{
		"/pipeline/events?since=abc": 400, "/pipeline/events?since=-1": 400,
		"/pipeline/events?run=gm-2026-10-09-009": 404, "/pipeline/events?run=..%2Fx": 400,
	} {
		resp, _ := apiGet(t, srv, path, "t")
		if resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, resp.StatusCode, want)
		}
	}
	if resp, _ := apiGet(t, srv, "/pipeline/events", ""); resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

func TestEventsKeepaliveComment(t *testing.T) {
	old := pipelineKeepalive
	pipelineKeepalive = 30 * time.Millisecond
	t.Cleanup(func() { pipelineKeepalive = old })
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	_, br := openStream(t, srv, "/pipeline/events", nil)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ": keepalive") {
		t.Fatalf("first line = %q, %v; want a keepalive comment", line, err)
	}
}

func TestEventsStreamCapAndSlotRelease(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root, MaxStreams: 2}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r1, _ := openStream(t, srv, "/pipeline/events", nil)
	_, _ = openStream(t, srv, "/pipeline/events", nil)
	r3, _ := openStream(t, srv, "/pipeline/events", nil)
	if r3.StatusCode != http.StatusServiceUnavailable || r3.Header.Get("Retry-After") == "" || r3.Header.Get(PipelineSchemaHeader) != "1" {
		t.Fatalf("third stream: %d %v, want 503 with Retry-After", r3.StatusCode, r3.Header)
	}
	r1.Body.Close() // a client goes away: its slot must come back
	deadline := time.Now().Add(3 * time.Second)
	for {
		r4, _ := openStream(t, srv, "/pipeline/events", nil)
		if r4.StatusCode == 200 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released after a disconnect, last status %d", r4.StatusCode)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPipelineMaxStreamsFromEnv(t *testing.T) {
	for v, want := range map[string]int{"": 0, "x": 0, "0": 0, "-3": 0, "4": 4} {
		t.Setenv("GOPHERMIND_PIPELINE_MAX_STREAMS", v)
		if got := PipelineMaxStreamsFromEnv(); got != want {
			t.Errorf("%q -> %d, want %d", v, got, want)
		}
	}
}
