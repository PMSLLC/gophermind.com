//go:build !windows

package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sseFrame struct{ event, data string }

// openBriefStream GETs the events route of a real server and returns the frames as
// they arrive. cancel ends the request.
func openBriefStream(t *testing.T, e *briefEnv, srvURL, id string) (*http.Response, <-chan sseFrame, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srvURL+"/app/briefs/"+id+"/events", nil)
	req.AddCookie(e.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	frames := make(chan sseFrame, 64)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		var f sseFrame
		for sc.Scan() {
			ln := sc.Text()
			switch {
			case strings.HasPrefix(ln, "event: "):
				f.event = ln[len("event: "):]
			case strings.HasPrefix(ln, "data: "):
				f.data += ln[len("data: "):]
			case ln == "":
				if f.event != "" {
					frames <- f
				}
				f = sseFrame{}
			}
		}
	}()
	return resp, frames, cancel
}

func nextFrame(t *testing.T, ch <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame in 5s")
	}
	return sseFrame{}
}

func fastEvents(t *testing.T) {
	old := defaultEventEvery
	defaultEventEvery = 20 * time.Millisecond
	t.Cleanup(func() { defaultEventEvery = old })
}

func TestBriefEventsFirstChangeAndCancel(t *testing.T) {
	fastEvents(t)
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-021"
	e.startAsking(id)
	srv := httptest.NewServer(e.h)
	defer srv.Close()

	resp, frames, cancel := openBriefStream(t, e, srv.URL, id)
	defer cancel()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	first := nextFrame(t, frames)
	var st BriefState
	if first.event != "state" || json.Unmarshal([]byte(first.data), &st) != nil || st.Run.ID != id || st.Run.Phase != phaseWaiting {
		t.Fatalf("first frame %+v", first)
	}

	// Nothing changed: no second state frame within a few polls.
	select {
	case f := <-frames:
		t.Fatalf("unexpected frame without a change: %+v", f)
	case <-time.After(200 * time.Millisecond):
	}

	lf, err := os.OpenFile(filepath.Join(e.dir, id, "run.log"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	lf.WriteString("a new log line\n")
	lf.Close()
	second := nextFrame(t, frames)
	if second.event != "state" || !strings.Contains(second.data, "a new log line") {
		t.Fatalf("change frame %+v", second)
	}

	// Cancelling the request ends the stream and frees its slot.
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				goto closed
			}
			continue
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("stream still open after cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
closed:
}

func TestBriefEventsEndsWhenRunIsFinal(t *testing.T) {
	fastEvents(t)
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-022"
	e.post("/app/briefs", e.briefText(id), nil)
	e.waitPhase(id, phaseDone)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	_, frames, cancel := openBriefStream(t, e, srv.URL, id)
	defer cancel()
	if f := nextFrame(t, frames); f.event != "state" || !strings.Contains(f.data, `"phase":"done"`) {
		t.Fatalf("first %+v", f)
	}
	if f := nextFrame(t, frames); f.event != "end" {
		t.Fatalf("second %+v, want end", f)
	}
}

func TestBriefEventsCapAndRefusals(t *testing.T) {
	fastEvents(t)
	e := newBriefEnv(t)
	const id = "gm-2026-10-24-023"
	e.startAsking(id)
	srv := httptest.NewServer(e.h)
	defer srv.Close()

	var cancels []context.CancelFunc
	for i := 0; i < maxEventStreams; i++ {
		_, frames, cancel := openBriefStream(t, e, srv.URL, id)
		cancels = append(cancels, cancel)
		nextFrame(t, frames)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/app/briefs/"+id+"/events", nil)
	req.AddCookie(e.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("stream %d = %d, want 503", maxEventStreams+1, resp.StatusCode)
	}
	cancels[0]() // a slot frees when its client goes away
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, frames, cancel := openBriefStream(t, e, srv.URL, id)
		select {
		case f, ok := <-frames:
			cancels = append(cancels, cancel)
			if ok && f.event == "state" {
				goto freed
			}
		case <-time.After(300 * time.Millisecond):
			cancel()
		}
		if time.Now().After(deadline) {
			t.Fatal("no slot freed after a cancel")
		}
	}
freed:
	for _, c := range cancels {
		c()
	}

	if w := e.get("/app/briefs/gm-2026-10-24-777/events"); w.Code != http.StatusNotFound {
		t.Errorf("unknown run = %d", w.Code)
	}
	if w := e.get("/app/briefs/x/events"); w.Code != http.StatusNotFound {
		t.Errorf("bad id = %d", w.Code)
	}
	if w := doBody(e.h, "GET", "/app/briefs/"+id+"/events", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", w.Code)
	}
}
