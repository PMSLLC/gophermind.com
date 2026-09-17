package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory TokenStore, used by every test in this file
// except TestKeychain_RealStoreRoundTrip: the real macOS Keychain can
// prompt for access the first time a given (unsigned, ad-hoc go test)
// binary touches it, which is not something an automated test suite
// should risk hanging on for logic that doesn't actually need real
// Keychain behavior to verify (refresh timing, login flow, etc. are pure
// Go logic once a TokenStore exists at all).
type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: make(map[string][]byte)} }

func (s *memStore) Save(backend string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), data...)
	s.data[backend] = cp
	return nil
}

func (s *memStore) Load(backend string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data[backend]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), d...), nil
}

func (s *memStore) Delete(backend string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, backend)
	return nil
}

// TestNewKeychainStore_ReturnsRealStore covers NewKeychainStore's whole
// job: handing back a usable TokenStore to callers outside this package
// (gophermind-osx's main package, for a configured backend's bearer
// token) without them needing to name the unexported keychainStore type
// themselves. Deliberately does not call Save/Load/Delete on it --
// keychainStore{}'s real behavior is already covered by
// TestKeychain_RealStoreRoundTrip, and touching the real Keychain here
// too would just be a second, redundant risk of the interactive-prompt
// hang that test's own doc comment warns about.
func TestNewKeychainStore_ReturnsRealStore(t *testing.T) {
	store := NewKeychainStore()
	if store == nil {
		t.Fatal("NewKeychainStore() = nil")
	}
	if _, ok := store.(keychainStore); !ok {
		t.Errorf("NewKeychainStore() returned %T, want keychainStore", store)
	}
}

// TestBackendTokenKey_DiffersFromBareName covers why BackendTokenKey
// exists at all: Manager's own Save/Load key a backend's OAuth Token blob
// by the bare backend name, so a bearer token stored under that same key
// would silently collide with (and could be overwritten by) that
// backend's OAuth tokens in the same TokenStore.
func TestBackendTokenKey_DiffersFromBareName(t *testing.T) {
	if got := BackendTokenKey("local"); got == "local" {
		t.Errorf("BackendTokenKey(%q) = %q, must differ from the bare backend name", "local", got)
	}
}

// TestBackendTokenKey_DistinctPerBackend covers the ordinary case: two
// different backends must not collide with each other either.
func TestBackendTokenKey_DistinctPerBackend(t *testing.T) {
	if BackendTokenKey("a") == BackendTokenKey("b") {
		t.Error("BackendTokenKey(\"a\") == BackendTokenKey(\"b\"), want distinct keys per backend")
	}
}

// TestKeychain_RealStoreRoundTrip covers "Keychain: tokens stored per
// backend, retrievable, deletable" against the REAL macOS Keychain (every
// other test in this file uses memStore -- see its doc comment for why).
// Uses t.Deadline-aware behavior implicitly via the surrounding `go test`
// timeout; if Keychain access ever prompts interactively in some CI
// environment, this test hanging (rather than every test in the package)
// is the intended blast radius.
func TestKeychain_RealStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Keychain access in -short mode")
	}
	store := keychainStore{}
	backend := fmt.Sprintf("gophermind-osx-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { store.Delete(backend) })

	if _, err := store.Load(backend); err != ErrNotFound {
		t.Fatalf("Load on a never-saved backend: err = %v, want ErrNotFound", err)
	}

	want := []byte(`{"access_token":"a","refresh_token":"r"}`)
	if err := store.Save(backend, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(backend)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Load = %q, want %q", got, want)
	}

	// Save again (update path, not the initial add path).
	want2 := []byte(`{"access_token":"b","refresh_token":"r2"}`)
	if err := store.Save(backend, want2); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	got2, err := store.Load(backend)
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	if string(got2) != string(want2) {
		t.Errorf("Load after update = %q, want %q", got2, want2)
	}

	if err := store.Delete(backend); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load(backend); err != ErrNotFound {
		t.Errorf("Load after Delete: err = %v, want ErrNotFound", err)
	}
}

