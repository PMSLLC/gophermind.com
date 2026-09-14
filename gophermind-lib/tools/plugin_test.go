package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlugin(t *testing.T, dir, name, script string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPluginToolRuns(t *testing.T) {
	dir := t.TempDir()
	// A plugin that reads args JSON on stdin and echoes a result JSON.
	bin := writePlugin(t, dir, "echo.sh", "#!/bin/sh\nread line\necho \"{\\\"result\\\":\\\"got: $line\\\"}\"\n")

	tool := PluginTool("myecho", "echoes", map[string]any{"type": "object"}, bin)
	if tool.Name != "myecho" {
		t.Fatalf("name = %q", tool.Name)
	}
	out, err := run(t, tool, `{"x":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "got:") {
		t.Errorf("plugin output = %q", out)
	}
}

func TestPluginToolError(t *testing.T) {
	dir := t.TempDir()
	bin := writePlugin(t, dir, "err.sh", "#!/bin/sh\necho '{\"error\":\"boom\"}'\n")
	if _, err := run(t, PluginTool("p", "d", nil, bin), `{}`); err == nil {
		t.Error("plugin returning an error field should surface as an error")
	}
}

func TestLoadPluginManifests(t *testing.T) {
	dir := t.TempDir()
	bin := writePlugin(t, dir, "tool.sh", "#!/bin/sh\necho '{\"result\":\"ok\"}'\n")
	manifest := `{"name":"custom","description":"a custom tool","command":"` + bin + `","schema":{"type":"object"}}`
	os.WriteFile(filepath.Join(dir, "custom.plugin.json"), []byte(manifest), 0o644)

	plugins, err := LoadPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].Name != "custom" {
		t.Fatalf("expected 1 plugin named custom, got %+v", plugins)
	}
}

func TestLoadPluginsMissingDir(t *testing.T) {
	plugins, err := LoadPlugins(filepath.Join(t.TempDir(), "none"))
	if err != nil || plugins != nil {
		t.Errorf("missing dir should be (nil,nil), got (%v,%v)", plugins, err)
	}
}
