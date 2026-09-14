package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeServers writes a config.json whose mcpServers block is built from the
// given JSON fragments, keyed by name.
func writeServers(t *testing.T, entries map[string]string) string {
	t.Helper()
	parts := make([]string, 0, len(entries))
	for name, body := range entries {
		parts = append(parts, `"`+name+`":`+body)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"mcpServers":{`+strings.Join(parts, ",")+`}}`)
	return path
}

// stdioEntry is a config fragment pointing at this test binary's MCP server.
func stdioEntry(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"transport": TransportStdio,
		"command":   os.Args[0],
		"env":       map[string]string{serverEnvVar: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadAdaptsAndNamespacesTools(t *testing.T) {
	path := writeServers(t, map[string]string{"local": stdioEntry(t)})

	m, err := Load(context.Background(), path, "", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer m.Close()

	byName := map[string]bool{}
	for _, tool := range m.Tools() {
		byName[tool.Name] = true
	}
	if !byName["local__echo"] {
		t.Fatalf("want namespaced tool local__echo, got %v", byName)
	}
	if byName["echo"] {
		t.Error("un-namespaced tool name leaked into the registry")
	}

	// The adapted tool must actually reach the server.
	var echo = func() func(context.Context, json.RawMessage) (string, error) {
		for _, tool := range m.Tools() {
			if tool.Name == "local__echo" {
				return tool.Run
			}
		}
		return nil
	}()
	out, err := echo(context.Background(), json.RawMessage(`{"text":"wired"}`))
	if err != nil {
		t.Fatalf("call through adapted tool: %v", err)
	}
	if out != "echo: wired" {
		t.Errorf("got %q", out)
	}
}

// One unreachable server must not stop the healthy ones from loading.
func TestLoadSkipsDeadServers(t *testing.T) {
	path := writeServers(t, map[string]string{
		"local": stdioEntry(t),
		"dead":  `{"transport":"stdio","command":"/nonexistent/definitely-not-a-real-binary"}`,
	})

	var warnings []string
	m, err := Load(context.Background(), path, "", func(s string) { warnings = append(warnings, s) })
	if err != nil {
		t.Fatalf("a dead server must not fail the load: %v", err)
	}
	defer m.Close()

	var haveLocal bool
	for _, tool := range m.Tools() {
		if tool.Name == "local__echo" {
			haveLocal = true
		}
		if strings.HasPrefix(tool.Name, "dead__") {
			t.Errorf("dead server contributed a tool: %s", tool.Name)
		}
	}
	if !haveLocal {
		t.Error("healthy server did not load alongside the dead one")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "dead") {
		t.Errorf("expected one warning naming the dead server, got %v", warnings)
	}
}

// A configuration mistake is fatal, unlike an unreachable server.
func TestLoadConfigErrorIsFatal(t *testing.T) {
	path := writeServers(t, map[string]string{"bad": `{"transport":"nonsense"}`})
	if _, err := Load(context.Background(), path, "", nil); err == nil {
		t.Error("an invalid transport must fail the load")
	}
}

func TestLoadNoServersIsFine(t *testing.T) {
	m, err := Load(context.Background(), filepath.Join(t.TempDir(), "absent.json"), "", nil)
	if err != nil {
		t.Fatalf("MCP is opt-in: %v", err)
	}
	defer m.Close()
	if len(m.Tools()) != 0 {
		t.Errorf("want no tools, got %d", len(m.Tools()))
	}
}

// Every adapted tool must carry the separator safety.Gated keys off.
func TestAdaptedToolsAreGateable(t *testing.T) {
	path := writeServers(t, map[string]string{"local": stdioEntry(t)})
	m, err := Load(context.Background(), path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if len(m.Tools()) == 0 {
		t.Fatal("no tools loaded")
	}
	for _, tool := range m.Tools() {
		if !strings.Contains(tool.Name, NameSeparator) {
			t.Errorf("tool %q lacks the namespace separator and would run ungated", tool.Name)
		}
	}
}
