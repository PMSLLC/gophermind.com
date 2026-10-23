package serve

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// TEMPORARY: tests for passwordauth.go; delete with it.

const (
	localUser = "admin"
	localPass = "correct horse battery staple"
)

var (
	hashOnce   sync.Once
	cachedHash string
)

func testHash(t *testing.T) string {
	t.Helper()
	hashOnce.Do(func() {
		h, err := HashPassword(localPass)
		if err != nil {
			panic(err)
		}
		cachedHash = h
	})
	return cachedHash
}

// newLocalAuth is a local-only auth (no Google client) with an injectable clock.
func newLocalAuth(t *testing.T, now *time.Time) *GoogleAuth {
	t.Helper()
	return &GoogleAuth{
		SessionSecret: testSessKey, PublicURL: "https://gophermind.example", BearerToken: testBearer,
		Now:   func() time.Time { return *now },
		Local: &LocalLogin{User: localUser, PasswordHash: testHash(t)},
	}
}

func postLogin(h http.Handler, user, pass, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	form := url.Values{"username": {user}, "password": {pass}}
	r := httptest.NewRequest("POST", "/auth/password", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if remote != "" {
		r.RemoteAddr = remote
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLocalLoginSuccessIssuesSessionCookie(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	w := postLogin(h, localUser, localPass, "203.0.113.5:1234", map[string]string{"Origin": "https://gophermind.example"})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("login: %d %q", w.Code, w.Header().Get("Location"))
	}
	sess := cookieNamed(w, "gm_session")
	if sess == nil || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode || sess.MaxAge != 12*3600 {
		t.Fatalf("bad session cookie: %+v", sess)
	}
	if got := do(h, "GET", "/api", map[string]string{"Authorization": "Bearer spoofed"}, sess).Body.String(); got != "Bearer "+testBearer+"||" {
		t.Fatalf("middleware did not accept the cookie: %q", got)
	}
	me := do(h, "GET", "/auth/me", nil, sess)
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"email":"admin"`) {
		t.Fatalf("/auth/me: %d %s", me.Code, me.Body.String())
	}
	if out := do(h, "POST", "/auth/logout", nil, sess); cookieNamed(out, "gm_session").MaxAge >= 0 {
		t.Fatal("logout did not clear the cookie")
	}
}

func TestLocalLoginFailuresAreGeneric(t *testing.T) {
	now := time.Now()
	a := newLocalAuth(t, &now)
	h := authedHandler(a)
	cases := []struct{ name, user, pass string }{
		{"wrong password", localUser, "nope"},
		{"wrong user", "root", localPass},
		{"wrong both", "root", "nope"},
		{"empty both", "", ""},
		{"empty password", localUser, ""},
		{"empty user", "", localPass},
	}
	var first string
	for i, c := range cases {
		// A distinct client per case so the limiter does not interfere.
		w := postLogin(h, c.user, c.pass, fmt.Sprintf("198.51.100.%d:1", i+1), nil)
		if w.Code != http.StatusUnauthorized || cookieNamed(w, "gm_session") != nil {
			t.Errorf("%s: %d", c.name, w.Code)
		}
		body := w.Body.String()
		if i == 0 {
			first = body
		} else if body != first {
			t.Errorf("%s: body %q differs from %q", c.name, body, first)
		}
		for _, secret := range []string{localPass, a.Local.PasswordHash, testSessKey, testBearer} {
			if secret != "" && strings.Contains(body, secret) {
				t.Errorf("%s: body leaks a secret", c.name)
			}
		}
	}
}

func TestLocalLoginMalformedHashFailsClosed(t *testing.T) {
	good := testHash(t)
	parts := strings.Split(good, "$")
	bad := []string{
		"", "garbage", "pbkdf2-sha256", "pbkdf2-sha256$600000$a$b",
		"md5$600000$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$1$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$abc$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$600000$!!!$" + parts[3],
		"pbkdf2-sha256$600000$" + parts[2] + "$!!!",
		"pbkdf2-sha256$600000$$" + parts[3],
		"pbkdf2-sha256$600000$" + parts[2] + "$",
		"pbkdf2-sha256$999999999999$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$-5$" + parts[2] + "$" + parts[3],
		good + "$extra",
	}
	now := time.Now()
	for i, b := range bad {
		if CheckPassword(b, localPass) {
			t.Errorf("CheckPassword accepted %q", b)
		}
		if ValidatePasswordHash(b) == nil {
			t.Errorf("ValidatePasswordHash accepted %q", b)
		}
		a := newLocalAuth(t, &now)
		a.Local.PasswordHash = b
		w := postLogin(authedHandler(a), localUser, localPass, fmt.Sprintf("198.51.100.%d:1", i+1), nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("handler with hash %q: %d", b, w.Code)
		}
	}
	if err := ValidatePasswordHash(good); err != nil {
		t.Errorf("good hash rejected: %v", err)
	}
	// Weak but well-formed iteration counts are refused.
	weak := "pbkdf2-sha256$599999$" + parts[2] + "$" + parts[3]
	if CheckPassword(weak, localPass) {
		t.Error("accepted fewer than the minimum iterations")
	}
}

func TestLocalLoginLockoutAndWindowExpiry(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	ip := "203.0.113.9:5555"
	for i := 0; i < 5; i++ {
		if w := postLogin(h, localUser, "wrong", ip, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, w.Code)
		}
	}
	// Locked: even the right password is refused for the rest of the window.
	if w := postLogin(h, localUser, localPass, ip, nil); w.Code != http.StatusTooManyRequests || cookieNamed(w, "gm_session") != nil {
		t.Fatalf("6th attempt: %d", w.Code)
	}
	now = now.Add(14 * time.Minute)
	if w := postLogin(h, localUser, localPass, ip, nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("inside window: %d", w.Code)
	}
	// Another client is unaffected.
	if w := postLogin(h, localUser, localPass, "203.0.113.10:1", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("other ip: %d", w.Code)
	}
	now = now.Add(2 * time.Minute) // 16 minutes after the first failure
	if w := postLogin(h, localUser, localPass, ip, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("after window: %d", w.Code)
	}
}

func TestLocalLoginSuccessClearsCount(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	ip := "203.0.113.20:1"
	for round := 0; round < 2; round++ {
		for i := 0; i < 4; i++ {
			if w := postLogin(h, localUser, "wrong", ip, nil); w.Code != http.StatusUnauthorized {
				t.Fatalf("round %d failure %d: %d", round, i, w.Code)
			}
		}
		if w := postLogin(h, localUser, localPass, ip, nil); w.Code != http.StatusSeeOther {
			t.Fatalf("round %d success: %d", round, w.Code)
		}
	}
}

func TestLocalLoginClientIP(t *testing.T) {
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct", "203.0.113.7:99", nil, "203.0.113.7"},
		{"direct ignores xff", "203.0.113.7:99", []string{"9.9.9.9"}, "203.0.113.7"},
		{"loopback uses last hop", "127.0.0.1:99", []string{"1.1.1.1, 2.2.2.2, 198.51.100.4"}, "198.51.100.4"},
		{"loopback v6", "[::1]:99", []string{"198.51.100.4"}, "198.51.100.4"},
		{"loopback last header line", "127.0.0.1:99", []string{"1.1.1.1", "198.51.100.5"}, "198.51.100.5"},
		{"loopback no xff", "127.0.0.1:99", nil, "127.0.0.1"},
		{"loopback junk xff", "127.0.0.1:99", []string{"not-an-ip"}, "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/auth/password", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestLocalLoginForwardedForCannotEvadeLockout(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	// Direct client rotates X-Forwarded-For: still one bucket.
	for i := 0; i < 5; i++ {
		postLogin(h, localUser, "wrong", "203.0.113.30:1", map[string]string{"X-Forwarded-For": fmt.Sprintf("10.0.0.%d", i)})
	}
	if w := postLogin(h, localUser, localPass, "203.0.113.30:1", map[string]string{"X-Forwarded-For": "10.0.0.99"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("direct client escaped via XFF: %d", w.Code)
	}
	// Behind the loopback proxy, buckets follow the last hop.
	for i := 0; i < 5; i++ {
		postLogin(h, localUser, "wrong", "127.0.0.1:1", map[string]string{"X-Forwarded-For": "198.51.100.40"})
	}
	if w := postLogin(h, localUser, localPass, "127.0.0.1:1", map[string]string{"X-Forwarded-For": "198.51.100.40"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("proxied client not locked: %d", w.Code)
	}
	if w := postLogin(h, localUser, localPass, "127.0.0.1:1", map[string]string{"X-Forwarded-For": "198.51.100.41"}); w.Code != http.StatusSeeOther {
		t.Fatalf("other proxied client: %d", w.Code)
	}
}

func TestLocalLoginRefusesCrossOriginPost(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	for i, hdr := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
	} {
		w := postLogin(h, localUser, localPass, fmt.Sprintf("198.51.100.%d:1", i+1), hdr)
		if w.Code != http.StatusForbidden || cookieNamed(w, "gm_session") != nil {
			t.Errorf("%v: %d", hdr, w.Code)
		}
	}
	ok := postLogin(h, localUser, localPass, "198.51.100.50:1", map[string]string{"Origin": "https://gophermind.example", "Sec-Fetch-Site": "same-origin"})
	if ok.Code != http.StatusSeeOther {
		t.Errorf("same-origin: %d", ok.Code)
	}
}

func TestLocalLoginFormAndGoogleRedirect(t *testing.T) {
	now := time.Now()
	h := authedHandler(newLocalAuth(t, &now))
	w := do(h, "GET", "/auth/login", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("form: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, want := range []string{`method="post"`, `action="/auth/password"`, `name="username"`, `name="password"`, `Content-Security-Policy`, `form-action 'self'`} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %q", want)
		}
	}
	for _, bad := range []string{"http://", "https://", "<script", " onclick", " onsubmit", "src="} {
		if strings.Contains(body, bad) {
			t.Errorf("form contains %q", bad)
		}
	}

	// With Google also configured, /auth/login still goes to Google unless ?local=1.
	g := newTestAuth(newFakeGoogle(t, goodClaims()))
	g.Local = &LocalLogin{User: localUser, PasswordHash: testHash(t)}
	gh := authedHandler(g)
	if w := do(gh, "GET", "/auth/login", nil); w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "https://accounts.example/authorize") {
		t.Fatalf("google redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := do(gh, "GET", "/auth/login?local=1", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "/auth/password") {
		t.Fatalf("?local=1: %d", w.Code)
	}
}

func TestLocalLoginOffMeansNoRoute(t *testing.T) {
	f := newFakeGoogle(t, goodClaims())
	h := authedHandler(newTestAuth(f))
	if w := postLogin(h, localUser, localPass, "", nil); w.Code == http.StatusSeeOther {
		t.Fatalf("password route active without Local: %d", w.Code)
	}
}

func TestLocalLoginNothingSecretIsLogged(t *testing.T) {
	var buf bytes.Buffer
	oldLog, oldSlog := log.Writer(), slog.Default()
	log.SetOutput(&buf)
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer func() { log.SetOutput(oldLog); slog.SetDefault(oldSlog) }()

	now := time.Now()
	a := newLocalAuth(t, &now)
	h := authedHandler(a)
	postLogin(h, localUser, "wrong-password-xyz", "203.0.113.60:1", nil)
	postLogin(h, localUser, localPass, "203.0.113.61:1", nil)
	for _, s := range []string{"wrong-password-xyz", localPass, a.Local.PasswordHash, testSessKey, testBearer} {
		if strings.Contains(buf.String(), s) {
			t.Fatalf("log contains a secret: %q", buf.String())
		}
	}
}

func TestLoginLimiterIsBounded(t *testing.T) {
	now := time.Now()
	l := &loginLimiter{}
	for i := 0; i < maxTrackedClients*2; i++ {
		l.fail(fmt.Sprintf("ip-%d", i), now)
	}
	if n := l.size(); n > maxTrackedClients {
		t.Fatalf("map grew to %d", n)
	}
	// Expired entries are swept on later use.
	later := now.Add(loginWindow + time.Second)
	l.fail("fresh", later)
	if n := l.size(); n != 1 {
		t.Fatalf("after expiry the map holds %d entries, want 1", n)
	}
}

func TestPasswordHashFormatAndRoundTrip(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Split(h, "$")
	if len(p) != 4 || p[0] != "pbkdf2-sha256" || p[1] != "600000" {
		t.Fatalf("format: %q", h)
	}
	if _, err := base64.StdEncoding.DecodeString(p[2]); err != nil {
		t.Fatalf("salt not base64: %v", err)
	}
	if !CheckPassword(h, "s3cret") || CheckPassword(h, "S3cret") || CheckPassword(h, "") {
		t.Fatal("round trip")
	}
	h2, _ := HashPassword("s3cret")
	if h == h2 {
		t.Fatal("salt is not random")
	}
	if strings.Contains(h, "s3cret") {
		t.Fatal("hash contains the password")
	}
	if _, err := HashPassword(""); err == nil {
		t.Fatal("empty password hashed")
	}
}
