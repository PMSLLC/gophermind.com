package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultDial        = 10 * time.Second
	defaultMaxReq      = int64(64 << 20)
	defaultMaxResp     = int64(512 << 20)
	maxRequestDuration = 10 * time.Minute
	defaultMaxTunnel   = int64(256 << 20)
	defaultIdle        = 2 * time.Minute
	maxTunnelDuration  = 30 * time.Minute
	closeWait          = 5 * time.Second
	maxRecorded        = 4096
	maxDialIPs         = 4
)

// Config configures a Proxy. The unexported fields are test seams and size
// overrides; zero values mean the defaults.
type Config struct {
	Listen  string // "127.0.0.1:0"; anything not loopback is refused
	LogPath string // proxy.log in the run folder, mode 0600, appended
	Rules   []Rule
	Dial    time.Duration // default 10s
	Now     func() time.Time

	maxReqBytes  int64
	maxRespBytes int64
	maxTunnel    int64
	idleTimeout  time.Duration
	resolve      func(ctx context.Context, host string) ([]net.IP, error)
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Failure is one failed request to a rule host. It holds the node, the
// normalized host and the kind only: never a URL, header or body.
type Failure struct {
	Node string
	Host string
	Kind string // "denied", "unreachable", "timeout", "5xx"
	At   time.Time
}

// Proxy is the loopback forward proxy.
type Proxy struct {
	cfg Config
	ln  net.Listener
	srv *http.Server
	tr  *http.Transport

	ctx    context.Context
	cancel context.CancelFunc

	logMu sync.Mutex
	logf  *os.File

	mu       sync.Mutex
	closed   bool
	tunnels  map[net.Conn]struct{}
	failures []Failure
	warnings []Failure

	wg            sync.WaitGroup
	serveDone     chan struct{}
	closeOnce     sync.Once
	closeErr      error
	closeReported atomic.Bool
}

var errDenied = errors.New("proxy: destination denied")

// New validates the config, opens the log and starts listening. Serving runs
// in a goroutine.
func New(c Config) (*Proxy, error) {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return nil, errors.New("proxy: listen address must be loopback")
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("proxy: listen address must be loopback")
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if c.LogPath == "" {
		return nil, errors.New("proxy: log path required")
	}
	if c.Dial <= 0 {
		c.Dial = defaultDial
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.maxReqBytes <= 0 {
		c.maxReqBytes = defaultMaxReq
	}
	if c.maxRespBytes <= 0 {
		c.maxRespBytes = defaultMaxResp
	}
	if c.maxTunnel <= 0 {
		c.maxTunnel = defaultMaxTunnel
	}
	if c.idleTimeout <= 0 {
		c.idleTimeout = defaultIdle
	}
	rules := make([]Rule, len(c.Rules))
	for i, r := range c.Rules {
		h, ok := normalizeRuleHost(r.Host)
		if !ok {
			return nil, fmt.Errorf("proxy: rule %d has an invalid host", i)
		}
		rules[i] = Rule{Host: h, Critical: r.Critical}
	}
	c.Rules = rules
	if c.resolve == nil {
		c.resolve = func(ctx context.Context, h string) ([]net.IP, error) {
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, h)
			if err != nil {
				return nil, err
			}
			out := make([]net.IP, len(addrs))
			for i, a := range addrs {
				out[i] = a.IP
			}
			return out, nil
		}
	}
	if c.dial == nil {
		c.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
	}
	lf, err := os.OpenFile(c.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("proxy: cannot open log")
	}
	if err := os.Chmod(c.LogPath, 0o600); err != nil {
		lf.Close()
		return nil, errors.New("proxy: cannot secure log")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), port))
	if err != nil {
		lf.Close()
		return nil, errors.New("proxy: cannot listen")
	}
	p := &Proxy{cfg: c, ln: ln, logf: lf, tunnels: map[net.Conn]struct{}{}, serveDone: make(chan struct{})}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.tr = &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialContext,
		DisableKeepAlives:     true,
		DisableCompression:    true,
		ResponseHeaderTimeout: 3 * c.Dial,
		TLSHandshakeTimeout:   c.Dial,
	}
	p.srv = &http.Server{
		Handler:           http.HandlerFunc(p.handle),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() {
		defer close(p.serveDone)
		_ = p.srv.Serve(ln)
	}()
	return p, nil
}