// fakeIdP is a minimal Keycloak-shaped OAuth2 identity provider for
// testing the login/refresh flows without a real gocloak instance: an
// /authorize endpoint (unused directly -- Login hands its URL to
// openBrowser, which in tests skips straight to the redirect) and a
// /token endpoint implementing both the authorization_code and
// refresh_token grants, PKCE-verified.
type fakeIdP struct {
	srv          *httptest.Server
	mu           sync.Mutex
	issuedCodes  map[string]string // code -> verifier's expected S256 challenge
	refreshCount int
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	f := &fakeIdP{issuedCodes: make(map[string]string)}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	grant := r.FormValue("grant_type")
	w.Header().Set("Content-Type", "application/json")

	switch grant {
	case "authorization_code":
		code := r.FormValue("code")
		verifier := r.FormValue("code_verifier")
		f.mu.Lock()
		wantChallenge, ok := f.issuedCodes[code]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "invalid_grant", 400)
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		gotChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
		if gotChallenge != wantChallenge {
			http.Error(w, "invalid_grant: PKCE mismatch", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-1",
			"refresh_token": "refresh-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	case "refresh_token":
		if r.FormValue("refresh_token") == "" {
			http.Error(w, "invalid_grant", 400)
			return
		}
		f.mu.Lock()
		f.refreshCount++
		n := f.refreshCount
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("access-refreshed-%d", n),
			"refresh_token": fmt.Sprintf("refresh-refreshed-%d", n),
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	default:
		http.Error(w, "unsupported_grant_type", 400)
	}
}

func (f *fakeIdP) config() Config {
	return Config{ClientID: "gophermind-osx-test", AuthURL: f.srv.URL + "/authorize", TokenURL: f.srv.URL + "/token"}
}

// issueCode registers a code the fake /token endpoint will accept for the
// given PKCE verifier, returning the code.
func (f *fakeIdP) issueCode(verifier string) string {
	code := "code-" + verifier[:8]
	sum := sha256.Sum256([]byte(verifier))
	f.mu.Lock()
	f.issuedCodes[code] = base64.RawURLEncoding.EncodeToString(sum[:])
	f.mu.Unlock()
	return code
}

// TestManager_LoginStoresToken covers "OAuth2 flow: initiates login,
// receives token, stores in Keychain" (via memStore -- see its doc
// comment) end to end: Login's openBrowser is a fake that extracts state/
// redirect_uri/code_challenge from the real AuthCodeURL Login built,
// registers a matching code with the fake IdP, and hits the redirect --
// exactly what a real browser completing a real login would produce.
func TestManager_LoginStoresToken(t *testing.T) {
	idp := newFakeIdP(t)
	store := newMemStore()
	m := NewManager(store)
	m.Configure("backend-1", idp.config())

	m.openBrowser = func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("client_id") != "gophermind-osx-test" {
			t.Errorf("authURL client_id = %q", q.Get("client_id"))
		}
		challenge := q.Get("code_challenge")
		if challenge == "" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("authURL missing PKCE params: %s", authURL)
		}
		state := q.Get("state")
		redirect := q.Get("redirect_uri")

		// A real browser would have the user log in against AuthURL, then
		// follow the IdP's redirect to redirect_uri?code=...&state=...
		// Simulating that requires knowing which verifier produced
		// `challenge`, which Login doesn't expose -- so this issues a code
		// tied to `challenge` directly and relies on Login having used the
		// matching verifier when it calls /token, which the fake IdP's
		// PKCE check (comparing against the verifier actually sent) proves
		// independently of this shortcut.
		code := "code-fixed"
		idp.mu.Lock()
		idp.issuedCodes[code] = challenge
		idp.mu.Unlock()

		go func() {
			http.Get(redirect + "?code=" + code + "&state=" + state)
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Login(ctx, "backend-1"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	tok, err := m.loadToken("backend-1")
	if err != nil {
		t.Fatalf("loadToken after Login: %v", err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" {
		t.Errorf("stored token = %+v", tok)
	}
	if tok.Expiry.Before(time.Now().Add(59 * time.Minute)) {
		t.Errorf("Expiry = %v, want ~1h from now", tok.Expiry)
	}
}

// TestManager_LoginStateMismatchRejected covers the CSRF guard: a redirect
// with the wrong state must not be accepted as a successful login.
func TestManager_LoginStateMismatchRejected(t *testing.T) {
	idp := newFakeIdP(t)
	m := NewManager(newMemStore())
	m.Configure("backend-1", idp.config())

	m.openBrowser = func(authURL string) error {
		u, _ := url.Parse(authURL)
		redirect := u.Query().Get("redirect_uri")
		code := idp.issueCode("irrelevant-verifier-shortcut")
		go func() {
			http.Get(redirect + "?code=" + code + "&state=wrong-state")
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := m.Login(ctx, "backend-1")
	if err == nil {
		t.Fatal("expected an error for a state mismatch")
	}
}

// TestGetToken_ReturnsCachedTokenWhenFarFromExpiry covers "GetToken(backend)
// returns valid access token" for the common case: no refresh needed.
func TestGetToken_ReturnsCachedTokenWhenFarFromExpiry(t *testing.T) {
	store := newMemStore()
	m := NewManager(store)
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }

	tok := Token{AccessToken: "still-good", RefreshToken: "r", Expiry: fixedNow.Add(30 * time.Minute)}
	data, _ := json.Marshal(tok)
	store.Save("b", data)

	got, err := m.GetToken(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got != "still-good" {
		t.Errorf("GetToken = %q, want the cached token unchanged", got)
	}
}

// TestGetToken_RefreshesWithinOneMinuteOfExpiry covers "Token refresh:
// refreshes 1 min before expiry, transparent to caller" directly: a token
// expiring in 30 seconds must trigger a refresh, and the caller gets the
// new access token back without any extra step.
func TestGetToken_RefreshesWithinOneMinuteOfExpiry(t *testing.T) {
	idp := newFakeIdP(t)
	store := newMemStore()
	m := NewManager(store)
	m.Configure("b", idp.config())
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }

	tok := Token{AccessToken: "about-to-expire", RefreshToken: "refresh-me", Expiry: fixedNow.Add(30 * time.Second)}
	data, _ := json.Marshal(tok)
	store.Save("b", data)

	got, err := m.GetToken(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got == "about-to-expire" {
		t.Error("GetToken returned the stale token instead of refreshing")
	}
	if got != "access-refreshed-1" {
		t.Errorf("GetToken = %q, want the refreshed token", got)
	}

	// The refreshed token must also be persisted, so a second call this
	// close call doesn't refresh again.
	stored, err := m.loadToken("b")
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if stored.AccessToken != "access-refreshed-1" {
		t.Errorf("persisted token = %+v, want the refreshed one stored", stored)
	}
}

// TestGetToken_NoStoredTokenReturnsErrNotFound covers GetToken's behavior
// for a backend that has never logged in.
func TestGetToken_NoStoredTokenReturnsErrNotFound(t *testing.T) {
	m := NewManager(newMemStore())
	_, err := m.GetToken(context.Background(), "never-logged-in")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestGetToken_ExpiredWithNoRefreshTokenErrors covers the dead-end case:
// no refresh token means GetToken cannot recover on its own and must
// report a clear error rather than silently returning a stale token.
func TestGetToken_ExpiredWithNoRefreshTokenErrors(t *testing.T) {
	store := newMemStore()
	m := NewManager(store)
	m.Configure("b", Config{})
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }

	tok := Token{AccessToken: "stale", Expiry: fixedNow.Add(-1 * time.Hour)} // no RefreshToken
	data, _ := json.Marshal(tok)
	store.Save("b", data)

	_, err := m.GetToken(context.Background(), "b")
	if err == nil {
		t.Fatal("expected an error when the token is expired and there's no refresh token")
	}
}

// TestLogout_ClearsStoredToken covers "Logout: clears Keychain entry for
// backend".
func TestLogout_ClearsStoredToken(t *testing.T) {
	store := newMemStore()
	m := NewManager(store)
	store.Save("b", []byte(`{"access_token":"x"}`))

	if err := m.Logout("b"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := m.GetToken(context.Background(), "b"); err != ErrNotFound {
		t.Errorf("GetToken after Logout: err = %v, want ErrNotFound", err)
	}
}

// TestLogout_NeverLoggedInIsNotAnError covers Logout's idempotency.
func TestLogout_NeverLoggedInIsNotAnError(t *testing.T) {
	m := NewManager(newMemStore())
	if err := m.Logout("never-existed"); err != nil {
		t.Errorf("Logout on a never-existing backend: %v, want nil", err)
	}
}
