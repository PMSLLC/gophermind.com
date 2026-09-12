package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile honours umask, so set the mode explicitly: the point of these
	// tests is the mode.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// No config file is the ordinary case, not an error: the desktop works with
// only its embedded server, and remote backends are opt-in like every other
// integration in this project.
func TestLoadBackendsWithNoConfigFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	got, err := loadBackendConfig(filepath.Join(dir, "backends.json"))
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d backends, want none", len(got))
	}
}

// A config file that exists but cannot be parsed is a failure and is reported.
// Falling back to "no remote backends" would silently drop a backend the user
// configured, and they would find out by wondering where their sessions went.
func TestLoadBackendsReportsAMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "backends.json"), "{not json", 0o600)
	if _, err := loadBackendConfig(path); err == nil {
		t.Error("malformed config returned no error")
	}
}

// A token file readable by anyone else on the machine is refused.
// This token authorizes shell execution on another machine, and a
// world-readable copy of it is worth as much to a local attacker as the shell
// itself. gocloak's SecretRef refuses the same way, and matching that is
// deliberate.
func TestLoadBackendsRefusesALooseTokenFile(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "s3cret", 0o644)
	cfg := `[{"name":"box","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + tok + `"}]`
	path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)

	got, err := loadBackendConfig(path)
	if err != nil {
		t.Fatalf("one bad entry must not fail the load: %v", err)
	}
	if len(got) != 1 || got[0].Available {
		t.Fatalf("a world-readable token file was accepted: %+v", got)
	}
	if !strings.Contains(got[0].Reason, "permission") {
		t.Errorf("reason does not explain the permission problem: %q", got[0].Reason)
	}
	if strings.Contains(got[0].Reason, "s3cret") {
		t.Error("the reason contains the token itself")
	}
	if got[0].Token != "" {
		t.Error("an unavailable backend holds a token")
	}
}

func TestLoadBackendsAcceptsA0600TokenFile(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "  s3cret\n", 0o600)
	cfg := `[{"name":"box","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + tok + `"}]`
	path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)

	got, err := loadBackendConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d backends, want 1", len(got))
	}
	if got[0].Token != "s3cret" {
		t.Errorf("token = %q, want the file contents with surrounding whitespace trimmed", got[0].Token)
	}
	if got[0].Name != "box" || got[0].Kind != BackendURL {
		t.Errorf("got %+v", got[0])
	}
}

// An empty token file is a misconfiguration that would otherwise produce a
// backend whose every request 401s, with nothing saying why.
func TestLoadBackendsRejectsAnEmptyTokenFile(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "\n  \n", 0o600)
	cfg := `[{"name":"box","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + tok + `"}]`
	path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)

	got, err := loadBackendConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Available {
		t.Errorf("an empty token file was accepted: %+v", got)
	}
}

// "local" is the embedded server and is registered before anything from the
// config. A config entry claiming that name would make it ambiguous which
// machine a session ran on, which is the one thing this design must never be.
func TestLoadBackendsRejectsTheReservedLocalName(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "s3cret", 0o600)
	cfg := `[{"name":"local","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + tok + `"}]`
	path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)

	if _, err := loadBackendConfig(path); err == nil {
		t.Error(`a backend named "local" was accepted`)
	}
}

// A base URL that is not http or https would be routed to by the proxy as
// written. file:// or a bare host are configuration mistakes, caught at load
// and surfaced as an unavailable backend rather than discovered at first use.
func TestLoadBackendsRejectsANonHTTPBaseURL(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "s3cret", 0o600)
	for _, bad := range []string{"file:///etc/passwd", "10.0.0.1:8090", "ftp://x/y", ""} {
		cfg := `[{"name":"box","kind":"url","base_url":"` + bad + `","token_file":"` + tok + `"}]`
		path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)
		got, err := loadBackendConfig(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(got) != 1 || got[0].Available {
			t.Errorf("base_url %q was accepted: %+v", bad, got)
		}
	}
}

// The backend name becomes a path segment in /b/<name>/..., so a name with a
// slash or a traversal sequence would route somewhere other than where it
// reads.
func TestLoadBackendsRejectsNamesThatAreNotPathSafe(t *testing.T) {
	dir := t.TempDir()
	tok := writeFile(t, filepath.Join(dir, "tok"), "s3cret", 0o600)
	for _, bad := range []string{"a/b", "..", "a b", "", "a?b"} {
		cfg := `[{"name":"` + bad + `","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + tok + `"}]`
		path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)
		if _, err := loadBackendConfig(path); err == nil {
			t.Errorf("backend name %q was accepted", bad)
		}
	}
}

// A backend whose token file is missing or unusable must not stop the app
// from launching. Bricking a desktop app over one bad entry in a config file
// is a worse failure than the missing backend: the user loses the local
// server too, and the window shows nothing at all.
//
// It must not be silently dropped either, which is the mistake this project
// already made once in LoadSettings. The entry is kept, marked unavailable,
// and carries the reason, so the UI can say why rather than the backend just
// not being there.
func TestLoadBackendsKeepsABrokenEntryAsUnavailable(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, filepath.Join(dir, "good"), "s3cret", 0o600)
	cfg := `[
	  {"name":"box","kind":"url","base_url":"http://10.0.0.1:8090","token_file":"` + good + `"},
	  {"name":"broken","kind":"url","base_url":"http://10.0.0.2:8090","token_file":"` + dir + `/nope"}
	]`
	path := writeFile(t, filepath.Join(dir, "backends.json"), cfg, 0o600)

	got, err := loadBackendConfig(path)
	if err != nil {
		t.Fatalf("one broken entry must not fail the whole load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d backends, want 2 (the broken one kept and marked)", len(got))
	}

	byName := map[string]Backend{}
	for _, b := range got {
		byName[b.Name] = b
	}
	if b := byName["box"]; !b.Available || b.Token != "s3cret" {
		t.Errorf("the good backend should be available: %+v", b)
	}
	broken := byName["broken"]
	if broken.Available {
		t.Error("the broken backend is marked available")
	}
	if broken.Reason == "" {
		t.Error("the broken backend carries no reason")
	}
	if broken.Token != "" {
		t.Error("an unavailable backend should hold no token")
	}
}

// An unavailable backend must not be routable: a request to it is an error,
// never a request sent with an empty credential.
func TestRouterRefusesAnUnavailableBackend(t *testing.T) {
	stub := newBackendStub()
	front, frontToken := routerFor(t, stub)

	resp := do(t, http.MethodGet, front.URL+"/b/down/session", frontToken, "")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("an unavailable backend was routed to")
	}
}