// Addr is the real listen address, such as "127.0.0.1:41733".
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// URL is the proxy URL carrying the node id as userinfo. An id that does not
// match the allowed shape becomes "-".
func (p *Proxy) URL(node string) string {
	if !validNodeID(node) {
		node = "-"
	}
	return "http://node-" + node + "@" + p.Addr()
}

// Close stops accepting, closes open tunnels and the log, and waits up to 5s
// for handlers. It is safe to call twice.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.cancel()
		var errs []error
		if err := p.srv.Close(); err != nil {
			errs = append(errs, errors.New("proxy: server close failed"))
		}
		p.mu.Lock()
		for c := range p.tunnels {
			_ = c.Close()
		}
		p.mu.Unlock()
		done := make(chan struct{})
		go func() { p.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(closeWait):
		}
		<-p.serveDone
		p.tr.CloseIdleConnections()
		p.logMu.Lock()
		if err := p.logf.Close(); err != nil {
			errs = append(errs, errors.New("proxy: log close failed"))
		}
		p.logMu.Unlock()
		p.closeErr = errors.Join(errs...)
	})
	// only the call that performed the shutdown reports its error
	if p.closeReported.CompareAndSwap(false, true) {
		return p.closeErr
	}
	return nil
}

// Failures are requests of node to critical hosts that failed, At >= since.
func (p *Proxy) Failures(node string, since time.Time) []Failure {
	return p.recorded(&p.failures, node, since)
}

// Warnings are the same failures for non-critical hosts.
func (p *Proxy) Warnings(node string, since time.Time) []Failure {
	return p.recorded(&p.warnings, node, since)
}

func (p *Proxy) recorded(src *[]Failure, node string, since time.Time) []Failure {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Failure
	for _, f := range *src {
		if f.Node == node && !f.At.Before(since) {
			out = append(out, f)
		}
	}
	return out
}

func (p *Proxy) record(node, host, kind string, critical bool) {
	f := Failure{Node: node, Host: host, Kind: kind, At: p.cfg.Now()}
	p.mu.Lock()
	defer p.mu.Unlock()
	dst := &p.warnings
	if critical {
		dst = &p.failures
	}
	if len(*dst) >= maxRecorded {
		*dst = append((*dst)[:0], (*dst)[len(*dst)-maxRecorded+1:]...)
	}
	*dst = append(*dst, f)
}

func (p *Proxy) enter() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.wg.Add(1)
	return true
}

func (p *Proxy) addTunnel(cs ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	for _, c := range cs {
		p.tunnels[c] = struct{}{}
	}
	return true
}

func (p *Proxy) dropTunnel(cs ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range cs {
		delete(p.tunnels, c)
	}
}

// logLine is the whole of what the log records.
type logLine struct {
	Time       string `json:"time"`
	Node       string `json:"node"`
	Method     string `json:"method"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Verdict    string `json:"verdict"`
	Status     int    `json:"status"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
}

type reqInfo struct {
	node   string
	method string
	host   string // normalized, or "-" when it could not be parsed
	port   int
	start  time.Time
}

func (p *Proxy) logReq(ri *reqInfo, verdict string, status int, n int64) {
	method := ri.method
	if len(method) > 16 {
		method = method[:16]
	}
	host := ri.host
	if len(host) > 253 {
		host = host[:253]
	}
	b, err := json.Marshal(logLine{
		Time: p.cfg.Now().UTC().Format(time.RFC3339Nano), Node: ri.node, Method: method, Host: host,
		Port: ri.port, Verdict: verdict, Status: status, Bytes: n,
		DurationMS: p.cfg.Now().Sub(ri.start).Milliseconds(),
	})
	if err != nil {
		return
	}
	p.logMu.Lock()
	defer p.logMu.Unlock()
	_, _ = p.logf.Write(append(b, '\n'))
}

func validNodeID(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func nodeOf(r *http.Request) string {
	if a := r.Header.Get("Proxy-Authorization"); len(a) > 6 && strings.EqualFold(a[:6], "Basic ") {
		if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(a[6:])); err == nil {
			user, _, _ := strings.Cut(string(raw), ":")
			if id, ok := strings.CutPrefix(user, "node-"); ok {
				if validNodeID(id) {
					return id
				}
				return "-"
			}
		}
	}
	if v := r.Header.Get("X-GopherMind-Node"); v != "" {
		if validNodeID(v) {
			return v
		}
	}
	return "-"
}

var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade", "X-Gophermind-Node",
}

