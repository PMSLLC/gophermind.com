package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Token is what's stored in the Keychain per backend, and what GetToken
// hands back the access token half of.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// TokenStore persists one Token blob (JSON-encoded by Manager) per backend
// name. keychainStore (keychain.go) is the real, macOS-Keychain-backed
// implementation; tests use an in-memory one (see manager_test.go) so they
// never touch the real Keychain (which can prompt for access the first
// time a process uses it -- not something an automated test should risk
// hanging on).
type TokenStore interface {
	Save(backend string, data []byte) error
	Load(backend string) ([]byte, error) // returns ErrNotFound if absent
	Delete(backend string) error
}

// refreshBefore is how long before expiry GetToken proactively refreshes,
// per the acceptance criteria ("refreshes 1 min before expiry, transparent
// to caller").
const refreshBefore = 1 * time.Minute

// Manager is gophermind-osx's OAuth2/Keychain client: one per app process,
// holding a Config per backend (set via Configure) and delegating storage
// to a TokenStore.
type Manager struct {
	mu          sync.Mutex
	store       TokenStore
	configs     map[string]Config
	openBrowser func(url string) error
	now         func() time.Time // injectable for refresh-timing tests
}

// NewManager returns a Manager backed by store. Pass keychainStore{} for
// real use; tests typically pass an in-memory TokenStore instead.
func NewManager(store TokenStore) *Manager {
	return &Manager{
		store:       store,
		configs:     make(map[string]Config),
		openBrowser: defaultOpenBrowser,
		now:         time.Now,
	}
}

// defaultOpenBrowser opens url in the system's default browser via macOS's
// `open` command.
func defaultOpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}

// Configure registers cfg as backend's identity provider, required before
// Login or a token refresh (GetToken past expiry) for that backend.
func (m *Manager) Configure(backend string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[backend] = cfg
}

// Login runs the full Authorization Code + PKCE flow for backend (see the
// package doc comment for the login UX) and stores the resulting token.
// Blocks until the user completes login in their browser, the IdP reports
// an error, or ctx is done.
func (m *Manager) Login(ctx context.Context, backend string) error {
	m.mu.Lock()
	cfg, ok := m.configs[backend]
	openBrowser := m.openBrowser
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("auth: no Config registered for backend %q (call Configure first)", backend)
	}

	redirectURL, results, shutdown, err := runLoopbackServer(ctx)
	if err != nil {
		return err
	}
	defer shutdown()

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}
	oc := cfg.oauth2Config(redirectURL)
	authURL := oc.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	if err := openBrowser(authURL); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	select {
	case res := <-results:
		if res.err != "" {
			return fmt.Errorf("login failed: %s", res.err)
		}
		if res.state != state {
			return fmt.Errorf("login failed: state mismatch (possible CSRF)")
		}
		tok, err := oc.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return fmt.Errorf("exchange code: %w", err)
		}
		return m.storeToken(backend, tok)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) storeToken(backend string, tok *oauth2.Token) error {
	t := Token{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry}
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("encode token: %w", err)
	}
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	return store.Save(backend, data)
}

func (m *Manager) loadToken(backend string) (Token, error) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	data, err := store.Load(backend)
	if err != nil {
		return Token{}, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return Token{}, fmt.Errorf("decode stored token: %w", err)
	}
	return t, nil
}

// GetToken returns a valid access token for backend -- from the Keychain
// directly if it's not within refreshBefore of expiring, otherwise after
// transparently refreshing it first (the refreshed token is also
// persisted, so the next call doesn't refresh again unnecessarily).
// Returns ErrNotFound if backend has never logged in (call Login first).
func (m *Manager) GetToken(ctx context.Context, backend string) (string, error) {
	tok, err := m.loadToken(backend)
	if err != nil {
		return "", err
	}
	if m.now().Add(refreshBefore).Before(tok.Expiry) {
		return tok.AccessToken, nil
	}

	m.mu.Lock()
	cfg, ok := m.configs[backend]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("auth: no Config registered for backend %q, cannot refresh", backend)
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("auth: token expired and no refresh token stored for %q; Login again", backend)
	}

	oc := cfg.oauth2Config("")
	src := oc.TokenSource(ctx, &oauth2.Token{RefreshToken: tok.RefreshToken})
	newTok, err := src.Token()
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}
	if err := m.storeToken(backend, newTok); err != nil {
		return "", err
	}
	return newTok.AccessToken, nil
}

// Logout clears backend's stored credentials. A no-op (not an error) if
// nothing was stored.
func (m *Manager) Logout(backend string) error {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	err := store.Delete(backend)
	if err == ErrNotFound {
		return nil
	}
	return err
}
