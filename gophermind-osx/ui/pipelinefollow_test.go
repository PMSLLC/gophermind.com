package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
)

func fastWait(int) time.Duration { return time.Millisecond }

func TestPipelineFollower_ReconnectsAtTheLastIDAfterADrop(t *testing.T) {
	var mu sync.Mutex
	var sinces []string
	var conns int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		n := conns
		sinces = append(sinces, r.URL.Query().Get("since")+"/"+r.URL.Query().Get("run"))
		mu.Unlock()
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		switch n {
		case 1: // the first connection delivers two events, then drops
			io.WriteString(w, "id: 11\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"in_progress\",\"wave\":0}\n\n")
			io.WriteString(w, "id: 12\nevent: wave-changed\ndata: {\"wave\":0,\"state\":\"started\"}\n\n")
		default: // the second stays up until the test ends
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	state := NewPipelineState()
	state.SetState(sampleState("gm-2026-10-08-001", 10))
	f := &PipelineFollower{State: state, Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := len(sinces)
		mu.Unlock()
		if got >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the follower did not reconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sinces[0] != "10/gm-2026-10-08-001" || sinces[1] != "12/gm-2026-10-08-001" {
		t.Errorf("requests = %v, want since=10 then since=12 for the same run", sinces)
	}
}

func TestPipelineFollower_RetriesAfterAnOpenError(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		if n < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	f := &PipelineFollower{State: NewPipelineState(), Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := hits
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no retry")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
}

func TestPipelineFollower_ASchemaMismatchStopsAndSetsTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "2")
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer srv.Close()
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	state := NewPipelineState()
	f := &PipelineFollower{State: state, Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	err := f.Run(context.Background())
	var mm *client.SchemaMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("Run = %v, want a *SchemaMismatchError", err)
	}
	if state.Message() != client.SchemaMismatchMessage {
		t.Errorf("message = %q", state.Message())
	}
}

func openerFor(c *client.Client) OpenEventsFunc {
	return func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}
}

func TestPipelineFollower_OnOpenRunsOnEveryConnectBeforeTheStreamIsRead(t *testing.T) {
	var mu sync.Mutex
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		n := conns
		mu.Unlock()
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			io.WriteString(w, "id: 5\nevent: run-status\ndata: {\"status\":\"verified\",\"stop_reason\":\"\"}\n\n")
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	state := NewPipelineState()
	state.SetState(sampleState("gm-2026-10-08-001", 1))
	var opens int32
	var sawStatusAtOpen sync.Map
	f := &PipelineFollower{State: state, Wait: fastWait, Open: openerFor(c)}
	f.OnOpen = func(ctx context.Context) {
		k := atomic.AddInt32(&opens, 1)
		sawStatusAtOpen.Store(k, state.Run().Status)
		// the app's refetch: a fresh snapshot of a run that ended while away
		state.SetState(sampleState("gm-2026-10-08-001", 1))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&opens) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("OnOpen did not run on the reconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if v, _ := sawStatusAtOpen.Load(int32(1)); v != "building" {
		t.Errorf("the first OnOpen ran after the stream was read: status %v", v)
	}
	if state.LastSeq() != 5 {
		t.Errorf("LastSeq = %d, want 5: an older refetch must not rewind the stream", state.LastSeq())
	}
}

func TestPipelineFollower_DoesNotRetryAnOverlongLineOrATooLargeResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want error
		msg  string
	}{
		"line": {"id: 3\nevent: run-report\ndata: " + strings.Repeat("x", 9<<20) + "\n\n", client.ErrSSELineTooLong, StreamLineTooLongMessage},
	} {
		t.Run(name, func(t *testing.T) {
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
			state := NewPipelineState()
			f := &PipelineFollower{State: state, Wait: fastWait, Open: openerFor(c)}
			err := f.Run(context.Background())
			if !errors.Is(err, tc.want) || state.Message() != tc.msg || atomic.LoadInt32(&hits) != 1 {
				t.Errorf("err %v message %q hits %d", err, state.Message(), hits)
			}
		})
	}
	// An open that fails with ErrResponseTooLarge is not retried either.
	var opens int32
	state := NewPipelineState()
	f := &PipelineFollower{State: state, Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		atomic.AddInt32(&opens, 1)
		return nil, fmt.Errorf("wrapped: %w", client.ErrResponseTooLarge)
	}}
	if err := f.Run(context.Background()); !errors.Is(err, client.ErrResponseTooLarge) || opens != 1 || state.Message() != ResponseTooLargeMessage {
		t.Errorf("err %v opens %d message %q", err, opens, state.Message())
	}
}

func TestPipelineFollower_StopsOnCancelWithoutLeakingGoroutines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	before := runtime.NumGoroutine()
	var opened int32
	f := &PipelineFollower{State: NewPipelineState(), Wait: fastWait, OnOpen: func(context.Context) { atomic.AddInt32(&opened, 1) }, Open: openerFor(c)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	for atomic.LoadInt32(&opened) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	srv.Close()
	http.DefaultClient.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+1 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines %d, was %d before: the follower leaked", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
