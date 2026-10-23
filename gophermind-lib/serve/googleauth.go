package serve

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sessionCookieName = "gm_session"
	stateCookieName   = "gm_state"
	sessionTTL        = 12 * time.Hour
	stateTTL          = 10 * time.Minute
	minSessionSecret  = 32

	defaultGoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleTokenURL = "https://oauth2.googleapis.com/token"
)

// GoogleAuth puts the server's bearer-token API behind a Google sign-in. It
// runs the OAuth authorization-code flow, keeps the result in an HMAC-signed
// session cookie, and (via Middleware) turns a valid session into the bearer
// token the existing handlers already require. The server token never reaches
// the browser.
type GoogleAuth struct {
	ClientID      string
	ClientSecret  string
	AllowedDomain string // email domain allowed to sign in; default brahy.com
	SessionSecret string // signs the state and session cookies; at least 32 bytes
	PublicURL     string // external base URL; the redirect URI is PublicURL/auth/callback
	BearerToken   string // server token injected for signed-in requests

	// Injectable for tests; empty means Google's real endpoints.
	AuthURL    string
	TokenURL   string
	Issuer     string // when set, the only accepted iss
	HTTPClient *http.Client
	Now        func() time.Time
}

// Validate reports which required settings are missing (names only, never
// values) or too weak.
func (a *GoogleAuth) Validate() error {
	var missing []string
	for _, f := range []struct{ name, val string }{
		{"GOPHERMIND_GOOGLE_CLIENT_ID", a.ClientID},
		{"GOPHERMIND_GOOGLE_CLIENT_SECRET", a.ClientSecret},
		{"GOPHERMIND_SESSION_SECRET", a.SessionSecret},
		{"GOPHERMIND_PUBLIC_URL", a.PublicURL},
		{"bearer token", a.BearerToken},
	} {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("google sign-in is missing %s", strings.Join(missing, ", "))
	}
	if len(a.SessionSecret) < minSessionSecret {
		return fmt.Errorf("GOPHERMIND_SESSION_SECRET must be at least %d bytes", minSessionSecret)
	}
	return nil
}

func (a *GoogleAuth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *GoogleAuth) domain() string {
	if a.AllowedDomain != "" {
		return strings.ToLower(a.AllowedDomain)
	}
	return "brahy.com"
}

func (a *GoogleAuth) redirectURI() string {
	return strings.TrimRight(a.PublicURL, "/") + "/auth/callback"
}

// Register adds the sign-in routes to mux.
func (a *GoogleAuth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /auth/logout", a.logout)
}

// sign returns base64url(payload) "." base64url(HMAC(kind "|" payload)). The
// kind is inside the MAC so a state cookie cannot pass as a session cookie.
func (a *GoogleAuth) sign(kind string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(a.SessionSecret))
	mac.Write([]byte(kind + "|"))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verify checks the MAC and returns the payload.
func (a *GoogleAuth) verify(kind, value string) ([]byte, bool) {
	p, s, ok := strings.Cut(value, ".")
	if !ok {
		return nil, false
	}
	payload, err1 := base64.RawURLEncoding.DecodeString(p)
	sig, err2 := base64.RawURLEncoding.DecodeString(s)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(a.SessionSecret))
	mac.Write([]byte(kind + "|"))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	return payload, true
}

type cookiePayload struct {
	State string `json:"s,omitempty"`
	Email string `json:"e,omitempty"`
	Exp   int64  `json:"x"`
}

