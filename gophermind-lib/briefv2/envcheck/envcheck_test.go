package envcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/settings"
)

func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			os.Unsetenv(strings.SplitN(kv, "=", 2)[0])
		}
	}
	os.Exit(m.Run())
}

func testConfig(base string, fallbacks ...string) *settings.Config {
	c := settings.Default()
	c.Providers = []settings.ProviderConfig{{
		Name: "fake", BaseURL: base, BaseURLFallbacks: fallbacks, Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}},
	}}
	c.Models = map[string][]string{}
	for _, tier := range settings.Tiers {
		c.Models[tier] = []string{"fake/fixture"}
	}
	return c
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestResolveBaseURLsFallbackUsed(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "{}") }))
	t.Cleanup(good.Close)
	dead := "http://" + closedAddr(t) + "/v1"
	cfg := testConfig(dead, good.URL+"/v1")

	got, res := ResolveBaseURLs(context.Background(), cfg, &http.Client{}, time.Second)
	if len(res) != 1 || !res[0].Answered || !res[0].Fallback || res[0].Provider != "fake" || res[0].Host != "127.0.0.1" {
		t.Fatalf("results = %+v", res)
	}
	if got.Providers[0].BaseURL != good.URL+"/v1" {
		t.Errorf("the copy carries %q, want the fallback", got.Providers[0].BaseURL)
	}
	if cfg.Providers[0].BaseURL != dead {
		t.Errorf("the original config was changed: %q", cfg.Providers[0].BaseURL)
	}
}

type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if h := r.URL.Hostname(); h != "127.0.0.1" {
		f.t.Errorf("a request went to %s", h)
	}
	return nil, errors.New("no network")
}

func TestResolveBaseURLsSkipsPublicFallbackForPrivate(t *testing.T) {
	cfg := testConfig("http://"+closedAddr(t)+"/v1", "http://203.0.113.9:11434/v1")
	got, res := ResolveBaseURLs(context.Background(), cfg, &http.Client{Transport: failingTransport{t}}, 100*time.Millisecond)
	if got.Providers[0].BaseURL != cfg.Providers[0].BaseURL || res[0].Answered || res[0].Fallback {
		t.Errorf("a public fallback was used or probed for a private provider: %+v", res)
	}
}

func TestProbeTimeoutIsAParameter(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			if _, err := l.Accept(); err != nil {
				return // accepted connections are never answered
			}
		}
	}()
	start := time.Now()
	if ProbeBaseURL(context.Background(), &http.Client{}, "http://"+l.Addr().String()+"/v1", 100*time.Millisecond) {
		t.Error("a listener that never answers was reported as answering")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, the 100 ms timeout argument was not honored", d)
	}
}

func TestLoopbackAddr(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"postgres://127.0.0.1:5433/db", "127.0.0.1:5433", true},
		{"postgres://localhost/db", "localhost:5432", true},
		{"redis://[::1]:6380", "[::1]:6380", true},
		{"postgres://192.168.1.35:5432/db", "", false},
		{"not a url", "", false},
	} {
		got, ok := LoopbackAddr(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("LoopbackAddr(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLookInPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := LookInPath("tool", strings.Join([]string{"", "relative/dir", dir}, string(os.PathListSeparator))); !ok || got != bin {
		t.Errorf("got %q, %v; want %q", got, ok, bin)
	}
	if _, ok := LookInPath("tool", ""); ok {
		t.Error("an empty PATH found a tool")
	}
	t.Chdir(dir)
	if _, ok := LookInPath("tool", ".:"); ok {
		t.Error("a relative or empty PATH entry was searched")
	}
}

func TestDirtyOutsideTestsEmptyRunDir(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".gophermind", "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gophermind", "run", "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A decoy list in the working directory that would allow stray.txt: with an
	// empty runDir it must not be read.
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "_state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "_state", "test_files.json"), []byte(`["stray.txt"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	n, err := DirtyOutsideTests(repo, "")
	if err != nil || n != 1 {
		t.Errorf("DirtyOutsideTests = %d, %v; want 1 (stray.txt only, .gophermind/ not counted)", n, err)
	}
}
