package llm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- ephemeral PKI helpers (throwaway, in-test only) -----------------------

type certPEM struct {
	certPEM []byte
	keyPEM  []byte
	cert    *x509.Certificate
	priv    *ecdsa.PrivateKey
}

// mintCA creates a self-signed CA certificate.
func mintCA(t *testing.T) certPEM {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gophermind-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	return certPEM{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  encodeKey(t, priv),
		cert:    cert,
		priv:    priv,
	}
}

// mintLeaf signs a leaf cert (server or client) with the given CA.
func mintLeaf(t *testing.T, ca certPEM, cn string, server bool, dnsNames []string, ips []net.IP) certPEM {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen leaf key: %v", err)
	}
	usage := x509.ExtKeyUsageClientAuth
	if server {
		usage = x509.ExtKeyUsageServerAuth
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &priv.PublicKey, ca.priv)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	return certPEM{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  encodeKey(t, priv),
		cert:    cert,
		priv:    priv,
	}
}

func encodeKey(t *testing.T, priv *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
}

// writePEM writes data to a file under dir and returns the path.
func writePEM(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// --- buildTLSConfig unit tests --------------------------------------------

func TestBuildTLSConfig_NoOptions_ReturnsNil(t *testing.T) {
	cfg, err := buildTLSConfig(TLSOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for zero options (default transport), got %+v", cfg)
	}
}

func TestBuildTLSConfig_Insecure_SetsSkipVerifyAndMinVersion(t *testing.T) {
	cfg, err := buildTLSConfig(TLSOptions{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify=true")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS1.2", cfg.MinVersion)
	}
}

func TestBuildTLSConfig_CertAndKey_BuildsOneCertificate(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	client := mintLeaf(t, ca, "client", false, nil, nil)
	certPath := writePEM(t, dir, "client.crt", client.certPEM)
	keyPath := writePEM(t, dir, "client.key", client.keyPEM)

	cfg, err := buildTLSConfig(TLSOptions{ClientCertPath: certPath, ClientKeyPath: keyPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected exactly one certificate, got %+v", cfg)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS1.2", cfg.MinVersion)
	}
	if cfg.InsecureSkipVerify {
		t.Error("mTLS path must NOT disable verification")
	}
}

func TestBuildTLSConfig_OnlyCert_IsError(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	client := mintLeaf(t, ca, "client", false, nil, nil)
	certPath := writePEM(t, dir, "client.crt", client.certPEM)

	_, err := buildTLSConfig(TLSOptions{ClientCertPath: certPath})
	if err == nil {
		t.Fatal("expected error when only the cert is provided")
	}
}

func TestBuildTLSConfig_OnlyKey_IsError(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	client := mintLeaf(t, ca, "client", false, nil, nil)
	keyPath := writePEM(t, dir, "client.key", client.keyPEM)

	_, err := buildTLSConfig(TLSOptions{ClientKeyPath: keyPath})
	if err == nil {
		t.Fatal("expected error when only the key is provided")
	}
}

func TestBuildTLSConfig_CACert_BuildsRootPool(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	caPath := writePEM(t, dir, "ca.crt", ca.certPEM)

	cfg, err := buildTLSConfig(TLSOptions{CACertPath: caPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || cfg.RootCAs == nil {
		t.Fatal("expected a RootCAs pool")
	}
	if cfg.InsecureSkipVerify {
		t.Error("custom-CA path must keep verification ON")
	}
}

func TestBuildTLSConfig_MissingCAFile_IsError(t *testing.T) {
	_, err := buildTLSConfig(TLSOptions{CACertPath: filepath.Join(t.TempDir(), "nope.pem")})
	if err == nil {
		t.Fatal("expected error for a missing CA file (must fail closed, not fall back to system roots)")
	}
}

func TestBuildTLSConfig_GarbageCAFile_IsError(t *testing.T) {
	dir := t.TempDir()
	bad := writePEM(t, dir, "ca.crt", []byte("this is not a PEM certificate"))
	_, err := buildTLSConfig(TLSOptions{CACertPath: bad})
	if err == nil {
		t.Fatal("expected error for a garbage CA file")
	}
}

func TestBuildTLSConfig_MalformedKey_IsError(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	client := mintLeaf(t, ca, "client", false, nil, nil)
	certPath := writePEM(t, dir, "client.crt", client.certPEM)
	keyPath := writePEM(t, dir, "client.key", []byte("-----BEGIN EC PRIVATE KEY-----\ngarbage\n-----END EC PRIVATE KEY-----\n"))

	_, err := buildTLSConfig(TLSOptions{ClientCertPath: certPath, ClientKeyPath: keyPath})
	if err == nil {
		t.Fatal("expected error for a malformed key")
	}
	// Security: the error must not contain raw key bytes.
	if containsAny(err.Error(), "garbage") {
		t.Errorf("error leaked key contents: %v", err)
	}
}

func containsAny(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- Timeout: 0 / ResponseHeaderTimeout regression guards ------------------
//
// These guard against reintroducing http.Client.Timeout as a total-request
// cap, which killed streaming turns mid-stream (see stream_test.go for the
// idle-watchdog tests that replace it). The client must have NO total cap and
// a non-zero ResponseHeaderTimeout so a dead endpoint still fails fast before
// any bytes/tokens arrive.

func TestHTTPClientFor_NoTLS_TimeoutZeroAndResponseHeaderTimeoutSet(t *testing.T) {
	client, err := httpClientFor(5*time.Second, TLSOptions{})
	if err != nil {
		t.Fatalf("httpClientFor: %v", err)
	}
	if client.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0 (no total cap; Complete bounds itself via context, Stream via idle watchdog)", client.Timeout)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout must be non-zero so a dead endpoint fails fast before headers arrive")
	}
}

func TestHTTPClientFor_WithTLSConfig_TimeoutZeroAndResponseHeaderTimeoutSet(t *testing.T) {
	client, err := httpClientFor(5*time.Second, TLSOptions{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("httpClientFor: %v", err)
	}
	if client.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", client.Timeout)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout must be non-zero even when a custom TLS config is set")
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLSClientConfig was not applied to the cloned transport")
	}
}

func TestNewWithTLS_ClientHasNoTotalCap(t *testing.T) {
	c, err := NewWithTLS("http://example.invalid", "", "m", 5*time.Second, TLSOptions{})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	if c.HTTP.Timeout != 0 {
		t.Errorf("HTTP.Timeout = %v, want 0", c.HTTP.Timeout)
	}
}

// --- end-to-end handshake tests -------------------------------------------

// newMTLSServer starts an httptest TLS server that REQUIRES and verifies a
// client certificate signed by the given CA. It returns the server.
func newMTLSServer(t *testing.T, serverCert tls.Certificate, clientCAPool *x509.CertPool) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestMTLS_EndToEnd_AcceptsConfiguredClient(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)

	// Server cert signed by the CA, valid for 127.0.0.1.
	server := mintLeaf(t, ca, "localhost", true, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	serverTLSCert, err := tls.X509KeyPair(server.certPEM, server.keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}

	// Client cert signed by the same CA.
	client := mintLeaf(t, ca, "client", false, nil, nil)
	certPath := writePEM(t, dir, "client.crt", client.certPEM)
	keyPath := writePEM(t, dir, "client.key", client.keyPEM)
	caPath := writePEM(t, dir, "ca.crt", ca.certPEM)

	// Server requires a client cert chaining to our CA.
	clientCAPool := x509.NewCertPool()
	clientCAPool.AppendCertsFromPEM(ca.certPEM)
	srv := newMTLSServer(t, serverTLSCert, clientCAPool)

	c, err := NewWithTLS(srv.URL, "", "m", 5*time.Second, TLSOptions{
		ClientCertPath: certPath,
		ClientKeyPath:  keyPath,
		CACertPath:     caPath, // trust the server's CA
	})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("mTLS request should succeed when client cert is configured: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMTLS_EndToEnd_RejectsWhenClientCertAbsent(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)

	server := mintLeaf(t, ca, "localhost", true, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	serverTLSCert, err := tls.X509KeyPair(server.certPEM, server.keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	caPath := writePEM(t, dir, "ca.crt", ca.certPEM)

	clientCAPool := x509.NewCertPool()
	clientCAPool.AppendCertsFromPEM(ca.certPEM)
	srv := newMTLSServer(t, serverTLSCert, clientCAPool)

	// Trust the server's CA but present NO client cert; the server must reject.
	c, err := NewWithTLS(srv.URL, "", "m", 5*time.Second, TLSOptions{CACertPath: caPath})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.HTTP.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected handshake/request failure when no client cert is presented to an mTLS server")
	}
}

func TestCACert_EndToEnd_TrustsPrivateCA(t *testing.T) {
	dir := t.TempDir()
	ca := mintCA(t)
	server := mintLeaf(t, ca, "localhost", true, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	serverTLSCert, err := tls.X509KeyPair(server.certPEM, server.keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	caPath := writePEM(t, dir, "ca.crt", ca.certPEM)

	// A normal TLS server (no client-cert requirement) using our private CA.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverTLSCert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	// With the custom CA, verification stays ON and succeeds.
	c, err := NewWithTLS(srv.URL, "", "m", 5*time.Second, TLSOptions{CACertPath: caPath})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("request with trusted private CA should succeed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Without the CA (and not insecure), verification must FAIL for the same
	// server — proving the CA pool is what enabled trust, not a downgrade.
	plain, err := NewWithTLS(srv.URL, "", "m", 5*time.Second, TLSOptions{})
	if err != nil {
		t.Fatalf("NewWithTLS plain: %v", err)
	}
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if resp2, err := plain.HTTP.Do(req2); err == nil {
		resp2.Body.Close()
		t.Fatal("expected verification failure against a private CA without the CA bundle")
	}
}

func TestInsecureTLS_StillSkipsVerify(t *testing.T) {
	// httptest's default TLS server uses a cert not in system roots; -insecure
	// must still connect, exactly as before.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, true) // insecure
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("insecure client should connect to a self-signed server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestInsecureWithClientCert_PresentsCertAndSkipsVerify(t *testing.T) {
	// Precedence check: InsecureSkipVerify=true keeps verification off but a
	// configured client cert is still presented to an mTLS server.
	dir := t.TempDir()
	ca := mintCA(t)
	server := mintLeaf(t, ca, "localhost", true, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	serverTLSCert, _ := tls.X509KeyPair(server.certPEM, server.keyPEM)
	clientLeaf := mintLeaf(t, ca, "client", false, nil, nil)
	certPath := writePEM(t, dir, "client.crt", clientLeaf.certPEM)
	keyPath := writePEM(t, dir, "client.key", clientLeaf.keyPEM)

	clientCAPool := x509.NewCertPool()
	clientCAPool.AppendCertsFromPEM(ca.certPEM)
	srv := newMTLSServer(t, serverTLSCert, clientCAPool)

	// No CA bundle for the server (would normally fail verify) but insecure=true
	// skips server verification, while the client cert is still presented.
	c, err := NewWithTLS(srv.URL, "", "m", 5*time.Second, TLSOptions{
		InsecureSkipVerify: true,
		ClientCertPath:     certPath,
		ClientKeyPath:      keyPath,
	})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("insecure+client-cert should still satisfy the mTLS server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
