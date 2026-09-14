package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// "mcpServers" is a structured key owned by internal/mcpclient, not an
// environment variable. The scalar loader must step over it rather than
// rejecting it as an unsupported object type.
func TestLoadConfigFileSkipsMCPServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "base_url": "http://example.invalid",
	  "mcpServers": {
	    "pelagosnow": {
	      "transport": "http",
	      "url": "https://mcp.pelagosnow.com/mcp",
	      "headers": {"Authorization": "Bearer ${TOK}"}
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	pairs, err := readConfigFile(path)
	if err != nil {
		t.Fatalf("mcpServers must not fail the scalar loader: %v", err)
	}
	for _, kv := range pairs {
		if kv[0] == "GOPHERMIND_MCPSERVERS" || kv[0] == "MCPSERVERS" {
			t.Errorf("mcpServers leaked into the environment as %q", kv[0])
		}
	}
	// The sibling scalar key still loads.
	var found bool
	for _, kv := range pairs {
		if kv[0] == "GOPHERMIND_BASE_URL" && kv[1] == "http://example.invalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("scalar keys beside mcpServers must still load, got %v", pairs)
	}
}

// Save documents itself as a merge that preserves keys the caller does not
// mention. A structured key it does not manage must survive a save, or the
// setup wizard silently deletes the user's MCP servers.
func TestSavePreservesMCPServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "base_url": "http://old.invalid",
	  "mcpServers": {"fs": {"transport": "stdio", "command": "npx"}}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Save(path, [][2]string{{"GOPHERMIND_BASE_URL", "http://new.invalid"}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved config is not valid JSON: %v", err)
	}
	if raw["base_url"] != "http://new.invalid" {
		t.Errorf("scalar key not updated: %v", raw["base_url"])
	}
	servers, ok := raw["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("Save dropped the mcpServers block; got %#v", raw["mcpServers"])
	}
	fs, ok := servers["fs"].(map[string]any)
	if !ok || fs["command"] != "npx" {
		t.Errorf("mcpServers content mangled: %#v", servers)
	}
}

// A non-scalar key that is NOT reserved must still be loud, so a typo like
// {"base_url": {"oops": 1}} does not silently do nothing.
func TestLoadConfigFileStillRejectsUnknownObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"base_url": {"oops": 1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(path); err == nil {
		t.Error("an unsupported object value must still be an error")
	}
}
