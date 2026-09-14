package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPluginFromFile(t *testing.T) {
	src := t.TempDir()
	manifest := `{"name":"hello","description":"d","command":"/bin/echo","schema":{"type":"object"}}`
	mf := filepath.Join(src, "hello.plugin.json")
	os.WriteFile(mf, []byte(manifest), 0o644)

	dst := filepath.Join(t.TempDir(), "plugins")
	name, err := InstallPlugin(dst, mf)
	if err != nil {
		t.Fatal(err)
	}
	if name != "hello" {
		t.Errorf("installed name = %q", name)
	}
	// The manifest should now load as a plugin from dst.
	plugins, err := LoadPlugins(dst)
	if err != nil || len(plugins) != 1 {
		t.Fatalf("installed plugin should load: %v err=%v", plugins, err)
	}
}

func TestInstallPluginFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"web","description":"d","command":"/bin/echo"}`))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "plugins")
	name, err := InstallPlugin(dst, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if name != "web" {
		t.Errorf("installed name = %q", name)
	}
}

func TestInstallPluginBadManifest(t *testing.T) {
	src := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(src, []byte(`{"description":"no name or command"}`), 0o644)
	if _, err := InstallPlugin(filepath.Join(t.TempDir(), "p"), src); err == nil {
		t.Error("manifest without name/command should be rejected")
	}
}
