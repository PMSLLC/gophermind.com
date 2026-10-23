package main

// TEMPORARY password login wiring and the hashpw subcommand. Remove with
// gophermind-lib/serve/passwordauth.go when Google sign-in is live (the full
// delete list is in that file's header).

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gophermind/gophermind-lib/serve"
)

// maxHashpwInput bounds what hashpw reads from stdin.
const maxHashpwInput = 1024

// localEnvSet reports whether either local-login variable is set.
func localEnvSet(getenv func(string) string) bool {
	return getenv("GOPHERMIND_WEB_USER") != "" || getenv("GOPHERMIND_WEB_PASSWORD_HASH") != ""
}

// addLocalLogin turns on the local login from GOPHERMIND_WEB_USER and
// GOPHERMIND_WEB_PASSWORD_HASH (plus the shared session secret, public URL and
// bearer token). Neither variable set returns g unchanged. When g is nil
// (Google off) a local-only auth is built. A partial set is an error that
// names variables, never values.
func addLocalLogin(getenv func(string) string, g *serve.GoogleAuth, token string) (*serve.GoogleAuth, error) {
	if !localEnvSet(getenv) {
		return g, nil
	}
	if g == nil {
		g = &serve.GoogleAuth{
			SessionSecret: getenv("GOPHERMIND_SESSION_SECRET"),
			PublicURL:     getenv("GOPHERMIND_PUBLIC_URL"),
			BearerToken:   token,
		}
	}
	g.Local = &serve.LocalLogin{
		User:         getenv("GOPHERMIND_WEB_USER"),
		PasswordHash: getenv("GOPHERMIND_WEB_PASSWORD_HASH"),
	}
	if err := g.ValidateLocal(); err != nil {
		return nil, err
	}
	return g, nil
}

// runHashpw reads a password from in (one trailing newline is dropped) and
// writes only its hash line to out. The password is never echoed or logged.
func runHashpw(in io.Reader, out, errOut io.Writer) error {
	b, err := io.ReadAll(io.LimitReader(in, maxHashpwInput+1))
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	if len(b) > maxHashpwInput {
		return fmt.Errorf("password is longer than %d bytes", maxHashpwInput)
	}
	pw := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if pw == "" {
		return errors.New("no password on stdin")
	}
	h, err := serve.HashPassword(pw)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, h)
	return err
}
