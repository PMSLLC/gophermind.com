package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates every test in this package from any real global config on the
// developer's machine: it points GOPHERMIND_CONFIG_DIR at an empty temp directory
// so Load() never reads the user's ~/.config/gophermind/.env.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gophermind-config-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestLoadDotEnvFileFillsGapsAndRealEnvWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("GOPHERMIND_TESTKEY_A=fromfile\nGOPHERMIND_TESTKEY_B=fromfile\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// B is already exported in the real environment and must not be overwritten.
	t.Setenv("GOPHERMIND_TESTKEY_A", "")
	os.Unsetenv("GOPHERMIND_TESTKEY_A")
	t.Setenv("GOPHERMIND_TESTKEY_B", "fromenv")

	if err := loadDotEnvFile(path); err != nil {
		t.Fatalf("loadDotEnvFile: %v", err)
	}
	if got := os.Getenv("GOPHERMIND_TESTKEY_A"); got != "fromfile" {
		t.Errorf("A = %q, want fromfile (unset var should be filled from file)", got)
	}
	if got := os.Getenv("GOPHERMIND_TESTKEY_B"); got != "fromenv" {
		t.Errorf("B = %q, want fromenv (real env must win)", got)
	}
}

func TestLoadDotEnvFileMissingIsNotError(t *testing.T) {
	if err := loadDotEnvFile(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Errorf("missing file should not error, got %v", err)
	}
}

func TestLoadHasNoBuiltInBaseURL(t *testing.T) {
	// No private endpoint may be baked in as a default. With GOPHERMIND_BASE_URL
	// unset, BaseURL is empty and Validate demands the user supply one (which the
	// first-run wizard does interactively). Setting the var to "" makes it
	// present-but-empty, which also shields the test from any real global config.
	t.Setenv("GOPHERMIND_BASE_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty (no private default)", cfg.BaseURL)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate should require a base URL when none is configured")
	}
}

func TestConfigFilePathEndsWithGophermindEnv(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", "") // observe the default OS-config-dir path
	p, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath: %v", err)
	}
	want := filepath.Join("gophermind", ".env")
	if !strings.HasSuffix(p, want) {
		t.Errorf("ConfigFilePath = %q, want suffix %q", p, want)
	}
}

func TestConfigFilePathHonorsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	p, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath: %v", err)
	}
	if want := filepath.Join(dir, ".env"); p != want {
		t.Errorf("ConfigFilePath = %q, want %q", p, want)
	}
}

func TestBuiltinProfileNamesIncludesKnownProfiles(t *testing.T) {
	got := BuiltinProfileNames()
	names := map[string]string{}
	for _, p := range got {
		names[p[0]] = p[1]
	}
	for _, want := range []string{"local-llama", "openai", "anthropic-proxy"} {
		if _, ok := names[want]; !ok {
			t.Errorf("BuiltinProfileNames missing %q; got %v", want, got)
		}
	}
	if names["local-llama"] == "" {
		t.Errorf("local-llama should carry a base URL, got %v", got)
	}
}
