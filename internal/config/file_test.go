package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirDefaultsToHomeDotGophermind(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".gophermind"); dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}

	p, err := ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".gophermind", "config.json"); p != want {
		t.Errorf("ConfigFilePath() = %q, want %q", p, want)
	}
}

func TestConfigDirEnvOverridesHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)

	p, err := ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "config.json"); p != want {
		t.Errorf("ConfigFilePath() = %q, want %q", p, want)
	}
}

// TestLoadConfigFileFriendlyKeys: the file is meant to be hand-edited, so a
// short lowercase key maps onto its GOPHERMIND_* variable, while an all-caps
// key (GITHUB_TOKEN, or a fully-spelled GOPHERMIND_*) is used verbatim.
func TestLoadConfigFileFriendlyKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "base_url": "http://192.168.5.2:8080/v1",
	  "max_iter": 40,
	  "insecure_tls": true,
	  "fallback_models": ["a", "b"],
	  "GITHUB_TOKEN": "gh-tok",
	  "GOPHERMIND_MODEL": "qwen"
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"GOPHERMIND_BASE_URL", "GOPHERMIND_MAX_ITER", "GOPHERMIND_INSECURE_TLS",
		"GOPHERMIND_FALLBACK_MODELS", "GITHUB_TOKEN", "GOPHERMIND_MODEL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if err := loadConfigFile(path); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"GOPHERMIND_BASE_URL":        "http://192.168.5.2:8080/v1",
		"GOPHERMIND_MAX_ITER":        "40",
		"GOPHERMIND_INSECURE_TLS":    "true",
		"GOPHERMIND_FALLBACK_MODELS": "a,b",
		"GITHUB_TOKEN":               "gh-tok",
		"GOPHERMIND_MODEL":           "qwen",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// TestLoadConfigFileDoesNotOverrideRealEnv keeps the file a gap-filler: a real
// exported variable always wins.
func TestLoadConfigFileDoesNotOverrideRealEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"base_url":"http://from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPHERMIND_BASE_URL", "http://from-env")

	if err := loadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GOPHERMIND_BASE_URL"); got != "http://from-env" {
		t.Errorf("GOPHERMIND_BASE_URL = %q, want the real environment to win", got)
	}
}

func TestLoadConfigFileMissingIsNotAnError(t *testing.T) {
	if err := loadConfigFile(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Errorf("missing file returned %v, want nil", err)
	}
}

func TestLoadConfigFileMalformedIsLoud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"base_url": }`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadConfigFile(path); err == nil {
		t.Error("malformed config.json parsed without error")
	}
}

// TestSaveMergesAndPreservesHandEdits is the property that makes the file safe
// to edit: re-running the wizard must not drop keys it does not know about.
func TestSaveMergesAndPreservesHandEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"base_url":"http://old","llm_timeout":"15m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Save(path, [][2]string{
		{"GOPHERMIND_BASE_URL", "http://new"},
		{"GOPHERMIND_MODEL", "qwen"},
	})
	if err != nil {
		t.Fatal(err)
	}

	doc := readDoc(t, path)
	if doc["base_url"] != "http://new" {
		t.Errorf("base_url = %q, want the updated value", doc["base_url"])
	}
	if doc["model"] != "qwen" {
		t.Errorf("model = %q, want %q", doc["model"], "qwen")
	}
	if doc["llm_timeout"] != "15m" {
		t.Errorf("hand-edited llm_timeout was dropped: %q", doc["llm_timeout"])
	}
}

// TestSaveEmptyValueUnsets: an empty pair removes its key rather than writing a
// blank one, which is how a setting is cleared.
func TestSaveEmptyValueUnsets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, [][2]string{{"GOPHERMIND_API_KEY", "secret"}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, [][2]string{{"GOPHERMIND_API_KEY", ""}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readDoc(t, path)["api_key"]; ok {
		t.Error("api_key survived being set to empty")
	}
}

func TestSaveCreatesDirAndFileWithTightPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := Save(path, [][2]string{{"GOPHERMIND_API_KEY", "secret"}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perm = %04o, want 0600 (it can hold an API key)", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %04o, want 0700", perm)
	}
}

// TestSaveRoundTripsThroughLoad is the end-to-end contract: what the wizard
// writes is what Load reads back.
func TestSaveRoundTripsThroughLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	path := filepath.Join(dir, "config.json")

	if err := Save(path, [][2]string{
		{"GOPHERMIND_BASE_URL", "http://round.trip"},
		{"GOPHERMIND_APPROVAL", "auto"},
		{"GITHUB_TOKEN", "gh"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"GOPHERMIND_BASE_URL", "GOPHERMIND_APPROVAL", "GITHUB_TOKEN"} {
		os.Unsetenv(k)
	}
	if err := loadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GOPHERMIND_BASE_URL"); got != "http://round.trip" {
		t.Errorf("GOPHERMIND_BASE_URL = %q", got)
	}
	if got := os.Getenv("GITHUB_TOKEN"); got != "gh" {
		t.Errorf("GITHUB_TOKEN = %q", got)
	}
}

// TestMigrateConvertsLegacyEnvAndMovesSiblings is the upgrade path: the old
// directory's .env becomes config.json and every sibling state file comes with
// it, so sessions and history are not orphaned.
func TestMigrateConvertsLegacyEnvAndMovesSiblings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", "")
	t.Setenv("HOME", home)

	old, err := legacyDir()
	if err != nil {
		t.Skip("no OS user config dir on this platform")
	}
	if err := os.MkdirAll(filepath.Join(old, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	envBody := "GOPHERMIND_BASE_URL=http://legacy:8080/v1\nGOPHERMIND_MODEL=\"qwen coder\"\n# a comment\n"
	if err := os.WriteFile(filepath.Join(old, ".env"), []byte(envBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "history"), []byte("\"hi\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	moved, err := Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("Migrate reported nothing moved")
	}

	newDir := filepath.Join(home, ".gophermind")
	doc := readDoc(t, filepath.Join(newDir, "config.json"))
	if doc["base_url"] != "http://legacy:8080/v1" {
		t.Errorf("base_url = %q, want the legacy endpoint", doc["base_url"])
	}
	if doc["model"] != "qwen coder" {
		t.Errorf("model = %q, want the unquoted legacy value", doc["model"])
	}
	if _, err := os.Stat(filepath.Join(newDir, "history")); err != nil {
		t.Errorf("history did not come along: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "sessions")); err != nil {
		t.Errorf("sessions/ did not come along: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, ".env")); !os.IsNotExist(err) {
		t.Error("legacy .env survived the migration; there must be one source of truth")
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("emptied legacy directory %s was left behind", old)
	}

	// Re-running must be a no-op, not a second migration.
	again, err := Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("Migrate moved something on the second run")
	}
}

func TestMigrateNoOpWhenConfigDirPinned(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	moved, err := Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Error("Migrate ran despite an explicit GOPHERMIND_CONFIG_DIR")
	}
}

func readDoc(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]string
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, strings.TrimSpace(string(data)))
	}
	return doc
}
