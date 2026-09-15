// Package auth implements gophermind-osx's gocloak (Keycloak) OAuth2
// client (.planning/tasks/03-04.json): Authorization Code flow with PKCE,
// tokens stored in the macOS Keychain per backend, automatic refresh, and
// logout.
//
// Login UX: not an embedded in-app consent screen. libui-ng (this app's
// GUI toolkit, see gophermind-osx/README.md) has no web view control, so a
// real OAuth2 Authorization Code flow's consent screen can't be rendered
// inside the app window -- confirmed by checking ui.h/ui_darwin.h before
// building this, not assumed. This uses the standard desktop-app pattern
// instead: open the system's default browser to the identity provider's
// login page, and catch the redirect on a local loopback HTTP server (the
// same approach the GitHub CLI, AWS CLI, and Docker Desktop use for
// exactly this reason).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// Config configures OAuth2 login against one backend's identity provider
// (a gocloak/Keycloak realm). Distinct backends can point at different
// realms/clients, so a Manager holds one Config per backend name (see
// Manager.Configure).
type Config struct {
	ClientID string
	AuthURL  string // e.g. "https://idp.example.com/realms/gophermind/protocol/openid-connect/auth"
	TokenURL string // e.g. "https://idp.example.com/realms/gophermind/protocol/openid-connect/token"
	Scopes   []string
}

func (c Config) oauth2Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    c.ClientID,
		Scopes:      c.Scopes,
		RedirectURL: redirectURL,
		Endpoint:    oauth2.Endpoint{AuthURL: c.AuthURL, TokenURL: c.TokenURL},
	}
}

// randomState returns a 32-byte (64 hex char) random OAuth2 state
// parameter, guarding the redirect against CSRF (a malicious page
// completing a login the user never initiated).
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// callbackResult is what the loopback redirect handler captures from the
// identity provider's response.
type callbackResult struct {
	code  string
	state string
	err   string // the IdP's own error= param, e.g. "access_denied"
}

// runLoopbackServer starts an HTTP server on an OS-assigned localhost port,
// serving exactly one request at /callback, and returns the assigned
// redirect URL plus a channel that receives that one request's result (or
// is closed without a value if the server is shut down first, e.g. via ctx
// cancellation). The server shuts itself down immediately after serving
// the one request it's here for.
func runLoopbackServer(ctx context.Context) (redirectURL string, results <-chan callbackResult, shutdown func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, fmt.Errorf("listen for OAuth2 redirect: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	ch := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		select {
		case ch <- callbackResult{code: q.Get("code"), state: q.Get("state"), err: q.Get("error")}:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>Login complete. You can close this window and return to gophermind.</body></html>")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	var shutdownOnce bool
	shutdown = func() {
		if shutdownOnce {
			return
		}
		shutdownOnce = true
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	go func() {
		<-ctx.Done()
		shutdown()
	}()

	return fmt.Sprintf("http://127.0.0.1:%d/callback", port), ch, shutdown, nil
}
