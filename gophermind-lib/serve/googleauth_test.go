package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testClientID = "client-123.apps.googleusercontent.com"
	testSessKey  = "0123456789abcdef0123456789abcdef-session-key"
	testBearer   = "server-bearer-token"
)

// fakeGoogle is an httptest stand-in for Google's token endpoint. claims is
// the ID token payload it hands back for any code.
type fakeGoogle struct {
	srv    *httptest.Server
	claims map[string]any
	gotReq url.Values
}

func newFakeGoogle(t *testing.T, claims map[string]any) *fakeGoogle {
	t.Helper()
	f := &fakeGoogle{claims: claims}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.gotReq = r.PostForm
		payload, _ := json.Marshal(f.claims)
		idt := "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idt})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func goodClaims() map[string]any {
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": testClientID,
		"exp": time.Now().Add(time.Hour).Unix(), "email": "john@brahy.com",
		"email_verified": true, "hd": "brahy.com",
	}
}

func newTestAuth(f *fakeGoogle) *GoogleAuth {
	return &GoogleAuth{
		ClientID: testClientID, ClientSecret: "client-secret", AllowedDomain: "brahy.com",
		SessionSecret: testSessKey, PublicURL: "https://gophermind.example",
		BearerToken: testBearer, AuthURL: "https://accounts.example/authorize",
		TokenURL: f.srv.URL, HTTPClient: f.srv.Client(),
	}
}

// authedHandler echoes the Authorization header the wrapped handler saw.
func authedHandler(a *GoogleAuth) http.Handler {
	mux := http.NewServeMux()
	a.Register(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "run:"+r.Header.Get("Authorization")) })
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		io.WriteString(w, r.Header.Get("Authorization")+"|"+r.Header.Get("X-Hub-Signature-256")+"|"+string(b))
	})
	return a.Middleware(mux)
}

func do(h http.Handler, method, target string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return doBody(h, method, target, "", hdr, cookies...)
}

func doBody(h http.Handler, method, target, body string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// startLogin runs /auth/login and returns the state param and state cookie.
func startLogin(t *testing.T, h http.Handler) (string, *http.Cookie) {
	t.Helper()
	w := do(h, "GET", "/auth/login", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("login status %d", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := loc.Query()
	if q.Get("client_id") != testClientID || q.Get("hd") != "brahy.com" ||
		q.Get("redirect_uri") != "https://gophermind.example/auth/callback" ||
		q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), "email") {
		t.Fatalf("bad authorize url: %s", loc)
	}
	c := cookieNamed(w, "gm_state")
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 600 {
		t.Fatalf("bad state cookie: %+v", c)
	}
	return q.Get("state"), c
}

func callback(h http.Handler, state string, c *http.Cookie) *httptest.ResponseRecorder {
	return do(h, "GET", "/auth/callback?code=abc&state="+url.QueryEscape(state), nil, c)
}

func TestGoogleAuthHappyPathInjectsBearer(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	h := authedHandler(newTestAuth(f))
	state, sc := startLogin(t, h)
	w := callback(h, state, sc)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	sess := cookieNamed(w, "gm_session")
	if sess == nil || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode || sess.MaxAge != 12*3600 {
		t.Fatalf("bad session cookie: %+v", sess)
	}
	if f.gotReq.Get("client_secret") != "client-secret" || f.gotReq.Get("code") != "abc" ||
		f.gotReq.Get("redirect_uri") != "https://gophermind.example/auth/callback" {
		t.Fatalf("bad token request: %v", f.gotReq)
	}
	w = do(h, "GET", "/api", map[string]string{"Authorization": "Bearer spoofed"}, sess)
	if got := w.Body.String(); got != "Bearer "+testBearer+"||" {
		t.Fatalf("handler saw %q", got)
	}
}

func TestGoogleAuthSignsBodyWhenHMACSet(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_HMAC_SECRET", "hmac-secret")
	f := newFakeGoogle(t, goodClaims())
	a := newTestAuth(f)
	h := authedHandler(a)
	sess := a.sessionCookie("john@brahy.com", time.Now())
	w := doBody(h, "POST", "/api", `{"x":1}`, nil, sess)
	parts := strings.SplitN(w.Body.String(), "|", 3)
	if len(parts) != 3 || parts[2] != `{"x":1}` || !verifyHMAC("hmac-secret", []byte(`{"x":1}`), parts[1]) {
		t.Fatalf("body not signed/restored: %q", w.Body.String())
	}
}

func TestGoogleAuthCallbackRejections(t *testing.T) {
	mutate := func(k string, v any) map[string]any {
		c := goodClaims()
		c[k] = v
		return c
	}
	cases := map[string]map[string]any{
		"wrong domain":        mutate("email", "john@gmail.com"),
		"unverified email":    mutate("email_verified", false),
		"wrong aud":           mutate("aud", "someone-else"),
		"wrong iss":           mutate("iss", "https://evil.example"),
		"expired":             mutate("exp", time.Now().Add(-time.Minute).Unix()),
		"lookalike prefix":    mutate("email", "john@evil-brahy.com"),
		"lookalike suffix":    mutate("email", "a@brahy.com.evil.com"),
		"two ats":             mutate("email", "a@evil.com@brahy.com"),
		"hd alone is no pass": mutate("email", "john@gmail.com"),
	}
	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGoogle(t, claims)
			h := authedHandler(newTestAuth(f))
			state, sc := startLogin(t, h)
			w := callback(h, state, sc)
			if w.Code != http.StatusForbidden || cookieNamed(w, "gm_session") != nil {
				t.Fatalf("want 403 and no session, got %d", w.Code)
			}
		})
	}
}

