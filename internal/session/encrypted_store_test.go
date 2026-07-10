package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/internal/agent"
)

// seedAgent builds an agent with a couple of seeded messages via the public
// LoadHistory path.
func seedAgent(t *testing.T) *agent.Agent {
	t.Helper()
	a := agent.New(nil, nil, 1, nil, nil)
	seed := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"encrypted please"}` + "\n"
	if err := a.LoadHistory(strings.NewReader(seed)); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSaveLoadEncryptedRoundTrip(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfgDir)
	t.Setenv("GOPHERMIND_SESSION_KEY", "a-strong-passphrase")

	if err := Save("sec", seedAgent(t)); err != nil {
		t.Fatalf("save: %v", err)
	}

	// On disk it must be encrypted (magic prefix, no plaintext leak).
	p, _ := Path("sec")
	raw, _ := os.ReadFile(p)
	if !isEncrypted(raw) {
		t.Fatal("session file is not encrypted")
	}
	if strings.Contains(string(raw), "encrypted please") {
		t.Fatal("plaintext leaked into the encrypted file")
	}

	// Loading with the key restores the history.
	restored := agent.New(nil, nil, 1, nil, nil)
	if err := Load("sec", restored); err != nil {
		t.Fatalf("load: %v", err)
	}
	var out strings.Builder
	restored.ExportJSONL(&out)
	if !strings.Contains(out.String(), "encrypted please") {
		t.Errorf("restored history missing content: %s", out.String())
	}
}

func TestLoadEncryptedWithoutKeyErrors(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfgDir)
	t.Setenv("GOPHERMIND_SESSION_KEY", "key-present-for-save")
	if err := Save("sec", seedAgent(t)); err != nil {
		t.Fatal(err)
	}

	// Now unset the key and try to load — must error clearly, not garble.
	t.Setenv("GOPHERMIND_SESSION_KEY", "")
	err := Load("sec", agent.New(nil, nil, 1, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("expected an encrypted-session error, got %v", err)
	}
}

func TestListLabelsEncryptedSession(t *testing.T) {
	dir := t.TempDir()
	// Write a fake encrypted file directly.
	os.WriteFile(filepath.Join(dir, "e.jsonl"), append([]byte(encMagic), 0xDE, 0xAD), 0o600)
	infos, err := listDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Title != "(encrypted)" {
		t.Errorf("encrypted session not labeled: %+v", infos)
	}
}
