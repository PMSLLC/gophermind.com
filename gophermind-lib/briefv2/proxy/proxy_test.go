package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/brief"
)

func newProxyCfg(t *testing.T, c Config) *Proxy {
	t.Helper()
	if c.Listen == "" {
		c.Listen = "127.0.0.1:0"
	}
	if c.LogPath == "" {
		c.LogPath = filepath.Join(t.TempDir(), "proxy.log")
	}
	p, err := New(c)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func newProxy(t *testing.T, rules ...Rule) *Proxy {
	t.Helper()
	return newProxyCfg(t, Config{Rules: rules})
}

func via(t *testing.T, p *Proxy, node string) *http.Client {
	t.Helper()
	u, err := url.Parse(p.URL(node))
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(u),
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func readLog(t *testing.T, p *Proxy, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		b, _ := os.ReadFile(p.cfg.LogPath)
		var out []map[string]any
		for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if ln == "" {
				continue
			}
			m := map[string]any{}
			if err := json.Unmarshal([]byte(ln), &m); err != nil {
				t.Fatalf("log line is not JSON: %v", err)
			}
			out = append(out, m)
		}
		if len(out) >= want || time.Now().After(deadline) {
			if len(out) < want {
				t.Fatalf("want %d log lines, have %d", want, len(out))
			}
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func upstream(t *testing.T, h http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s, s.Listener.Addr().String()
}

func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }
}

func failuresEventually(t *testing.T, get func() []Failure, want int) []Failure {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		f := get()
		if len(f) >= want || time.Now().After(deadline) {
			return f
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hostProxy builds a proxy whose name resolution and dialing are faked: every
// name resolves to resolved and every dial lands on target.
func hostProxy(t *testing.T, target string, resolved []net.IP, rules ...Rule) (*Proxy, *[]string, *int) {
	t.Helper()
	var mu sync.Mutex
	var dialed []string
	calls := 0
	p := newProxyCfg(t, Config{
		Rules: rules,
		resolve: func(ctx context.Context, host string) ([]net.IP, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			return resolved, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, "tcp", target)
		},
	})
	return p, &dialed, &calls
}

var publicIP = []net.IP{net.ParseIP("93.184.216.34")}

func TestProxyListensOnLoopbackPort(t *testing.T) {
	a := newProxy(t)
	b := newProxy(t)
	for _, p := range []*Proxy{a, b} {
		h, port, err := net.SplitHostPort(p.Addr())
		if err != nil || h != "127.0.0.1" || port == "0" || port == "" {
			t.Fatalf("addr %q", p.Addr())
		}
	}
	if a.Addr() == b.Addr() {
		t.Fatal("two proxies share an address")
	}
	for _, l := range []string{"0.0.0.0:0", "192.168.1.35:0", ":0"} {
		_, err := New(Config{Listen: l, LogPath: filepath.Join(t.TempDir(), "l")})
		if err == nil || !strings.Contains(err.Error(), "must be loopback") {
			t.Fatalf("Listen %q: %v", l, err)
		}
	}
	p := newProxyCfg(t, Config{Listen: "localhost:0"})
	if !strings.HasPrefix(p.Addr(), "127.0.0.1:") {
		t.Fatalf("localhost bound %q", p.Addr())
	}
	if l, err := net.Listen("tcp", "[::1]:0"); err == nil {
		_ = l.Close()
		p6 := newProxyCfg(t, Config{Listen: "[::1]:0"})
		if !strings.HasPrefix(p6.Addr(), "[::1]:") {
			t.Fatalf("::1 bound %q", p6.Addr())
		}
	}
}

func TestBuildRules(t *testing.T) {
	net1 := []brief.Network{
		{Host: "Registry.Example.test", Critical: true},
		{Host: "*.cdn.example.test"},
		{Host: "api.kilo.ai"},
	}
	provs := []string{"http://192.168.1.35:11434/v1", "https://api.kilo.ai/api/gateway"}
	got, err := BuildRules(net1, provs, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{"*.cdn.example.test", false},
		{"192.168.1.35", false},
		{"api.kilo.ai", false},
		{"proxy.golang.org", false},
		{"registry.example.test", true},
		{"sum.golang.org", false},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got, _ = BuildRules(net1, provs, true)
	for _, r := range got {
		if strings.HasSuffix(r.Host, "golang.org") {
			t.Fatalf("go host kept: %v", got)
		}
	}
	got, _ = BuildRules([]brief.Network{{Host: "api.kilo.ai", Critical: true}}, provs, true)
	for _, r := range got {
		if r.Host == "api.kilo.ai" && !r.Critical {
			t.Fatal("critical not OR-ed")
		}
	}
	_, err = BuildRules(nil, []string{"http://ok.test", "http://bad host/%zz-SECRETCANARY"}, false)
	if err == nil || !strings.Contains(err.Error(), "1") || strings.Contains(err.Error(), "SECRETCANARY") || strings.Contains(err.Error(), "bad host") {
		t.Fatalf("bad url error: %v", err)
	}
	for _, bad := range []string{"*", "*.com", "a b", "", "a/b", "*.*.x.test", "x.*.test"} {
		if _, err := BuildRules([]brief.Network{{Host: bad}}, nil, true); err == nil {
			t.Fatalf("host %q accepted", bad)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Example.COM.", "example.com", true},
		{"example.com..", "", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"::ffff:127.0.0.1", "127.0.0.1", true},
		{"::1", "::1", true},
		{"fe80::1%eth0", "", false},
		{"", "", false},
		{"a b", "", false},
		{"a..b", "", false},
		{"0x7f.1", "", false},
		{"2130706433", "", false},
		{"0177.0.0.1", "", false},
		{"127.1", "", false},
		{"user@host.test", "", false},
		{"host.test/", "", false},
		{"ho\x00st.test", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeHost(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("normalizeHost(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestForbiddenIP(t *testing.T) {
	bad := []string{"127.0.0.1", "127.5.5.5", "::1", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "169.254.1.1", "100.64.0.1", "0.0.0.0", "::", "::ffff:10.0.0.1", "::ffff:127.0.0.1",
		"fe80::1", "fc00::1", "fd00::1", "224.0.0.1", "255.255.255.255", "198.18.0.1", "240.0.0.1", "64:ff9b::7f00:1",
		"192.0.2.1", "198.51.100.7", "203.0.113.9", "2002:7f00:1::1", "::7f00:1", "::8.8.8.8", "ff02::1"}
	for _, s := range bad {
		if !forbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be forbidden", s)
		}
	}
	good := []string{"93.184.216.34", "8.8.8.8", "172.32.0.1", "2606:4700::1111"}
	for _, s := range good {
		if forbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestMatchRules(t *testing.T) {
	rules := []Rule{{"example.test", false}, {"*.suffix.test", false}, {"127.0.0.1", false}, {"crit.test", true}}
	cases := []struct {
		host  string
		allow bool
		crit  bool
	}{
		{"example.test", true, false},
		{"a.example.test", false, false},
		{"a.suffix.test", true, false},
		{"a.b.suffix.test", true, false},
		{"suffix.test", false, false},
		{"xsuffix.test", false, false},
		{"127.0.0.1", true, false},
		{"127.0.0.2", false, false},
		{"crit.test", true, true},
	}
	for _, c := range cases {
		a, cr := matchRules(rules, c.host)
		if a != c.allow || cr != c.crit {
			t.Errorf("%s: got %v,%v want %v,%v", c.host, a, cr, c.allow, c.crit)
		}
	}
	// a wildcard never matches an IP literal
	if a, _ := matchRules([]Rule{{"*.0.0.1", false}}, "127.0.0.1"); a {
		t.Error("wildcard matched an IP")
	}
}

func TestProxyAllowDeny(t *testing.T) {
	srv, addr := upstream(t, okHandler("upstream-body"))
	rules := []Rule{{"exact.test", false}, {"*.example.test", false}}
	p, dialed, _ := hostProxy(t, addr, publicIP, rules...)
	c := via(t, p, "fn-a")

	rows := []struct {
		host string
		want int
	}{
		{"exact.test", 200},
		{"EXACT.test", 200},
		{"exact.test.", 200},
		{"a.example.test", 200},
		{"a.b.example.test", 200},
		{"example.test", 403},
		{"xexample.test", 403},
		{"unlisted.test", 403},
		{"exact.test.evil.test", 403},
	}
	for _, r := range rows {
		resp, err := c.Get("http://" + r.host + "/x")
		if err != nil {
			t.Fatalf("%s: %v", r.host, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != r.want {
			t.Errorf("%s: status %d want %d", r.host, resp.StatusCode, r.want)
		}
		if r.want == 200 && string(b) != "upstream-body" {
			t.Errorf("%s: body %q", r.host, b)
		}
		if r.want == 403 && len(b) != 0 {
			t.Errorf("%s: denied body %q", r.host, b)
		}
	}
	for _, d := range *dialed {
		if !strings.HasPrefix(d, "93.184.216.34:") {
			t.Errorf("dialed %q, not the resolved IP", d)
		}
	}
	_ = srv

	// CONNECT end to end to a TLS server on loopback
	tsrv := httptest.NewTLSServer(okHandler("tls-body"))
	defer tsrv.Close()
	p2 := newProxy(t, Rule{Host: "127.0.0.1"})
	pool := x509.NewCertPool()
	pool.AddCert(tsrv.Certificate())
	pu, _ := url.Parse(p2.URL("fn-a"))
	tc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		Proxy: http.ProxyURL(pu), DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
	resp, err := tc.Get(tsrv.URL)
	if err != nil {
		t.Fatalf("connect get: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "tls-body" {
		t.Fatalf("tls body %q", b)
	}
	st, conn := rawConnect(t, p2, "denied.test:443")
	conn.Close()
	if st != 403 {
		t.Fatalf("denied CONNECT status %d", st)
	}

	// redirect to an unlisted host is returned, not followed
	rsrv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://unlisted.test/next", http.StatusFound)
	})
	resp, err = via(t, p2, "fn-a").Get(rsrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "http://unlisted.test/next" {
		t.Fatalf("redirect: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func rawConnect(t *testing.T, p *Proxy, target string) (int, net.Conn) {
	t.Helper()
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, conn
}

func rawRequest(t *testing.T, p *Proxy, req string) int {
	t.Helper()
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, req)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestProxyDeniesUnlistedLoopback(t *testing.T) {
	srv, _ := upstream(t, okHandler("secret-local-service"))
	p := newProxy(t, Rule{Host: "example.test"})
	resp, err := via(t, p, "fn-a").Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || len(b) != 0 {
		t.Fatalf("status %d body %q", resp.StatusCode, b)
	}
	p2 := newProxy(t, Rule{Host: "127.0.0.1"})
	resp, err = via(t, p2, "fn-a").Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("listed loopback status %d", resp.StatusCode)
	}
	// CONNECT to unlisted loopback is denied as well
	st, conn := rawConnect(t, p, srv.Listener.Addr().String())
	conn.Close()
	if st != 403 {
		t.Fatalf("CONNECT loopback %d", st)
	}
}

func TestSSRFTargetsDenied(t *testing.T) {
	p := newProxy(t, Rule{Host: "example.test"})
	for _, target := range []string{
		"169.254.169.254", "10.0.0.5", "172.16.0.1", "192.168.1.1", "100.64.0.1",
		"[::1]", "[::ffff:127.0.0.1]", "[fe80::1]", "0.0.0.0", "localhost", "localhost.",
		"2130706433", "0x7f.0.0.1", "0177.0.0.1", "127.1",
	} {
		if st := rawRequest(t, p, "GET http://"+target+"/latest/meta-data/ HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); st != 403 {
			t.Errorf("GET %s: %d", target, st)
		}
		conn := func() int { s, c := rawConnect(t, p, strings.TrimSuffix(target, ".")+":80"); c.Close(); return s }()
		if conn != 403 && conn != 400 {
			t.Errorf("CONNECT %s: %d", target, conn)
		}
	}
	// userinfo trick: the host is what follows the @
	if st := rawRequest(t, p, "GET http://example.test@evil.test/ HTTP/1.1\r\nHost: evil.test\r\n\r\n"); st != 403 {
		t.Errorf("userinfo trick: %d", st)
	}
	if st := rawRequest(t, p, "GET http://example.test:80@127.0.0.1:9/ HTTP/1.1\r\nHost: x\r\n\r\n"); st != 403 {
		t.Errorf("userinfo with loopback: %d", st)
	}
}

func TestHostnameResolvingToPrivateDenied(t *testing.T) {
	srv, addr := upstream(t, okHandler("must-not-reach"))
	_ = srv
	for _, bad := range []string{"127.0.0.1", "169.254.169.254", "10.1.2.3", "::ffff:192.168.1.1", "::1"} {
		p, dialed, _ := hostProxy(t, addr, []net.IP{net.ParseIP(bad)}, Rule{Host: "rebind.test"})
		resp, err := via(t, p, "fn-a").Get("http://rebind.test/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 || len(*dialed) != 0 {
			t.Errorf("%s: status %d dialed %v", bad, resp.StatusCode, *dialed)
		}
	}
	// mixed answers: one bad address denies the lot
	p, dialed, _ := hostProxy(t, addr, []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.1")}, Rule{Host: "mixed.test"})
	resp, _ := via(t, p, "fn-a").Get("http://mixed.test/")
	resp.Body.Close()
	if resp.StatusCode != 403 || len(*dialed) != 0 {
		t.Errorf("mixed: %d %v", resp.StatusCode, *dialed)
	}
}

func TestResolveOnceDialResolvedIP(t *testing.T) {
	_, addr := upstream(t, okHandler("ok"))
	p, dialed, calls := hostProxy(t, addr, publicIP, Rule{Host: "once.test"})
	resp, err := via(t, p, "fn-a").Get("http://once.test:8123/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if *calls != 1 {
		t.Fatalf("resolver called %d times", *calls)
	}
	if len(*dialed) != 1 || (*dialed)[0] != "93.184.216.34:8123" {
		t.Fatalf("dialed %v", *dialed)
	}
}

func TestProxyRejectsDirectAndOddRequests(t *testing.T) {
	p := newProxy(t, Rule{Host: "127.0.0.1"})
	if st := rawRequest(t, p, "GET /direct HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); st != 400 {
		t.Errorf("direct hit %d", st)
	}
	if st := rawRequest(t, p, "GET https://127.0.0.1/ HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); st != 400 {
		t.Errorf("https absolute URL %d", st)
	}
	if st := rawRequest(t, p, "CONNECT 127.0.0.1 HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); st != 400 {
		t.Errorf("CONNECT without port %d", st)
	}
}

func TestHopByHopStripped(t *testing.T) {
	got := make(chan http.Header, 1)
	srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-Keep", "yes")
	})
	p := newProxy(t, Rule{Host: "127.0.0.1"})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("X-Custom-Hop", "v")
	req.Header.Set("Connection", "X-Custom-Hop")
	req.Header.Set("Keep-Alive", "300")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Te", "trailers")
	req.Header.Set("Upgrade", "websocket")
	resp, err := via(t, p, "fn-a").Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	h := <-got
	for _, k := range []string{"X-Custom-Hop", "Keep-Alive", "Proxy-Connection", "Te", "Upgrade", "Proxy-Authorization"} {
		if h.Get(k) != "" {
			t.Errorf("upstream saw %s", k)
		}
	}
	if resp.Header.Get("Keep-Alive") != "" || resp.Header.Get("X-Keep") != "yes" {
		t.Errorf("response headers: %v", resp.Header)
	}
}

func TestRequestAndResponseCaps(t *testing.T) {
	srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/known":
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
		case "/chunked":
			fl := w.(http.Flusher)
			for i := 0; i < 8; i++ {
				_, _ = w.Write([]byte(strings.Repeat("x", 512)))
				fl.Flush()
			}
		default:
			_, _ = io.Copy(io.Discard, r.Body)
		}
	})
	p := newProxyCfg(t, Config{Rules: []Rule{{Host: "127.0.0.1"}}, maxReqBytes: 1024, maxRespBytes: 1024})
	c := via(t, p, "fn-a")
	resp, err := c.Post(srv.URL+"/post", "text/plain", strings.NewReader(strings.Repeat("y", 4096)))
	if err != nil {
		t.Fatalf("oversized request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("oversized request status %d", resp.StatusCode)
	}
	resp, err = c.Get(srv.URL + "/known")
	if err != nil {
		t.Fatalf("known-length response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Fatalf("oversized known-length response status %d", resp.StatusCode)
	}
	resp, err = c.Get(srv.URL + "/chunked")
	if err != nil {
		t.Fatalf("chunked response: %v", err)
	}
	b, rerr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if rerr == nil || len(b) > 1024 {
		t.Fatalf("chunked response not aborted at the cap: %d bytes, err %v", len(b), rerr)
	}
}

func TestHostHeaderForcedToURLHost(t *testing.T) {
	var mu sync.Mutex
	var resolved []string
	seen := make(chan string, 1)
	srv, addr := upstream(t, func(w http.ResponseWriter, r *http.Request) { seen <- r.Host })
	p := newProxyCfg(t, Config{
		Rules: []Rule{{Host: "a.test"}, {Host: "b.test"}},
		resolve: func(ctx context.Context, h string) ([]net.IP, error) {
			mu.Lock()
			resolved = append(resolved, h)
			mu.Unlock()
			return publicIP, nil
		},
		dial: func(ctx context.Context, network, a string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
	})
	_ = srv
	if st := rawRequest(t, p, "GET http://a.test/ HTTP/1.1\r\nHost: b.test\r\n\r\n"); st != 200 {
		t.Fatalf("status %d", st)
	}
	if h := <-seen; h != "a.test" {
		t.Fatalf("upstream saw Host %q", h)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resolved) != 1 || resolved[0] != "a.test" {
		t.Fatalf("resolved %v", resolved)
	}
}

func TestClientAbortIsNotAFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	p := newProxy(t, Rule{Host: "127.0.0.1", Critical: true})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	go func() { <-started; cancel() }()
	if resp, err := via(t, p, "fn-a").Do(req); err == nil {
		resp.Body.Close()
	}
	readLog(t, p, 1)
	time.Sleep(200 * time.Millisecond)
	if f := p.Failures("fn-a", time.Time{}); len(f) != 0 {
		t.Fatalf("client abort recorded %v", f)
	}
	if w := p.Warnings("fn-a", time.Time{}); len(w) != 0 {
		t.Fatalf("client abort recorded warning %v", w)
	}
	var s Streak
	for i := 0; i < 3; i++ {
		s.Observe("fn-a", len(p.Failures("fn-a", time.Time{})) > 0)
	}
	if s.Export()["fn-a"] != 0 {
		t.Fatal("streak moved")
	}
}

func openTunnel(t *testing.T, p *Proxy) (net.Conn, func()) {
	t.Helper()
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	tg := echo.Addr().String()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", tg, tg,
		base64.StdEncoding.EncodeToString([]byte("node-fn-a:x")))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("tunnel: %v", err)
	}
	return conn, func() { conn.Close(); echo.Close() }
}

func TestTunnelByteCap(t *testing.T) {
	p := newProxyCfg(t, Config{Rules: []Rule{{Host: "127.0.0.1"}}, maxTunnel: 1024})
	conn, done := openTunnel(t, p)
	defer done()
	go func() {
		buf := []byte(strings.Repeat("z", 512))
		for i := 0; i < 64; i++ {
			if _, err := conn.Write(buf); err != nil {
				return
			}
		}
	}()
	n, err := io.Copy(io.Discard, conn)
	_ = err
	if n > 4096 {
		t.Fatalf("tunnel carried %d bytes past a 1024 cap", n)
	}
	w := failuresEventually(t, func() []Failure { return p.Warnings("fn-a", time.Time{}) }, 1)
	if len(w) != 1 || w[0].Kind != "tunnel_cap" || w[0].Host != "127.0.0.1" {
		t.Fatalf("warnings %v", w)
	}
}

func TestTunnelIdleTimeout(t *testing.T) {
	p := newProxyCfg(t, Config{Rules: []Rule{{Host: "127.0.0.1"}}, idleTimeout: 200 * time.Millisecond})
	conn, done := openTunnel(t, p)
	defer done()
	start := time.Now()
	_, _ = io.Copy(io.Discard, conn)
	if time.Since(start) > 4*time.Second {
		t.Fatal("idle tunnel not closed")
	}
	w := failuresEventually(t, func() []Failure { return p.Warnings("fn-a", time.Time{}) }, 1)
	if len(w) != 1 || w[0].Kind != "tunnel_idle" {
		t.Fatalf("warnings %v", w)
	}
}

func TestConnectDialTimeoutBounded(t *testing.T) {
	p := newProxyCfg(t, Config{
		Rules: []Rule{{Host: "127.0.0.1", Critical: true}},
		Dial:  150 * time.Millisecond,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	start := time.Now()
	st, conn := rawConnect(t, p, "127.0.0.1:9")
	conn.Close()
	if st != 504 || time.Since(start) > 2*time.Second {
		t.Fatalf("status %d after %v", st, time.Since(start))
	}
	f := failuresEventually(t, func() []Failure { return p.Failures("-", time.Time{}) }, 1)
	if len(f) != 1 || f[0].Kind != "timeout" {
		t.Fatalf("failures %v", f)
	}
}

func TestMalformedHostRecordedAsWarning(t *testing.T) {
	p := newProxy(t, Rule{Host: "example.test"})
	if st := rawRequest(t, p, "GET http://2130706433/ HTTP/1.1\r\nHost: x\r\n\r\n"); st != 403 {
		t.Fatalf("status %d", st)
	}
	w := failuresEventually(t, func() []Failure { return p.Warnings("-", time.Time{}) }, 1)
	if len(w) != 1 || w[0].Kind != "denied" {
		t.Fatalf("warnings %v", w)
	}
}

func TestNewValidatesRules(t *testing.T) {
	for _, bad := range []string{"*.com", "*", "a b", ""} {
		_, err := New(Config{Listen: "127.0.0.1:0", LogPath: filepath.Join(t.TempDir(), "l"), Rules: []Rule{{Host: bad}}})
		if err == nil || !strings.Contains(err.Error(), "invalid host") {
			t.Errorf("rule %q: %v", bad, err)
		}
	}
	p := newProxy(t, Rule{Host: "Example.TEST."})
	if ok, _ := matchRules(p.cfg.Rules, "example.test"); !ok {
		t.Error("rule not normalized")
	}
}

func TestCloseReturnsShutdownError(t *testing.T) {
	p, err := New(Config{Listen: "127.0.0.1:0", LogPath: filepath.Join(t.TempDir(), "l")})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.logf.Close() // simulate a failing log shutdown
	if err := p.Close(); err == nil {
		t.Fatal("Close hid the log close error")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestProxyLogHasNoPathOrBody(t *testing.T) {
	srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "reply CANARY-resp")
	})
	p := newProxy(t, Rule{Host: "127.0.0.1"})
	c := via(t, p, "fn-a")
	do := func(target string) {
		req, _ := http.NewRequest("POST", target+"/secret-path?token=CANARY-q", strings.NewReader("CANARY-body"))
		req.Header.Set("X-Secret", "CANARY-hdr")
		req.Header.Set("Authorization", "Bearer CANARY-hdr")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	do(srv.URL)
	do("http://denied.test")
	lines := readLog(t, p, 2)
	want := []string{"time", "node", "method", "host", "port", "verdict", "status", "bytes", "duration_ms"}
	for _, m := range lines {
		if len(m) != len(want) {
			t.Errorf("keys: %v", m)
		}
		for _, k := range want {
			if _, ok := m[k]; !ok {
				t.Errorf("missing key %s", k)
			}
		}
	}
	raw, _ := os.ReadFile(p.cfg.LogPath)
	f := fmt.Sprint(p.Failures("fn-a", time.Time{}), p.Warnings("fn-a", time.Time{}))
	for _, s := range []string{string(raw), f} {
		for _, bad := range []string{"CANARY", "secret-path", "token="} {
			if strings.Contains(s, bad) {
				t.Errorf("leaked %q in %q", bad, s)
			}
		}
	}
	st, err := os.Stat(p.cfg.LogPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st, err)
	}
}

func TestProxyNodeAttribution(t *testing.T) {
	seen := make(chan http.Header, 8)
	srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) { seen <- r.Header.Clone() })
	p := newProxy(t, Rule{Host: "127.0.0.1"})
	for _, n := range []string{"fn-a", "fn-b"} {
		resp, err := via(t, p, n).Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		<-seen
	}
	pu, _ := url.Parse("http://" + p.Addr())
	bare := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}}
	send := func(hdr, val string) http.Header {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		if hdr != "" {
			req.Header.Set(hdr, val)
		}
		resp, err := bare.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return <-seen
	}
	h := send("X-GopherMind-Node", "fn-c")
	if h.Get("X-GopherMind-Node") != "" || h.Get("Proxy-Authorization") != "" {
		t.Errorf("attribution headers reached upstream: %v", h)
	}
	send("X-GopherMind-Node", "a/b")
	send("X-GopherMind-Node", strings.Repeat("z", 65))
	send("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("node-a/b:pw")))
	send("", "")
	lines := readLog(t, p, 7)
	want := []string{"fn-a", "fn-b", "fn-c", "-", "-", "-", "-"}
	for i, m := range lines {
		if m["node"] != want[i] {
			t.Errorf("line %d node %v want %s", i, m["node"], want[i])
		}
	}
	if !strings.HasPrefix(p.URL("a/b"), "http://node--@") {
		t.Errorf("URL not sanitized: %s", p.URL("a/b"))
	}
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	_ = l.Close()
	return a
}

type failCase struct {
	name string
	kind string
	do   func(t *testing.T, p *Proxy) // makes the failing request
}

func failCases() []failCase {
	return []failCase{
		{"5xx", "5xx", func(t *testing.T, p *Proxy) {
			srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
			resp, err := via(t, p, "fn-a").Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		}},
		{"unreachable", "unreachable", func(t *testing.T, p *Proxy) {
			resp, err := via(t, p, "fn-a").Get("http://" + closedAddr(t) + "/")
			if err == nil {
				resp.Body.Close()
			}
		}},
		{"timeout", "timeout", func(t *testing.T, p *Proxy) {
			srv, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(1500 * time.Millisecond) })
			resp, err := via(t, p, "fn-a").Get(srv.URL)
			if err == nil {
				resp.Body.Close()
			}
		}},
	}
}

func TestCriticalHostFailure(t *testing.T) {
	for _, fc := range failCases() {
		t.Run(fc.name, func(t *testing.T) {
			p := newProxyCfg(t, Config{Rules: []Rule{{Host: "127.0.0.1", Critical: true}}, Dial: 150 * time.Millisecond})
			before := time.Now().Add(-time.Second)
			fc.do(t, p)
			f := failuresEventually(t, func() []Failure { return p.Failures("fn-a", before) }, 1)
			if len(f) != 1 || f[0].Kind != fc.kind || f[0].Node != "fn-a" || f[0].Host != "127.0.0.1" || f[0].At.IsZero() {
				t.Fatalf("failures %v", f)
			}
			if len(p.Failures("fn-b", before)) != 0 {
				t.Error("failure attributed to the wrong node")
			}
			if len(p.Failures("fn-a", time.Now().Add(time.Minute))) != 0 {
				t.Error("since did not hide the failure")
			}
			if len(p.Warnings("fn-a", before)) != 0 {
				t.Error("critical failure also a warning")
			}
		})
	}
	t.Run("denied wildcard", func(t *testing.T) {
		p := newProxy(t, Rule{Host: "*.crit.test", Critical: true})
		before := time.Now().Add(-time.Second)
		resp, err := via(t, p, "fn-a").Get("http://crit.test/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		f := failuresEventually(t, func() []Failure { return p.Failures("fn-a", before) }, 1)
		if len(f) != 1 || f[0].Kind != "denied" || f[0].Host != "crit.test" {
			t.Fatalf("failures %v", f)
		}
	})
	t.Run("ok", func(t *testing.T) {
		p := newProxy(t, Rule{Host: "127.0.0.1", Critical: true})
		srv, _ := upstream(t, okHandler("fine"))
		resp, err := via(t, p, "fn-a").Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		readLog(t, p, 1)
		if len(p.Failures("fn-a", time.Time{})) != 0 || len(p.Warnings("fn-a", time.Time{})) != 0 {
			t.Error("a 200 recorded something")
		}
	})
}

func TestNonCriticalWarns(t *testing.T) {
	for _, fc := range failCases() {
		t.Run(fc.name, func(t *testing.T) {
			p := newProxyCfg(t, Config{Rules: []Rule{{Host: "127.0.0.1"}}, Dial: 150 * time.Millisecond})
			before := time.Now().Add(-time.Second)
			fc.do(t, p)
			w := failuresEventually(t, func() []Failure { return p.Warnings("fn-a", before) }, 1)
			if len(w) != 1 || w[0].Kind != fc.kind {
				t.Fatalf("warnings %v", w)
			}
			if len(p.Failures("fn-a", before)) != 0 {
				t.Error("non-critical host produced a Failure")
			}
		})
	}
}

func TestThreeCriticalFailuresTerminal(t *testing.T) {
	var s Streak
	seq := []struct {
		failed bool
		want   bool
	}{{true, false}, {true, false}, {false, false}, {true, false}, {true, false}, {true, true}, {true, true}, {false, false}, {true, false}}
	for i, st := range seq {
		if got := s.Observe("n", st.failed); got != st.want {
			t.Fatalf("step %d: got %v want %v", i, got, st.want)
		}
	}
	var s2 Streak
	s2.Observe("a", true)
	s2.Observe("a", true)
	if s2.Observe("b", true) {
		t.Fatal("nodes not independent")
	}
	if !s2.Observe("a", true) {
		t.Fatal("a should be terminal")
	}
	ex := s2.Export()
	ex["a"] = 99
	if s2.Export()["a"] != 3 {
		t.Fatal("Export is not a copy")
	}
	var s3 Streak
	s3.Import(map[string]int{"x": 2})
	if !s3.Observe("x", true) {
		t.Fatal("resumed count of 2 should go terminal")
	}
	s3.Import(map[string]int{})
	if s3.Observe("x", true) {
		t.Fatal("Import did not replace state")
	}
	var s4 Streak
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s4.Observe(fmt.Sprint("n", i%3), j%5 != 0)
				_ = s4.Export()
			}
		}(i)
	}
	wg.Wait()
}

func TestHelperFetch(t *testing.T) {
	target := os.Getenv("PROXY_TEST_FETCH")
	if target == "" {
		return
	}
	resp, err := http.Get(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch failed")
		os.Exit(1)
	}
	b, _ := io.ReadAll(resp.Body)
	fmt.Print(string(b))
	os.Exit(0)
}

func TestLoopbackNoProxy(t *testing.T) {
	srv, _ := upstream(t, okHandler("loopback-body"))
	p := newProxy(t, Rule{Host: "127.0.0.1"})
	for _, noProxy := range []string{"127.0.0.1,localhost,::1", ""} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperFetch$")
		var env []string
		for _, e := range os.Environ() {
			u := strings.ToUpper(e)
			if strings.HasPrefix(u, "NO_PROXY=") || strings.HasPrefix(u, "HTTP_PROXY=") || strings.HasPrefix(u, "HTTPS_PROXY=") {
				continue
			}
			env = append(env, e)
		}
		env = append(env, "PROXY_TEST_FETCH="+srv.URL, "HTTP_PROXY="+p.URL("fn-a"))
		if noProxy != "" {
			env = append(env, "NO_PROXY="+noProxy)
		}
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || !strings.Contains(string(out), "loopback-body") {
			t.Fatalf("NO_PROXY=%q: %v %q", noProxy, err, out)
		}
	}
	if b, _ := os.ReadFile(p.cfg.LogPath); len(b) != 0 {
		t.Fatalf("loopback traffic reached the proxy: %q", b)
	}
}

func TestProxyCloseIsClean(t *testing.T) {
	base := runtime.NumGoroutine()
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	p, err := New(Config{Listen: "127.0.0.1:0", LogPath: filepath.Join(t.TempDir(), "p.log"), Rules: []Rule{{Host: "127.0.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	addr := p.Addr()
	st, conn := rawConnect(t, p, echo.Addr().String())
	if st != 200 {
		t.Fatalf("tunnel status %d", st)
	}
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	_, _ = io.ReadFull(conn, buf)
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("Close took too long")
	}
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("port still accepting")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	conn.Close()
	echo.Close()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base+2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base+2 {
		t.Fatalf("goroutines %d, baseline %d", n, base)
	}
}
