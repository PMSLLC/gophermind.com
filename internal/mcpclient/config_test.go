package mcpclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigsGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "config.json")
	writeFile(t, global, `{
	  "base_url": "http://ignored.invalid",
	  "mcpServers": {
	    "fs": {"transport": "stdio", "command": "npx", "args": ["-y", "srv"]},
	    "remote": {"transport": "http", "url": "https://example.invalid/mcp"}
	  }
	}`)

	got, err := LoadConfigs(global, filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 servers, got %d: %+v", len(got), got)
	}
	// Sorted by name: fs, remote.
	if got[0].Name != "fs" || got[0].Command != "npx" {
		t.Errorf("fs wrong: %+v", got[0])
	}
	if got[1].Name != "remote" || got[1].URL != "https://example.invalid/mcp" {
		t.Errorf("remote wrong: %+v", got[1])
	}
}

func TestLoadConfigsProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "config.json")
	writeFile(t, global, `{"mcpServers": {"db": {"transport": "stdio", "command": "global-cmd"}}}`)

	proj := filepath.Join(dir, ".gophermind")
	writeFile(t, filepath.Join(proj, "db.mcp.json"), `{"transport": "stdio", "command": "project-cmd"}`)

	got, err := LoadConfigs(global, proj)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 server, got %d: %+v", len(got), got)
	}
	if got[0].Command != "project-cmd" {
		t.Errorf("project entry must win, got %q", got[0].Command)
	}
	if got[0].Name != "db" {
		t.Errorf("name should come from the filename, got %q", got[0].Name)
	}
}

func TestLoadConfigsExpandsEnv(t *testing.T) {
	t.Setenv("MCP_TEST_TOKEN", "s3cret")
	dir := t.TempDir()
	global := filepath.Join(dir, "config.json")
	writeFile(t, global, `{"mcpServers": {"r": {
	  "transport": "http",
	  "url": "https://example.invalid/mcp",
	  "headers": {"Authorization": "Bearer ${MCP_TEST_TOKEN}"}
	}}}`)

	got, err := LoadConfigs(global, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if h := got[0].Headers["Authorization"]; h != "Bearer s3cret" {
		t.Errorf("env not expanded: %q", h)
	}
}

func TestLoadConfigsUnsetEnvIsFatal(t *testing.T) {
	os.Unsetenv("MCP_DEFINITELY_UNSET_VAR")
	dir := t.TempDir()
	global := filepath.Join(dir, "config.json")
	writeFile(t, global, `{"mcpServers": {"r": {
	  "transport": "http",
	  "url": "https://example.invalid/mcp",
	  "headers": {"Authorization": "Bearer ${MCP_DEFINITELY_UNSET_VAR}"}
	}}}`)

	_, err := LoadConfigs(global, "")
	if err == nil {
		t.Fatal("an unset ${VAR} must be fatal, not an empty header")
	}
	if !strings.Contains(err.Error(), "MCP_DEFINITELY_UNSET_VAR") {
		t.Errorf("error should name the missing variable, got: %v", err)
	}
}

func TestLoadConfigsMissingSourcesAreNotErrors(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadConfigs(filepath.Join(dir, "absent.json"), filepath.Join(dir, "absent"))
	if err != nil {
		t.Fatalf("MCP is opt-in; absent sources must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no servers, got %+v", got)
	}
}

func TestLoadConfigsValidation(t *testing.T) {
	cases := map[string]string{
		"missing transport": `{"mcpServers": {"a": {"command": "x"}}}`,
		"unknown transport": `{"mcpServers": {"a": {"transport": "carrier-pigeon", "url": "u"}}}`,
		"http without url":  `{"mcpServers": {"a": {"transport": "http"}}}`,
		"stdio without cmd": `{"mcpServers": {"a": {"transport": "stdio"}}}`,
		"separator in name": `{"mcpServers": {"a__b": {"transport": "stdio", "command": "x"}}}`,
		"malformed json":    `{"mcpServers": {`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			global := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, global, body)
			if _, err := LoadConfigs(global, ""); err == nil {
				t.Errorf("%s must be rejected", name)
			}
		})
	}
}

// Disabling a server must not paper over a mistake in its definition.
func TestLoadConfigsValidatesDisabledServers(t *testing.T) {
	global := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, global, `{"mcpServers": {"a": {"transport": "bogus", "disabled": true}}}`)
	if _, err := LoadConfigs(global, ""); err == nil {
		t.Error("a disabled server with an invalid transport must still be reported")
	}
}

func TestLoadConfigsDropsDisabled(t *testing.T) {
	global := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, global, `{"mcpServers": {
	  "on":  {"transport": "stdio", "command": "x"},
	  "off": {"transport": "stdio", "command": "y", "disabled": true}
	}}`)
	got, err := LoadConfigs(global, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Name != "on" {
		t.Errorf("disabled server should be dropped, got %+v", got)
	}
}