func (a *GoogleAuth) cookie(name, kind string, p cookiePayload, ttl time.Duration) *http.Cookie {
	b, _ := json.Marshal(p)
	return &http.Cookie{
		Name: name, Value: a.sign(kind, b), Path: "/", MaxAge: int(ttl / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

func (a *GoogleAuth) stateCookie(state string, now time.Time) *http.Cookie {
	return a.cookie(stateCookieName, "state", cookiePayload{State: state, Exp: now.Add(stateTTL).Unix()}, stateTTL)
}

func (a *GoogleAuth) sessionCookie(email string, now time.Time) *http.Cookie {
	return a.cookie(sessionCookieName, "session", cookiePayload{Email: email, Exp: now.Add(sessionTTL).Unix()}, sessionTTL)
}

func (a *GoogleAuth) readCookie(r *http.Request, name, kind string) (cookiePayload, bool) {
	var p cookiePayload
	c, err := r.Cookie(name)
	if err != nil {
		return p, false
	}
	b, ok := a.verify(kind, c.Value)
	if !ok || json.Unmarshal(b, &p) != nil || a.now().Unix() >= p.Exp {
		return cookiePayload{}, false
	}
	return p, true
}

func (a *GoogleAuth) login(w http.ResponseWriter, r *http.Request) {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "sign-in unavailable", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	http.SetCookie(w, a.stateCookie(state, a.now()))
	authURL := a.AuthURL
	if authURL == "" {
		authURL = defaultGoogleAuthURL
	}
	q := url.Values{
		"client_id": {a.ClientID}, "redirect_uri": {a.redirectURI()}, "response_type": {"code"},
		"scope": {"openid email"}, "state": {state}, "hd": {a.domain()}, "prompt": {"select_account"},
	}
	http.Redirect(w, r, authURL+"?"+q.Encode(), http.StatusFound)
}

func (a *GoogleAuth) callback(w http.ResponseWriter, r *http.Request) {
	sp, ok := a.readCookie(r, stateCookieName, "state")
	got := r.URL.Query().Get("state")
	if !ok || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sp.State)) != 1 {
		http.Error(w, "invalid sign-in state", http.StatusBadRequest)
		return
	}
	// The state is single use: drop it whatever happens next.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "sign-in was not completed", http.StatusBadRequest)
		return
	}
	email, err := a.exchange(r, code)
	if err != nil {
		http.Error(w, "sign-in refused", http.StatusForbidden)
		return
	}
	http.SetCookie(w, a.sessionCookie(email, a.now()))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *GoogleAuth) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// exchange trades code for an ID token at Google's token endpoint and returns
// the verified email. The token arrives directly from Google over TLS, so its
// signature is not re-verified (OIDC Core 3.1.3.7); its claims are checked.
func (a *GoogleAuth) exchange(r *http.Request, code string) (string, error) {
	tokenURL := a.TokenURL
	if tokenURL == "" {
		tokenURL = defaultGoogleTokenURL
	}
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	form := url.Values{
		"code": {code}, "client_id": {a.ClientID}, "client_secret": {a.ClientSecret},
		"redirect_uri": {a.redirectURI()}, "grant_type": {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}
	var tr struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return "", err
	}
	parts := strings.Split(tr.IDToken, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed id_token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var c struct {
		Iss           string `json:"iss"`
		Aud           any    `json:"aud"`
		Exp           int64  `json:"exp"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", err
	}
	return a.checkClaims(c.Iss, c.Aud, c.Exp, c.Email, c.EmailVerified)
}

// checkClaims enforces iss, aud, exp, email_verified and the email domain.
// The hd claim is deliberately not consulted: it is a hint, not a guarantee.
func (a *GoogleAuth) checkClaims(iss string, aud any, exp int64, email string, verified any) (string, error) {
	issOK := iss == a.Issuer
	if a.Issuer == "" {
		issOK = iss == "https://accounts.google.com" || iss == "accounts.google.com"
	}
	if !issOK {
		return "", errors.New("bad issuer")
	}
	if !audMatches(aud, a.ClientID) {
		return "", errors.New("bad audience")
	}
	if a.now().Unix() >= exp {
		return "", errors.New("id_token expired")
	}
	if v, ok := verified.(bool); !(ok && v) {
		if s, ok := verified.(string); !(ok && s == "true") {
			return "", errors.New("email not verified")
		}
	}
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || strings.Contains(domain, "@") || domain != a.domain() {
		return "", errors.New("email domain not allowed")
	}
	return email, nil
}

func audMatches(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return subtle.ConstantTimeCompare([]byte(v), []byte(clientID)) == 1
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && subtle.ConstantTimeCompare([]byte(s), []byte(clientID)) == 1 {
				return true
			}
		}
	}
	return false
}

// Middleware wraps the whole mux. Public paths (/healthz, /readyz, /auth/*,
// /run) pass through untouched. Otherwise a valid session gets the server
// bearer token (and a body signature when an HMAC secret is set) so the
// handlers run unchanged; a request with no session but its own Authorization
// header passes through for the handlers to check (native clients); anything
// else is redirected to sign-in (browser navigation) or gets 401.
func (a *GoogleAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/healthz" || p == "/readyz" || p == "/run" || strings.HasPrefix(p, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.readCookie(r, sessionCookieName, "session"); ok {
			r.Header.Set("Authorization", "Bearer "+a.BearerToken)
			r.Header.Del("X-Hub-Signature-256")
			if secret := serveHMACSecret(); secret != "" {
				body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if err != nil {
					http.Error(w, "read body", http.StatusBadRequest)
					return
				}
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write(body)
				r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
