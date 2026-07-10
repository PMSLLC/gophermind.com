// Package bundle exports and imports a shareable, versioned config bundle — the
// repo-local .gophermind directory (policy, prompts, personas, plugin manifests)
// as a single tar.gz — so a team can share consistent setup across repos.
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// configDirName is the repo-local config directory bundled.
const configDirName = ".gophermind"

// Export writes the repo's .gophermind directory to dst as a tar.gz. Session
// stores and caches are skipped — only shareable config is included.
func Export(root, dst string) error {
	srcDir := filepath.Join(root, configDirName)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("no %s directory to export", configDirName)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if skipFromBundle(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
}

// skipFromBundle excludes machine-specific/large state from the bundle.
func skipFromBundle(rel string) bool {
	top := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		top = rel[:i]
	}
	switch top {
	case "sessions", "docs-cache", "index.json", "memory.json", "episodes.json", "telemetry.json":
		return true
	}
	return false
}

// Import extracts a bundle (tar.gz) into the repo's .gophermind directory.
func Import(src, root string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a gzip bundle: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destDir := filepath.Join(root, configDirName)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Guard against path traversal in archive entries.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe path in bundle: %q", hdr.Name)
		}
		target := filepath.Join(destDir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(tr, 8<<20))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
