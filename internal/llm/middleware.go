package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Middleware wraps an http.RoundTripper, returning a new RoundTripper that may
// inspect or modify the outgoing request and the incoming response before
// delegating to next. This is the single extensibility seam for the HTTP layer:
// logging, redaction, header injection, etc. — without forking the client.
//
// Because every request (both Complete and Stream) flows through the same
// *http.Client, a Middleware installed via Client.Use covers BOTH paths
// uniformly.
//
// Contract for middleware authors:
//
//   - DO NOT consume the request body unless you restore it. The request body
//     is a single-use io.ReadCloser; reading it without resetting req.Body (or
//     using req.GetBody) sends an empty/truncated body to the server. Prefer not
//     to read the body at all. The built-in middlewares never read it.
//   - DO NOT read the RESPONSE body. For streaming (SSE) responses, reading the
//     body buffers/consumes the stream and breaks incremental delivery. Inspect
//     only status and headers. The built-in logging middleware reads neither
//     request nor response bodies.
//   - Returning an error from the wrapped RoundTripper fails the request: it is
//     surfaced to the caller as a transport error (Complete/Stream treat it like
//     any other transport failure — retryable/fallback-eligible per existing
//     policy). A request hook that returns an error aborts the request WITHOUT
//     hitting the network.
type Middleware func(http.RoundTripper) http.RoundTripper

// chainMiddleware wraps base with the given middlewares so that the FIRST
// middleware in the slice is the OUTERMOST wrapper — i.e. its request hook runs
// first on the way out and its response hook runs last on the way back. When mws
// is empty it returns base unchanged, so a client with no registered middleware
// has exactly today's behavior and zero wrapping overhead.
func chainMiddleware(base http.RoundTripper, mws []Middleware) http.RoundTripper {
	if len(mws) == 0 {
		return base
	}
	rt := base
	// Apply in reverse so index 0 ends up outermost (runs first on request).
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			rt = mws[i](rt)
		}
	}
	return rt
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// RequestHook inspects (and may mutate, e.g. set headers on) an outgoing request
// before it is sent. Returning an error ABORTS the request: it is surfaced as a
// transport error and the network is never touched.
//
// A RequestHook MUST NOT consume req.Body without restoring it (see Middleware
// contract). Header mutation is safe and is the intended use.
type RequestHook func(*http.Request) error

// ResponseHook inspects an incoming response (status, headers) after a
// successful round-trip. Returning an error fails the request, surfacing the
// error to the caller (the response body is closed by the wrapper in that case
// so the connection is not leaked).
//
// A ResponseHook MUST NOT read resp.Body: for streaming responses that would
// buffer the SSE stream and break incremental delivery.
type ResponseHook func(*http.Response) error

// HookMiddleware builds a Middleware from an optional request hook and an
// optional response hook (either may be nil). It hardens the seam: a panic in a
// hook is recovered and converted to a transport error rather than crashing the
// process. The request hook runs before the request is sent (and aborts it on
// error); the response hook runs after a successful round-trip (and fails the
// request on error, closing the body).
func HookMiddleware(reqHook RequestHook, respHook ResponseHook) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (resp *http.Response, err error) {
			if reqHook != nil {
				if herr := safeReqHook(reqHook, req); herr != nil {
					// Abort: never hit the network.
					return nil, fmt.Errorf("request hook: %w", herr)
				}
			}
			resp, err = next.RoundTrip(req)
			if err != nil {
				return nil, err
			}
			if respHook != nil {
				if herr := safeRespHook(respHook, resp); herr != nil {
					// Fail the request; close the body so we don't leak the connection.
					if resp.Body != nil {
						resp.Body.Close()
					}
					return nil, fmt.Errorf("response hook: %w", herr)
				}
			}
			return resp, nil
		})
	}
}

// safeReqHook runs h, converting a panic into an error so a misbehaving hook
// cannot crash the agent.
func safeReqHook(h RequestHook, req *http.Request) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in request hook: %v", r)
		}
	}()
	return h(req)
}

