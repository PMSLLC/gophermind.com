package serve

import (
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/session"
)

// sessionModelPath returns the path of the plaintext sidecar file that stores
// id's chosen model, next to its session history (<id>.jsonl -> <id>.model).
func sessionModelPath(id string) (string, error) {
	p, err := session.Path(id)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, ".jsonl") + ".model", nil
}

// writeSessionModel records id's chosen model in its sidecar file. An empty
// model removes the sidecar (best-effort) so the session falls back to the
// server default model.
func writeSessionModel(id, model string) error {
	p, err := sessionModelPath(id)
	if err != nil {
		return err
	}
	if model == "" {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(model), 0o600)
}

// writeSessionBackend records both the provider profile and the model a
// session is pinned to.
//
// The profile is stored because a model id is only meaningful at its own
// endpoint. Pinning a Kilo Code model while the active client points at
// OVHcloud and swapping only the model string sends that id to OVHcloud,
// which does not have it. The turn needs the profile to build a client for
// the right endpoint.
//
// The format stays the plain-text sidecar it always was. One line is a bare
// model, which is what earlier versions wrote and still means "this model on
// whatever endpoint is active". Two lines are profile then model.
func writeSessionBackend(id, profile, model string) error {
	if strings.TrimSpace(model) == "" {
		return writeSessionModel(id, "")
	}
	if strings.TrimSpace(profile) == "" {
		return writeSessionModel(id, model)
	}
	p, err := sessionModelPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(profile+"\n"+model), 0o600)
}

// ReadSessionBackend returns a session's pinned profile and model. An empty
// profile means "whatever endpoint is already active", which is both the
// no-profile case and what a sidecar written before profiles were recorded
// meant.
func ReadSessionBackend(id string) (profile, model string) {
	p, err := sessionModelPath(id)
	if err != nil {
		return "", ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) >= 2 {
		return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	}
	return "", strings.TrimSpace(lines[0])
}

// ReadSessionModel returns id's stored model, or "" if none is set or the
// sidecar can't be read.
func ReadSessionModel(id string) string {
	p, err := sessionModelPath(id)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	// Return only the model, so callers that predate profiles are unaffected
	// by the two-line form.
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
