package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testStream(r io.Reader) *EventStream {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxSSELine)
	return &EventStream{body: io.NopCloser(r), scanner: sc}
}

func TestNextDeliversA100KBDataLineAsOneEvent(t *testing.T) {
	big := strings.Repeat("x", 100<<10)
	s := testStream(strings.NewReader("event: run-report\ndata: " + big + "\n\n"))
	ev, err := s.Next()
	if err != nil || ev.Type != "run-report" || ev.Data != big {
		t.Fatalf("err = %v, type = %q, data len = %d", err, ev.Type, len(ev.Data))
	}
}

func TestNextLineOver8MiBReturnsSentinel(t *testing.T) {
	s := testStream(strings.NewReader("data: " + strings.Repeat("x", maxSSELine) + "\n\n"))
	if _, err := s.Next(); !errors.Is(err, ErrSSELineTooLong) {
		t.Fatalf("err = %v, want ErrSSELineTooLong", err)
	}
}

func TestOpenSSEUsesTheLargeBuffer(t *testing.T) {
	big := strings.Repeat("x", 100<<10)
	srv := sseServer(t, "", nil, "event: run-report\ndata: "+big+"\n\n")
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	s, err := c.openSSE(context.Background(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ev, err := s.Next(); err != nil || ev.Data != big {
		t.Fatalf("err = %v, data len = %d", err, len(ev.Data))
	}
}

func TestNextCRLFLineEndings(t *testing.T) {
	s := testStream(strings.NewReader("id: 3\r\nevent: token\r\ndata: hi\r\n\r\nevent: done\r\ndata: bye\r\n\r\n"))
	ev, err := s.Next()
	if err != nil || ev.Type != "token" || ev.Data != "hi" || ev.ID != 3 {
		t.Fatalf("first = %+v, %v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != "done" || ev.Data != "bye" {
		t.Fatalf("second = %+v, %v", ev, err)
	}
	if _, err := s.Next(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestNextIDLineWithNoDataIsNotAFrame(t *testing.T) {
	s := testStream(strings.NewReader("id: 5\n\nevent: token\ndata: x\n\n"))
	ev, err := s.Next()
	if err != nil || ev.Type != "token" || ev.Data != "x" {
		t.Fatalf("ev = %+v, %v", ev, err)
	}
	if _, err := s.Next(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestNextFrameSplitAcrossReads(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		for _, p := range []string{"ev", "ent: tok", "en\nda", "ta: hel", "lo\n", "\n"} {
			pw.Write([]byte(p))
		}
		pw.Close()
	}()
	s := testStream(pr)
	ev, err := s.Next()
	if err != nil || ev.Type != "token" || ev.Data != "hello" {
		t.Fatalf("ev = %+v, %v", ev, err)
	}
	if _, err := s.Next(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestNextReturnsAnErrorAfterContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	ctx, cancel := context.WithCancel(context.Background())
	s, err := c.openSSE(ctx, http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.Next(); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil || err == io.EOF {
			t.Fatalf("err = %v, want a non-EOF error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Next did not return after the context was cancelled")
	}
}

func bigBodyClient(t *testing.T, n int) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		io.WriteString(w, strings.Repeat("a", n))
	}))
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, Token: "t"})
}

func TestResponseOverLimitIsAnErrorNotATruncation(t *testing.T) {
	c := bigBodyClient(t, maxResponseBytes+1)
	if _, err := c.ListSessions(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("JSON endpoint: err = %v, want ErrResponseTooLarge", err)
	}
	if _, err := c.PipelineArtifact(context.Background(), "", "x", 1, "reply.txt"); !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("artifact: err = %v, want ErrResponseTooLarge", err)
	}
}

func TestResponseAtLimitIsAccepted(t *testing.T) {
	c := bigBodyClient(t, maxResponseBytes)
	s, err := c.PipelineArtifact(context.Background(), "", "x", 1, "reply.txt")
	if err != nil || len(s) != maxResponseBytes {
		t.Fatalf("len = %d, err = %v", len(s), err)
	}
}