// safeRespHook runs h, converting a panic into an error.
func safeRespHook(h ResponseHook, resp *http.Response) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in response hook: %v", r)
		}
	}()
	return h(resp)
}

// HeaderInjector returns a Middleware that sets the given header on every
// outgoing request, overwriting any existing value. Use it to inject a static
// header (e.g. a tenant id, a tracing header, or an extra auth header) without
// modifying call sites.
//
// Security note: net/http rejects header values containing CR/LF at write time,
// so a value cannot smuggle additional headers (response splitting). The name
// and value are caller-trusted; injecting an empty name is a no-op.
func HeaderInjector(name, value string) Middleware {
	return HookMiddleware(func(req *http.Request) error {
		if name == "" {
			return nil
		}
		req.Header.Set(name, value)
		return nil
	}, nil)
}

// sensitiveHeaders is the default deny-list whose VALUES are never logged. Names
// are matched case-insensitively. In addition to these exact names, any header
// whose name contains "token" or "api-key" / "apikey" (case-insensitive) is also
// redacted, so provider-specific auth headers are covered without enumerating
// every one.
var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
}

// isSensitiveHeader reports whether a header's value must be redacted in logs.
func isSensitiveHeader(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := sensitiveHeaders[lower]; ok {
		return true
	}
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "api-key") ||
		strings.Contains(lower, "apikey")
}

// LoggingMiddleware returns a Middleware that writes a one-line record of each
// request to w: method, URL, response status, and wall-clock duration. It NEVER
// logs sensitive header VALUES (Authorization, Proxy-Authorization, Cookie,
// Set-Cookie, and anything matching token/api-key) — those are emitted as
// "<redacted>" — and it NEVER reads or logs the request or response BODY, so it
// can neither leak the bearer token / API key nor buffer a streaming response.
//
// A nil w disables logging (the middleware becomes a no-op pass-through).
func LoggingMiddleware(w io.Writer) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if w == nil {
				return next.RoundTrip(req)
			}
			start := time.Now()
			resp, err := next.RoundTrip(req)
			dur := time.Since(start)
			method := req.Method
			url := req.URL.Redacted() // strips userinfo (user:password@) from the URL
			if err != nil {
				fmt.Fprintf(w, "llm http %s %s -> error after %s: %v\n", method, url, dur, err)
				return resp, err
			}
			fmt.Fprintf(w, "llm http %s %s -> %d in %s\n", method, url, resp.StatusCode, dur)
			return resp, nil
		})
	}
}

// JSONLoggingMiddleware is like LoggingMiddleware but emits one JSON object per
// request (fields: time, method, url, status, duration_ms, and error when the
// round-trip failed) for machine-readable logs. It applies the same safety
// guarantees: it never reads bodies and the URL is redacted of userinfo, so no
// bearer token / API key is logged. A nil w disables it.
func JSONLoggingMiddleware(w io.Writer) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if w == nil {
				return next.RoundTrip(req)
			}
			start := time.Now()
			resp, err := next.RoundTrip(req)
			rec := map[string]any{
				"time":        start.UTC().Format(time.RFC3339Nano),
				"method":      req.Method,
				"url":         req.URL.Redacted(),
				"duration_ms": time.Since(start).Milliseconds(),
			}
			if err != nil {
				rec["error"] = err.Error()
			} else {
				rec["status"] = resp.StatusCode
			}
			b, _ := json.Marshal(rec)
			fmt.Fprintf(w, "%s\n", b)
			return resp, err
		})
	}
}

// redactHeaders returns a copy of h with sensitive values replaced by
// "<redacted>". It is exported-adjacent helper kept unexported; callers that
// want to log headers safely can build on LoggingMiddleware instead. Provided
// for any future header-dumping hook so redaction logic lives in one place.
func redactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, vals := range h {
		if isSensitiveHeader(name) {
			out[name] = []string{"<redacted>"}
			continue
		}
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[name] = cp
	}
	return out
}