func stripHeaders(h http.Header) http.Header {
	out := h.Clone()
	for _, v := range h.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				out.Del(tok)
			}
		}
	}
	for _, k := range hopByHop {
		out.Del(k)
	}
	return out
}

func deny(w http.ResponseWriter) {
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusForbidden)
}

func loopbackPeer(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request) {
	if !p.enter() {
		http.Error(w, "", http.StatusServiceUnavailable)
		return
	}
	defer p.wg.Done()
	if !loopbackPeer(r.RemoteAddr) {
		deny(w)
		return
	}
	ri := &reqInfo{node: nodeOf(r), method: r.Method, host: "-", start: p.cfg.Now()}
	if r.Method == http.MethodConnect {
		p.connect(w, r, ri)
		return
	}
	p.forward(w, r, ri)
}

// target parses and authorizes host and port. ok is false for a denial (the
// response is already written); critical is the matched rule's flag.
func (p *Proxy) authorize(w http.ResponseWriter, ri *reqInfo, rawHost, rawPort string) (host, port string, critical, ok bool) {
	n, err := strconv.Atoi(rawPort)
	if err != nil || n < 1 || n > 65535 {
		ri.host = "-"
		http.Error(w, "", http.StatusBadRequest)
		p.logReq(ri, "error", http.StatusBadRequest, 0)
		return "", "", false, false
	}
	ri.port = n
	host, valid := normalizeHost(rawHost)
	if !valid {
		ri.host = "-"
		p.record(ri.node, "-", "denied", false)
		deny(w)
		p.logReq(ri, "denied", http.StatusForbidden, 0)
		return "", "", false, false
	}
	ri.host = host
	allowed, crit := matchRules(p.cfg.Rules, host)
	if !allowed {
		p.record(ri.node, host, "denied", criticalDenied(p.cfg.Rules, host))
		deny(w)
		p.logReq(ri, "denied", http.StatusForbidden, 0)
		return "", "", false, false
	}
	return host, rawPort, crit, true
}

func classify(err error) string {
	if errors.Is(err, errDenied) {
		return "denied"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	return "unreachable"
}

func (p *Proxy) failStatus(w http.ResponseWriter, ri *reqInfo, host string, critical bool, err error) {
	kind := classify(err)
	if kind == "canceled" {
		// the client went away or the proxy is closing: not an upstream failure
		p.logReq(ri, "error", 0, 0)
		return
	}
	p.record(ri.node, host, kind, critical)
	switch kind {
	case "denied":
		deny(w)
		p.logReq(ri, "denied", http.StatusForbidden, 0)
	case "timeout":
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusGatewayTimeout)
		p.logReq(ri, "error", 0, 0)
	default:
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusBadGateway)
		p.logReq(ri, "error", 0, 0)
	}
}

// dialContext is the transport's dialer. The request's host was authorized
// by rule; a name is resolved once, every answer is checked against the deny
// list and the connection goes to the resolved IP.
func (p *Proxy) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	h, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errDenied
	}
	host, ok := normalizeHost(h)
	if !ok {
		return nil, errDenied
	}
	return p.dialHost(ctx, host, port)
}

