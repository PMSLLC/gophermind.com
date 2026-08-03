package mcpclient

import "net/http"

// Authenticator attaches credentials to an outbound MCP request.
//
// This is the seam OAuth 2.1 plugs into later: an OAuth implementation refreshes
// its token and sets the Authorization header inside Apply, and http.go needs no
// change to support it.
type Authenticator interface {
	Apply(req *http.Request) error
}

// staticHeaders authenticates with a fixed set of headers, already expanded
// from ${VAR} references at config load. It covers bearer tokens, x-api-key,
// and any other header-carried scheme.
type staticHeaders struct {
	headers map[string]string
}

func newStaticHeaders(h map[string]string) *staticHeaders {
	// Copy so a later config mutation cannot change live credentials.
	cp := make(map[string]string, len(h))
	for k, v := range h {
		cp[k] = v
	}
	return &staticHeaders{headers: cp}
}

func (s *staticHeaders) Apply(req *http.Request) error {
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	return nil
}