func TestGoogleAuthStateMismatchAndForgery(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	a := newTestAuth(f)
	h := authedHandler(a)
	state, sc := startLogin(t, h)

	if w := callback(h, "other-state", sc); w.Code != http.StatusBadRequest {
		t.Fatalf("state mismatch: %d", w.Code)
	}
	if w := do(h, "GET", "/auth/callback?code=abc&state="+url.QueryEscape(state), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing cookie: %d", w.Code)
	}
	forged := &http.Cookie{Name: "gm_state", Value: sc.Value + "x"}
	if w := callback(h, state, forged); w.Code != http.StatusBadRequest {
		t.Fatalf("forged state cookie: %d", w.Code)
	}
	// A session cookie must not be accepted as a state cookie.
	sess := a.sessionCookie("john@brahy.com", time.Now())
	if w := callback(h, state, &http.Cookie{Name: "gm_state", Value: sess.Value}); w.Code != http.StatusBadRequest {
		t.Fatalf("cross-kind cookie: %d", w.Code)
	}
	// An expired state cookie is rejected even with a valid signature.
	old := a.stateCookie(state, time.Now().Add(-time.Hour))
	if w := callback(h, state, old); w.Code != http.StatusBadRequest {
		t.Fatalf("expired state cookie: %d", w.Code)
	}
}

func TestGoogleAuthSessionCookieChecks(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	a := newTestAuth(f)
	h := authedHandler(a)

	forged := a.sessionCookie("john@brahy.com", time.Now())
	forged.Value = forged.Value[:len(forged.Value)-2] + "AA"
	other := newTestAuth(f)
	other.SessionSecret = strings.Repeat("z", 40)
	for name, c := range map[string]*http.Cookie{
		"forged":       forged,
		"expired":      a.sessionCookie("john@brahy.com", time.Now().Add(-13*time.Hour)),
		"other secret": other.sessionCookie("john@brahy.com", time.Now()),
		"garbage":      {Name: "gm_session", Value: "nope"},
	} {
		w := do(h, "GET", "/api", nil, c)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", name, w.Code)
		}
	}
}

func TestGoogleAuthMiddlewareRouting(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	h := authedHandler(newTestAuth(f))

	for _, p := range []string{"/healthz", "/auth/login"} {
		if w := do(h, "GET", p, nil); w.Code == http.StatusUnauthorized || w.Code == http.StatusSeeOther {
			t.Errorf("%s should be public, got %d", p, w.Code)
		}
	}
	// /run is public to the middleware and keeps the client's own header.
	if w := do(h, "POST", "/run", map[string]string{"Authorization": "Bearer abc"}); w.Body.String() != "run:Bearer abc" {
		t.Errorf("/run: %q", w.Body.String())
	}
	// Browser navigation redirects to login.
	w := do(h, "GET", "/api", map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login" {
		t.Errorf("browser: %d %s", w.Code, w.Header().Get("Location"))
	}
	// Everything else without credentials gets 401.
	if w := do(h, "GET", "/api", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("api no creds: %d", w.Code)
	}
	if w := do(h, "POST", "/api", map[string]string{"Accept": "text/html"}); w.Code != http.StatusUnauthorized {
		t.Errorf("html POST: %d", w.Code)
	}
	// A native client's own Authorization passes through unchanged.
	w = do(h, "GET", "/api", map[string]string{"Authorization": "Bearer native"})
	if w.Body.String() != "Bearer native||" {
		t.Errorf("pass through: %q", w.Body.String())
	}
}

func TestGoogleAuthLogout(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	a := newTestAuth(f)
	h := authedHandler(a)
	w := do(h, "POST", "/auth/logout", nil, a.sessionCookie("john@brahy.com", time.Now()))
	c := cookieNamed(w, "gm_session")
	if c == nil || c.MaxAge >= 0 || c.Value != "" {
		t.Fatalf("session not cleared: %+v", c)
	}
	if w := do(h, "GET", "/auth/logout", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout: %d", w.Code)
	}
}

func TestGoogleAuthValidate(t *testing.T) {
	ok := GoogleAuth{ClientID: "a", ClientSecret: "b", SessionSecret: testSessKey, PublicURL: "https://x.example", BearerToken: "t"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	short := ok
	short.SessionSecret = "short"
	if err := short.Validate(); err == nil {
		t.Fatal("short session secret accepted")
	}
	none := ok
	none.ClientSecret = ""
	if err := none.Validate(); err == nil || strings.Contains(err.Error(), testSessKey) {
		t.Fatalf("bad error: %v", err)
	}
}

func TestNewMuxRegistersAuthRoutes(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	mux, err := NewMux(Deps{Run: func(ctx context.Context, task string) (string, error) { return "", nil }}, Options{Token: testBearer, Google: newTestAuth(f)})
	if err != nil {
		t.Fatal(err)
	}
	if w := do(mux, "GET", "/auth/login", nil); w.Code != http.StatusFound {
		t.Fatalf("login via NewMux: %d", w.Code)
	}
}

func TestGoogleAuthRefusesCrossOriginWrites(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	h := authedHandler(newTestAuth(f))
	state, sc := startLogin(t, h)
	sess := cookieNamed(callback(h, state, sc), "gm_session")
	cases := []struct {
		name, method string
		hdr          map[string]string
		want         int
	}{
		{"same origin post", "POST", map[string]string{"Origin": "https://gophermind.example"}, 200},
		{"no origin post (non-browser)", "POST", nil, 200},
		{"other origin post", "POST", map[string]string{"Origin": "https://evil.brahy.com"}, 403},
		{"sibling site fetch metadata", "POST", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"cross-site delete", "DELETE", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"other origin get is a read", "GET", map[string]string{"Origin": "https://evil.brahy.com"}, 200},
	}
	for _, c := range cases {
		if w := do(h, c.method, "/api", c.hdr, sess); w.Code != c.want {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.want)
		}
	}
}