func (p *Proxy) dialHost(ctx context.Context, host, port string) (net.Conn, error) {
	if allowed, _ := matchRules(p.cfg.Rules, host); !allowed {
		return nil, errDenied
	}
	if isIPLiteral(host) {
		return p.dialOne(ctx, net.JoinHostPort(host, port))
	}
	rctx, cancel := context.WithTimeout(ctx, p.cfg.Dial)
	ips, err := p.cfg.resolve(rctx, host)
	cancel()
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("proxy: no addresses")
	}
	for _, ip := range ips {
		if forbiddenIP(ip) {
			return nil, errDenied
		}
	}
	if len(ips) > maxDialIPs {
		ips = ips[:maxDialIPs]
	}
	var last error
	for _, ip := range ips {
		c, err := p.dialOne(ctx, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

func (p *Proxy) dialOne(ctx context.Context, addr string) (net.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, p.cfg.Dial)
	defer cancel()
	return p.cfg.dial(dctx, "tcp", addr)
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, ri *reqInfo) {
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "", http.StatusBadRequest)
		p.logReq(ri, "error", http.StatusBadRequest, 0)
		return
	}
	port := r.URL.Port()
	if port == "" {
		port = "80"
	}
	host, _, critical, ok := p.authorize(w, ri, r.URL.Hostname(), port)
	if !ok {
		return
	}
	if r.ContentLength > p.cfg.maxReqBytes {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		p.logReq(ri, "error", http.StatusRequestEntityTooLarge, 0)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxRequestDuration)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()

	out := r.Clone(ctx)
	out.RequestURI = ""
	out.Header = stripHeaders(r.Header)
	out.Host = r.URL.Host
	out.Close = false
	if r.Body != nil && r.Body != http.NoBody {
		out.Body = http.MaxBytesReader(w, r.Body, p.cfg.maxReqBytes)
	}
	resp, err := p.tr.RoundTrip(out)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			p.logReq(ri, "error", http.StatusRequestEntityTooLarge, 0)
			return
		}
		p.failStatus(w, ri, host, critical, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		p.record(ri.node, host, "5xx", critical)
	}
	if resp.ContentLength > p.cfg.maxRespBytes {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusBadGateway)
		p.logReq(ri, "error", 0, 0)
		return
	}
	for k, vs := range stripHeaders(resp.Header) {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	n, cerr := io.Copy(w, io.LimitReader(resp.Body, p.cfg.maxRespBytes))
	truncated := false
	if cerr == nil && n == p.cfg.maxRespBytes {
		var one [1]byte
		if m, _ := resp.Body.Read(one[:]); m > 0 {
			truncated = true
		}
	}
	if cerr != nil || truncated {
		p.logReq(ri, "error", resp.StatusCode, n)
		panic(http.ErrAbortHandler)
	}
	p.logReq(ri, "allowed", resp.StatusCode, n)
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request, ri *reqInfo) {
	h, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		p.logReq(ri, "error", http.StatusBadRequest, 0)
		return
	}
	host, _, critical, ok := p.authorize(w, ri, h, port)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxRequestDuration)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	upstream, err := p.dialHost(ctx, host, port)
	if err != nil {
		p.failStatus(w, ri, host, critical, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "", http.StatusInternalServerError)
		p.logReq(ri, "error", 0, 0)
		return
	}
	client, bufrw, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		p.logReq(ri, "error", 0, 0)
		return
	}
	if !p.addTunnel(client, upstream) {
		client.Close()
		upstream.Close()
		return
	}
	defer p.dropTunnel(client, upstream)
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		client.Close()
		upstream.Close()
		p.logReq(ri, "error", 0, 0)
		return
	}
	dl := p.cfg.Now().Add(maxTunnelDuration)
	_ = client.SetDeadline(dl)
	_ = upstream.SetDeadline(dl)
	t := &tunnelState{max: p.cfg.maxTunnel, idle: p.cfg.idleTimeout, now: p.cfg.Now}
	t.last.Store(p.cfg.Now().UnixNano())
	var clientIn io.Reader = client
	if bufrw != nil {
		clientIn = bufrw.Reader
	}
	done := make(chan struct{}, 2)
	go func() { t.pipe(upstream, clientIn, client); done <- struct{}{} }()
	go func() { t.pipe(client, upstream, upstream); done <- struct{}{} }()
	<-done
	client.Close()
	upstream.Close()
	<-done
	switch t.reason.Load() {
	case reasonCap:
		p.record(ri.node, host, "tunnel_cap", false)
	case reasonIdle:
		p.record(ri.node, host, "tunnel_idle", false)
	}
	p.logReq(ri, "allowed", 0, t.total.Load())
}

const (
	reasonCap  = 1
	reasonIdle = 2
)

// tunnelState is shared by both directions of one CONNECT tunnel: a total
// byte cap, and an idle timeout counted from the last byte in either direction.
type tunnelState struct {
	max    int64
	idle   time.Duration
	now    func() time.Time
	total  atomic.Int64
	last   atomic.Int64
	reason atomic.Int32
}

func (t *tunnelState) pipe(dst io.Writer, src io.Reader, srcConn net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		_ = srcConn.SetReadDeadline(t.now().Add(t.idle))
		n, err := src.Read(buf)
		if n > 0 {
			t.last.Store(t.now().UnixNano())
			if t.total.Add(int64(n)) > t.max {
				t.reason.CompareAndSwap(0, reasonCap)
				return
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				if t.now().UnixNano()-t.last.Load() < int64(t.idle) {
					continue // the other direction is active
				}
				t.reason.CompareAndSwap(0, reasonIdle)
			}
			return
		}
	}
}
