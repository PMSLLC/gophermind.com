package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func pinDir(t *testing.T) {
	t.Helper()
	t.Setenv("GOPHERMIND_SESSION_DIR", t.TempDir())
}

// Pinning has to record WHICH PROVIDER the model belongs to, not just its name.
// A model id is only meaningful at its own endpoint: pinning a Kilo Code model
// while the client is on OVHcloud and merely swapping the model string POSTs
// that id to OVHcloud, which 404s. The session turn needs the profile to build
// a client for the right endpoint.
func TestSessionPinStoresProfileAndModel(t *testing.T) {
	pinDir(t)
	if err := writeSessionBackend("s1", "free-kilocode", "nvidia/nemotron:free"); err != nil {
		t.Fatal(err)
	}
	profile, model := ReadSessionBackend("s1")
	if profile != "free-kilocode" || model != "nvidia/nemotron:free" {
		t.Fatalf("got (%q, %q), want the profile and the model", profile, model)
	}
}

// A sidecar written before profiles were recorded holds a bare model name.
// It must keep working and report an empty profile, meaning "whatever endpoint
// is already active", which is what it meant when it was written.
func TestSessionPinReadsTheOldBareModelFormat(t *testing.T) {
	pinDir(t)
	p, err := sessionModelPath("s2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("qwen2.5-coder-32b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, model := ReadSessionBackend("s2")
	if profile != "" {
		t.Errorf("old format reported profile %q, want empty", profile)
	}
	if model != "qwen2.5-coder-32b" {
		t.Errorf("model = %q", model)
	}
	// The old accessor must still answer the same way.
	if got := ReadSessionModel("s2"); got != "qwen2.5-coder-32b" {
		t.Errorf("ReadSessionModel = %q", got)
	}
}

// A model pinned on the user's own endpoint has no profile, and that must
// round-trip as empty rather than as the literal string "".
func TestSessionPinWithNoProfileRoundTrips(t *testing.T) {
	pinDir(t)
	if err := writeSessionBackend("s3", "", "local-model"); err != nil {
		t.Fatal(err)
	}
	profile, model := ReadSessionBackend("s3")
	if profile != "" || model != "local-model" {
		t.Fatalf("got (%q, %q)", profile, model)
	}
	if got := ReadSessionModel("s3"); got != "local-model" {
		t.Errorf("ReadSessionModel = %q", got)
	}
}

// Clearing the pin removes it, so the session falls back to the server's own
// selection policy.
func TestSessionPinCanBeCleared(t *testing.T) {
	pinDir(t)
	if err := writeSessionBackend("s4", "free-groq", "llama"); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionBackend("s4", "", ""); err != nil {
		t.Fatal(err)
	}
	if profile, model := ReadSessionBackend("s4"); profile != "" || model != "" {
		t.Fatalf("pin survived clearing: (%q, %q)", profile, model)
	}
}
