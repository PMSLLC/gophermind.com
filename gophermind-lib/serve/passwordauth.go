package serve

// TEMPORARY password login. Remove when Google sign-in is live.
//
// This is a stopgap until a Google OAuth client exists. It issues the same
// gm_session cookie the Google flow does, so Middleware, /auth/me, the CSRF
// guard, the pages and logout need no change. To remove it, delete exactly:
//
//  1. this file (gophermind-lib/serve/passwordauth.go)
//  2. its test (gophermind-lib/serve/passwordauth_test.go)
//  3. the login asset (gophermind-lib/serve/assets/login.html)
//  4. the env wiring in gophermind-server: gophermind-server/passwordauth.go,
//     its test, the localEnvSet/addLocalLogin calls in main.go
//     (parseServerConfig and googleAuthFromEnv), and the hashpw dispatch
//     in main()
//  5. the hashpw subcommand (gophermind-server/passwordauth.go, above)
//  6. the three hooks in googleauth.go: the Local field, the Local check at the
//     top of login(), and the Local route in Register()
//
// Docs to drop afterwards: the "Temporary password login" sections of
// docs/SERVER-SETUP.md and docs/design/web-ui.md, and the CHANGELOG bullet.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pbkdf2Scheme     = "pbkdf2-sha256"
	pbkdf2Iterations = 600000
	pbkdf2MaxIter    = 10000000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16

	maxLoginFailures  = 5
	loginWindow       = 15 * time.Minute
	maxTrackedClients = 4096
	maxLoginFormBytes = 4096
)

//go:embed assets/login.html
var loginHTML []byte

// LocalLogin is a single username and a PBKDF2 hash of its password.
type LocalLogin struct {
	User         string
	PasswordHash string // pbkdf2-sha256$<iterations>$<base64 salt>$<base64 key>

	limiter loginLimiter
}

// HashPassword returns a hash line for password in the format LocalLogin
// expects.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{pbkdf2Scheme, strconv.Itoa(pbkdf2Iterations),
		base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(key)}, "$"), nil
}

// parsePasswordHash splits and checks a hash line. Anything off is an error.
func parsePasswordHash(stored string) (iter int, salt, key []byte, err error) {
	bad := errors.New("not a valid " + pbkdf2Scheme + " hash")
	p := strings.Split(stored, "$")
	if len(p) != 4 || p[0] != pbkdf2Scheme {
		return 0, nil, nil, bad
	}
	iter, e := strconv.Atoi(p[1])
	if e != nil || iter < pbkdf2Iterations || iter > pbkdf2MaxIter {
		return 0, nil, nil, bad
	}
	salt, e1 := base64.StdEncoding.DecodeString(p[2])
	key, e2 := base64.StdEncoding.DecodeString(p[3])
	if e1 != nil || e2 != nil || len(salt) < 8 || len(key) < 16 || len(key) > 64 {
		return 0, nil, nil, bad
	}
	return iter, salt, key, nil
}

// ValidatePasswordHash reports whether stored is a usable hash line. It never
// includes the value in the error.
func ValidatePasswordHash(stored string) error {
	_, _, _, err := parsePasswordHash(stored)
	return err
}

// CheckPassword compares password with a stored hash line in constant time. A
// malformed hash never matches.
func CheckPassword(stored, password string) bool {
	iter, salt, key, err := parsePasswordHash(stored)
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(key))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, key) == 1
}

// ValidateLocal reports which settings the local login still needs (names
// only, never values).
func (a *GoogleAuth) ValidateLocal() error {
	var missing []string
	for _, f := range []struct{ name, val string }{
		{"GOPHERMIND_WEB_USER", a.Local.User},
		{"GOPHERMIND_WEB_PASSWORD_HASH", a.Local.PasswordHash},
		{"GOPHERMIND_SESSION_SECRET", a.SessionSecret},
		{"GOPHERMIND_PUBLIC_URL", a.PublicURL},
		{"bearer token", a.BearerToken},
	} {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return errors.New("local login is missing " + strings.Join(missing, ", "))
	}
	if len(a.SessionSecret) < minSessionSecret {
		return errors.New("GOPHERMIND_SESSION_SECRET must be at least " + strconv.Itoa(minSessionSecret) + " bytes")
	}
	if err := ValidatePasswordHash(a.Local.PasswordHash); err != nil {
		return errors.New("GOPHERMIND_WEB_PASSWORD_HASH is not a valid hash (make one with: gophermind-server hashpw)")
	}
	return nil
}

// serveLoginForm answers GET /auth/login with the password form.
func (l *LocalLogin) serveLoginForm(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(loginHTML)
}

// passwordLogin answers POST /auth/password.
func (a *GoogleAuth) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOriginWrite(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	l := a.Local
	ip := clientIP(r)
	now := a.now()
	if l.limiter.blocked(ip, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	_ = r.ParseForm()
	user, pass := r.PostForm.Get("username"), r.PostForm.Get("password")
	// Both checks always run, whichever field is wrong.
	passOK := CheckPassword(l.PasswordHash, pass)
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(l.User)) == 1
	if !(passOK && userOK && user != "") {
		l.limiter.fail(ip, now)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	l.limiter.clear(ip)
	http.SetCookie(w, a.sessionCookie(user, now))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// clientIP is the connection's remote address. Only when that is loopback (the
// reverse proxy on this host) is X-Forwarded-For consulted, and then only its
// last entry, the one the proxy itself appended.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if vals := r.Header.Values("X-Forwarded-For"); len(vals) > 0 {
			parts := strings.Split(vals[len(vals)-1], ",")
			last := strings.TrimSpace(parts[len(parts)-1])
			if fwd := net.ParseIP(last); fwd != nil {
				return fwd.String()
			}
		}
	}
	return host
}

// loginLimiter counts failures per client in a fixed window that starts at the
// first failure. State is in memory only.
type loginLimiter struct {
	mu        sync.Mutex
	m         map[string]*loginAttempts
	lastSweep time.Time
}

type loginAttempts struct {
	count int
	start time.Time
}

// sweep drops expired entries; callers hold mu. It runs at most once a minute
// unless the map is full.
func (l *loginLimiter) sweep(now time.Time) {
	if len(l.m) < maxTrackedClients && now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, e := range l.m {
		if now.Sub(e.start) >= loginWindow {
			delete(l.m, k)
		}
	}
}

// blocked reports whether ip has used up its failures in the current window.
// When the table is full of live entries an unknown client is refused too.
func (l *loginLimiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	e, ok := l.m[ip]
	if !ok {
		return len(l.m) >= maxTrackedClients
	}
	return now.Sub(e.start) < loginWindow && e.count >= maxLoginFailures
}

func (l *loginLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	if l.m == nil {
		l.m = map[string]*loginAttempts{}
	}
	e, ok := l.m[ip]
	switch {
	case ok && now.Sub(e.start) < loginWindow:
		e.count++
	case ok || len(l.m) < maxTrackedClients:
		l.m[ip] = &loginAttempts{count: 1, start: now}
	}
}

func (l *loginLimiter) clear(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

func (l *loginLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}
