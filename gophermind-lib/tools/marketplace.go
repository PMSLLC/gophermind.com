package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallPlugin fetches a plugin manifest from source (a file path or http(s)
// URL), validates it, and writes it into pluginsDir as <name>.plugin.json so it
// is discovered on the next run. Returns the installed plugin name.
func InstallPlugin(pluginsDir, source string) (string, error) {
	var data []byte
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		data, err = fetchManifest(source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}
	var m PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}
	if m.Name == "" || m.Command == "" {
		return "", fmt.Errorf("manifest must set name and command")
	}
	if !nameSafe(m.Name) {
		return "", fmt.Errorf("invalid plugin name %q", m.Name)
	}
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(pluginsDir, m.Name+".plugin.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return m.Name, nil
}

func fetchManifest(url string) ([]byte, error) {
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("manifest fetch returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// nameSafe reports whether a plugin name is a safe filename component.
func nameSafe(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
