package llm

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// TLSOptions configures how the client establishes TLS to the endpoint. The
// zero value selects today's behavior exactly: a default transport with system
// root verification (or, when InsecureSkipVerify is set, no verification).
//
// The fields support secure internal deployments WITHOUT disabling
// verification:
//
//   - ClientCertPath + ClientKeyPath enable mutual TLS: the client presents a
//     certificate the server can verify. Both are required together; supplying
//     one without the other is a configuration error.
//   - CACertPath supplies a custom CA bundle (PEM) to trust for the SERVER,
//     for internal/private CAs, while keeping verification ON. It is appended
//     to a clone of the system root pool so public endpoints still verify.
//
// Precedence with InsecureSkipVerify: when InsecureSkipVerify is true,
// verification is OFF (preserving the existing -insecure behavior) and a custom
// CA pool is irrelevant, but a configured client certificate is STILL presented
// so an mTLS server reached over a trusted VPN keeps working. Prefer the secure
// path (CA cert, no InsecureSkipVerify) for new deployments.
type TLSOptions struct {
	InsecureSkipVerify bool
	ClientCertPath     string
	ClientKeyPath      string
	CACertPath         string
}

// configured reports whether any non-insecure TLS customization is requested.
func (o TLSOptions) configured() bool {
	return o.ClientCertPath != "" || o.ClientKeyPath != "" || o.CACertPath != ""
}

// buildTLSConfig validates the options and builds a *tls.Config, or returns nil
// when no TLS customization is needed at all (the caller then uses the default
// transport, preserving prior behavior). Errors are config-time and fail fast:
// a missing/unreadable/malformed cert, key, or CA file produces a clear error,
// never a panic and never a silent fallback to no-cert or to system roots.
//
// Security notes:
//   - MinVersion is pinned to TLS 1.2 whenever this builds a config.
//   - The client key is loaded only via tls.LoadX509KeyPair; its bytes are
//     never read into our own buffers, never logged, and never embedded in an
//     error message.
//   - A custom CA fails closed: a missing/garbage CA file is an error, not a
//     downgrade to system roots that the operator did not ask for.
func buildTLSConfig(o TLSOptions) (*tls.Config, error) {
	// Exactly-one-of cert/key is always wrong, regardless of the insecure flag.
	if (o.ClientCertPath != "") != (o.ClientKeyPath != "") {
		return nil, fmt.Errorf("client certificate auth requires BOTH a cert and a key: " +
			"set GOPHERMIND_CLIENT_CERT and GOPHERMIND_CLIENT_KEY together")
	}

	// Nothing custom requested and not insecure: signal "use default transport".
	if !o.configured() && !o.InsecureSkipVerify {
		return nil, nil
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if o.InsecureSkipVerify {
		// Preserve existing -insecure behavior: verification off.
		cfg.InsecureSkipVerify = true
	}

	// Client certificate (mutual TLS). LoadX509KeyPair reads both files itself,
	// so the private key never passes through our code or any log/error string.
	if o.ClientCertPath != "" {
		cert, err := tls.LoadX509KeyPair(o.ClientCertPath, o.ClientKeyPath)
		if err != nil {
			// Note: this error may reference the file PATHS but never key CONTENTS;
			// LoadX509KeyPair does not echo key bytes.
			return nil, fmt.Errorf("load client certificate/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	// Custom CA for verifying the SERVER. Only meaningful when verification is on;
	// when InsecureSkipVerify is set, RootCAs is ignored by crypto/tls anyway, but
	// we still validate and load it so a misconfiguration is surfaced loudly
	// rather than silently ignored.
	if o.CACertPath != "" {
		pool, err := caPool(o.CACertPath)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}

// caPool builds a RootCAs pool from a PEM file. It starts from a CLONE of the
// system root pool (so public endpoints still verify) and appends the provided
// CA. If the system pool is unavailable, it starts from an empty pool seeded
// only with the provided CA. A missing/unreadable/garbage CA file is an error
// (fail closed) — never a silent fallback to system-only roots.
func caPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate %q: %w", path, err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		// Document: degrade to a pool containing ONLY the provided CA rather than
		// failing. This still fails closed (the server must chain to the given CA);
		// it just means public roots are not also trusted on this platform.
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA certificate %q contains no valid PEM certificates", path)
	}
	return pool, nil
}

// httpClientFor builds an *http.Client for the given timeout and TLS options.
// When the options need no TLS customization, it returns a plain client with
// the default transport — byte-for-byte the prior behavior. Otherwise it
// installs a transport whose TLSClientConfig is the validated config. Returns a
// config-time error so startup fails fast on bad cert/key/CA input.
func httpClientFor(timeout time.Duration, o TLSOptions) (*http.Client, error) {
	tlsCfg, err := buildTLSConfig(o)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	if tlsCfg != nil {
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}
	return client, nil
}
