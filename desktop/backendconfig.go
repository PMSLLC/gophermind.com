package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// backendConfigFile is the file, inside the gophermind config directory, that
// lists remote gophermind servers this desktop can run sessions against.
const backendConfigFile = "backends.json"

// backendEntry is one entry in that file.
//
// The token is named by path and never written here. A config file is the
// thing most likely to be copied, diffed, pasted into a bug report or picked
// up by a backup, and a token that authorizes shell execution on another
// machine should not be in any of those. This mirrors gocloak's SecretRef,
// for the same reason and with the same permission check.
type backendEntry struct {
	Name      string      `json:"name"`
	Kind      BackendKind `json:"kind"`
	BaseURL   string      `json:"base_url"`
	TokenFile string      `json:"token_file"`
}

// loadBackendConfig reads the backend list at path and resolves each entry's
// token from disk.
//
// A missing file yields no backends and no error: remote backends are opt-in,
// and the desktop is fully usable with only its embedded server.
//
// Failures are split by what can be salvaged. A file that cannot be parsed,
// or an entry whose name is unusable, is fatal: nothing can be known about
// what was intended, and a name that would route somewhere other than where
// it reads has no safe interpretation. Everything else is per-entry and is
// returned as an unavailable backend carrying a reason, so one bad entry
// cannot stop the app launching and cannot silently vanish either.
func loadBackendConfig(path string) ([]Backend, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backend config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}

	var entries []backendEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse backend config %s: %w", path, err)
	}

	out := make([]Backend, 0, len(entries))
	seen := map[string]bool{}
	for i, e := range entries {
		// A name that is not usable as a path segment is still fatal: there
		// is no safe way to keep an entry whose name would route somewhere
		// other than where it reads.
		if err := validBackendName(e.Name); err != nil {
			return nil, fmt.Errorf("backend %d: %w", i, err)
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("backend %q appears twice", e.Name)
		}
		seen[e.Name] = true

		kind := e.Kind
		if kind == "" {
			kind = BackendURL
		}

		// Everything below is per-entry and recoverable: the entry is kept
		// and marked, so the app still launches and the UI can explain it.
		unavailable := func(reason string) Backend {
			return Backend{Name: e.Name, Kind: kind, Reason: reason}
		}
		if kind != BackendURL {
			out = append(out, unavailable(fmt.Sprintf("kind %q is not supported yet", kind)))
			continue
		}
		if err := validBackendURL(e.BaseURL); err != nil {
			out = append(out, unavailable(err.Error()))
			continue
		}
		token, err := readTokenFile(e.TokenFile)
		if err != nil {
			out = append(out, unavailable(err.Error()))
			continue
		}
		out = append(out, Backend{
			Name:      e.Name,
			Kind:      kind,
			BaseURL:   strings.TrimRight(e.BaseURL, "/"),
			Token:     token,
			Available: true,
		})
	}
	return out, nil
}

// validBackendName rejects anything that would not survive being a path
// segment. The name appears in /b/<name>/..., so a name containing a slash or
// a traversal sequence would route somewhere other than where it reads, and
// "local" is reserved for the embedded server: two backends answering to one
// name would make it ambiguous which machine a command ran on.
func validBackendName(name string) error {
	if name == "" {
		return errors.New("needs a name")
	}
	if name == "local" {
		return errors.New(`the name "local" is reserved for the embedded server`)
	}
	if name != url.PathEscape(name) {
		return fmt.Errorf("name %q is not usable as a path segment", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is not usable as a path segment", name)
	}
	return nil
}

// validBackendURL requires an absolute http or https URL with a host. The
// router proxies to this as written, so a file:// or a bare "host:port" would
// fail at first use rather than at load, which is the wrong time to find out.
func validBackendURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("needs a base_url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("base_url is not a URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base_url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("base_url has no host")
	}
	return nil
}

// readTokenFile reads a bearer token from disk, refusing a file any other
// user on the machine can read.
//
// This token authorizes shell execution on another machine. A world-readable
// copy of it is worth as much to a local attacker as a shell on that machine,
// so the check is a refusal rather than a warning. The token never appears in
// an error message: an error is the most likely thing to be pasted somewhere
// public.
func readTokenFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("needs a token_file")
	}
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return "", fmt.Errorf("token_file: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return "", fmt.Errorf("token_file %s has permission %#o; it must not be readable "+
			"by other users, run: chmod 600 %s", expanded, mode, expanded)
	}
	raw, err := os.ReadFile(expanded)
	if err != nil {
		return "", fmt.Errorf("token_file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("token_file %s is empty", expanded)
	}
	return token, nil
}

// expandHome resolves a leading "~/" so a config file can name a path the
// user would type.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
