package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// fetchCacheDir returns the configured disk cache directory for fetched URLs, or
// "" when caching is disabled (the default).
func fetchCacheDir() string {
	return os.Getenv("GOPHERMIND_FETCH_CACHE_DIR")
}

// cacheKeyPath maps a URL to its cache file path under dir.
func cacheKeyPath(dir, url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".txt")
}

// cacheGet returns a previously cached fetch result for url, if present. This is
// what lets fetched docs be reused offline.
func cacheGet(dir, url string) (string, bool) {
	data, err := os.ReadFile(cacheKeyPath(dir, url))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// cachePut stores a fetch result for url, best-effort (errors are ignored so a
// caching failure never breaks a fetch).
func cachePut(dir, url, content string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(cacheKeyPath(dir, url), []byte(content), 0o644)
}
