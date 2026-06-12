package llm

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer returns an httptest server that records how many requests it
// served and always replies with the given completion content.
func countingServer(t *testing.T, content string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + content + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func cachedClient(url, model, dir string, ttl time.Duration) *Client {
	c := New(url, "", model, 5*time.Second, false)
	c.Cache = &Cache{Dir: dir, TTL: ttl}
	return c
}

// TestCacheMissThenHit: first call hits the network and stores; the second
// identical call is served from cache and does NOT increment the server count.
func TestCacheMissThenHit(t *testing.T) {
	srv, hits := countingServer(t, "hello")
	c := cachedClient(srv.URL, "m", t.TempDir(), time.Hour)
	msgs := []Message{{Role: "user", Content: "hi"}}

	msg1, _, err := c.Complete(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("after first call hits=%d, want 1", got)
	}

	msg2, usage2, err := c.Complete(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("cache hit still reached network: hits=%d, want 1", got)
	}
	if msg1.Content != msg2.Content || msg2.Content != "hello" {
		t.Errorf("cached response differs: %q vs %q", msg1.Content, msg2.Content)
	}
	// A cache hit had no real token spend: usage is zero.
	if usage2 != (Usage{}) {
		t.Errorf("cache hit reported nonzero usage: %+v", usage2)
	}
}

// TestCacheKeyDistinguishesInputs: changing model, messages, tools, or
// temperature must miss (no false hit).
func TestCacheKeyDistinguishesInputs(t *testing.T) {
	srv, hits := countingServer(t, "x")
	dir := t.TempDir()
	c := cachedClient(srv.URL, "m", dir, time.Hour)

	base := []Message{{Role: "user", Content: "hi"}}
	tools := []Tool{{Type: "function", Function: Function{Name: "read_file"}}}

	// Each distinct request must reach the network once.
	c.Complete(context.Background(), base, nil)                                       // 1: model m, msgs base, no tools
	c.Complete(context.Background(), []Message{{Role: "user", Content: "bye"}}, nil)  // 2: different message
	c.Complete(context.Background(), base, tools)                                     // 3: different tools
	c2 := cachedClient(srv.URL, "other-model", dir, time.Hour)
	c2.Complete(context.Background(), base, nil) // 4: different model

	if got := atomic.LoadInt32(hits); got != 4 {
		t.Fatalf("distinct requests collided: hits=%d, want 4", got)
	}
}

// TestCacheTemperatureDistinguishes: a different temperature must produce a
// different key. Complete hard-codes Temperature:0, so build the request and
// key directly to prove the key function discriminates on it.
func TestCacheTemperatureDistinguishes(t *testing.T) {
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour}
	r0 := ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}, Temperature: 0}
	r1 := r0
	r1.Temperature = 0.7
	k0, _ := c.key(r0)
	k1, _ := c.key(r1)
	if k0 == k1 {
		t.Fatalf("temperature did not change the cache key: %s", k0)
	}
}

// TestCacheTTLExpiry: an entry older than the TTL is a miss and a real request
// proceeds (network hit increments).
func TestCacheTTLExpiry(t *testing.T) {
	srv, hits := countingServer(t, "y")
	c := cachedClient(srv.URL, "m", t.TempDir(), 20*time.Millisecond)
	msgs := []Message{{Role: "user", Content: "hi"}}

	c.Complete(context.Background(), msgs, nil)
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("first hits=%d, want 1", got)
	}
	time.Sleep(40 * time.Millisecond) // let the entry expire

	c.Complete(context.Background(), msgs, nil)
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("expired entry was served from cache: hits=%d, want 2", got)
	}
}

// TestCacheCorruptFileIsMiss: a garbage cache file is treated as a miss; a real
// request proceeds rather than crashing.
func TestCacheCorruptFileIsMiss(t *testing.T) {
	srv, hits := countingServer(t, "z")
	dir := t.TempDir()
	c := cachedClient(srv.URL, "m", dir, time.Hour)
	msgs := []Message{{Role: "user", Content: "hi"}}

	// Plant a corrupt file at the exact key path.
	req := ChatRequest{Model: "m", Messages: msgs}
	key, err := c.Cache.key(req)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, key)
	if err := os.WriteFile(p, []byte("not json at all {{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	msg, _, err := c.Complete(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Complete with corrupt cache: %v", err)
	}
	if msg.Content != "z" {
		t.Errorf("did not fall through to network: content=%q", msg.Content)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("corrupt file not treated as miss: hits=%d, want 1", got)
	}
}

// TestCacheDisabledNoDiskIO: with no cache configured, every call hits the
// network and nothing is written to the candidate dir.
func TestCacheDisabledNoDiskIO(t *testing.T) {
	srv, hits := countingServer(t, "n")
	dir := t.TempDir()
	c := New(srv.URL, "", "m", 5*time.Second, false) // Cache nil
	msgs := []Message{{Role: "user", Content: "hi"}}

	c.Complete(context.Background(), msgs, nil)
	c.Complete(context.Background(), msgs, nil)
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("disabled cache still cached: hits=%d, want 2", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("disabled cache wrote %d files to dir", len(entries))
	}
}

// TestCacheAtomicWriteNoPartials: after a store, the cache dir contains exactly
// the final hex-named entry and no leftover temp files; the file is 0600.
func TestCacheAtomicWriteNoPartials(t *testing.T) {
	srv, _ := countingServer(t, "a")
	dir := t.TempDir()
	c := cachedClient(srv.URL, "m", dir, time.Hour)

	c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 entry, got %d: %v", len(entries), entries)
	}
	name := entries[0].Name()
	if strings.HasPrefix(name, ".tmp-") {
		t.Errorf("leftover temp file: %s", name)
	}
	if len(name) != 64 { // sha256 hex
		t.Errorf("entry name is not a sha256 hex: %q", name)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("cache file perm = %o, want 600", perm)
	}
}

// TestCacheHashMismatchRejected: a well-formed entry stored under the WRONG key
// (its content doesn't correspond to that request) is never served for a
// different request — cross-request poisoning is prevented by content-addressing.
func TestCacheHashMismatchRejected(t *testing.T) {
	srv, hits := countingServer(t, "real")
	dir := t.TempDir()
	c := cachedClient(srv.URL, "m", dir, time.Hour)

	// Store a valid-looking entry under the key for request A...
	reqA := ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "A"}}}
	keyA, _ := c.Cache.key(reqA)
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, keyA), []byte(`{"created_at":"`+time.Now().Format(time.RFC3339)+`","message":{"role":"assistant","content":"poison"},"usage":{}}`), 0o600)

	// ...then request B. Its key differs, so the planted entry is not served.
	msg, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "B"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content == "poison" {
		t.Fatal("served an entry stored under a different request's key")
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("request B did not reach network: hits=%d", got)
	}
}
