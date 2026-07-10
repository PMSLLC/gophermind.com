package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// runtimeGOOS/runtimeGOARCH are indirections so tests could stub them if needed.
func runtimeGOOS() string  { return runtime.GOOS }
func runtimeGOARCH() string { return runtime.GOARCH }

// AssetName returns the release archive name for a platform, matching the
// GoReleaser name_template. macOS ships a single universal ("all") archive;
// Windows archives are .zip, others .tar.gz.
func AssetName(project, version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	arch := goarch
	if goos == "darwin" {
		arch = "all" // universal binary
	}
	return project + "_" + version + "_" + goos + "_" + arch + "." + ext
}

// ParseChecksums parses a checksums.txt ("<hex>  <filename>" per line) into a
// filename -> hex map.
func ParseChecksums(data string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out[fields[len(fields)-1]] = fields[0]
		}
	}
	return out
}

// VerifyChecksum reports whether the SHA-256 of data matches expectedHex
// (case-insensitive) — the integrity check before replacing the binary.
func VerifyChecksum(data []byte, expectedHex string) bool {
	sum := sha256.Sum256(data)
	return strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(expectedHex))
}

// The download/extract/replace flow below is network- and OS-dependent; the
// integrity-critical parts (AssetName, ParseChecksums, VerifyChecksum) are unit
// tested above.

// PerformUpgrade downloads the release archive for version from repo's GitHub
// releases, verifies its checksum against checksums.txt, extracts the project
// binary, and atomically replaces the running executable. releaseBase is the
// download base URL (overridable for testing); pass "" for GitHub.
func PerformUpgrade(repo, project, version, releaseBase string) error {
	if releaseBase == "" {
		releaseBase = "https://github.com/" + repo + "/releases/download/v" + version
	}
	asset := AssetName(project, version, runtimeGOOS(), runtimeGOARCH())

	sums, err := httpGet(releaseBase + "/checksums.txt")
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	want := ParseChecksums(string(sums))[asset]
	if want == "" {
		return fmt.Errorf("no checksum for %s in the release", asset)
	}
	archive, err := httpGet(releaseBase + "/" + asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	if !VerifyChecksum(archive, want) {
		return fmt.Errorf("checksum mismatch for %s — refusing to install", asset)
	}
	bin, err := extractBinary(asset, project, archive)
	if err != nil {
		return err
	}
	return replaceExecutable(bin)
}

func httpGet(url string) ([]byte, error) {
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// extractBinary pulls the project binary out of a .tar.gz or .zip archive.
func extractBinary(asset, project string, archive []byte) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == project || path.Base(f.Name) == project+".exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("binary %q not found in archive", project)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Base(hdr.Name) == project {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", project)
}

// replaceExecutable atomically swaps the running binary for newBin.
func replaceExecutable(newBin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".gophermind-upgrade-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace %s (need write permission?): %w", exe, err)
	}
	return nil
}
