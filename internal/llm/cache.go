package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"gophermind/internal/safety"
)

// maxCacheFileBytes caps how much of a cache file is read before it is treated
// as garbage. A planted oversized file must not exhaust memory: anything beyond
// this is rejected as a miss. 16 MiB is far larger than any real cached
// completion (prompt + response) yet bounds a hostile file.
const maxCacheFileBytes = 16 << 20

// Cache is a content-addressed, on-disk cache for non-streaming completions.
// Entries are keyed by a SHA-256 of the request inputs that determine the
// output (model, messages, tools, sampling params) and expire after TTL.
//
// PRIVACY: cached files contain the full prompt and response content, which may
// be sensitive. They are written with 0600 perms in a 0700 directory and the
// API key is never part of the key or the stored value. Caching is therefore
// opt-in (off by default) — see config.CacheEnabled.
type Cache struct {
	Dir string        // containment root for entry files (created 0700 on first write)
	TTL time.Duration // entries older than TTL are treated as a miss
}

// cacheKeyInput is the canonical, deterministic projection of a request that
// determines its completion. Marshaled as a struct (stable field order) so the
// SHA-256 over it is reproducible — never hash a Go map directly. The API key
// and any auth material are intentionally absent.
type cacheKeyInput struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools"`
	Temperature float64   `json:"temperature"`
	ToolChoice  string    `json:"tool_choice"`
}

// cacheEntry is the on-disk record. CreatedAt drives TTL expiry.
type cacheEntry struct {
	CreatedAt time.Time `json:"created_at"`
	Message   Message   `json:"message"`
	Usage     Usage     `json:"usage"`
}

// key derives the hex SHA-256 filename for a request. Returns "" only if the
// canonical input cannot be marshaled (it always can for these types).
func (c *Cache) key(req ChatRequest) (string, error) {
	in := cacheKeyInput{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		ToolChoice:  req.ToolChoice,
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// path resolves the entry file for a hex key inside the cache dir. The key is a
// SHA-256 hex (safe charset), but the path is still built via safety.SafeJoin so
// no key can ever escape the cache directory.
func (c *Cache) path(key string) (string, error) {
	return safety.SafeJoin(c.Dir, key)
}

// get returns a fresh cached entry for req, or ok=false on any miss: no file, an
// unreadable/oversized/corrupt file, a hash mismatch, or an expired entry. A
// miss never errors out a real request — bad cache files are best-effort removed
// and ignored. The returned Usage is the entry's stored usage.
func (c *Cache) get(req ChatRequest) (Message, Usage, bool) {
	key, err := c.key(req)
	if err != nil {
		return Message{}, Usage{}, false
	}
	p, err := c.path(key)
	if err != nil {
		return Message{}, Usage{}, false
	}
	f, err := os.Open(p)
	if err != nil {
		return Message{}, Usage{}, false
	}
	defer f.Close()

	// Cap the read so a planted huge file cannot exhaust memory.
	data, err := io.ReadAll(io.LimitReader(f, maxCacheFileBytes+1))
	if err != nil || len(data) > maxCacheFileBytes {
		c.remove(p)
		return Message{}, Usage{}, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		// Corrupt/garbage content: treat as a miss and drop it.
		c.remove(p)
		return Message{}, Usage{}, false
	}
	if c.TTL > 0 && time.Since(entry.CreatedAt) > c.TTL {
		c.remove(p)
		return Message{}, Usage{}, false
	}
	return entry.Message, entry.Usage, true
}

// put stores the completion for req. Failures are non-fatal (caching is a
// best-effort optimization): a write error must never break a real request, so
// the error is swallowed by the caller. The file is written atomically (temp +
// rename) with 0600 perms in a 0700 directory.
func (c *Cache) put(req ChatRequest, msg Message, usage Usage) {
	key, err := c.key(req)
	if err != nil {
		return
	}
	p, err := c.path(key)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return
	}
	entry := cacheEntry{CreatedAt: time.Now(), Message: msg, Usage: usage}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	c.writeAtomic(p, data)
}

// writeAtomic writes data to a temp file in the same directory and renames it
// into place, so a reader never observes a partial file. The temp file is
// created 0600; on any failure it is cleaned up and no partial entry remains.
func (c *Cache) writeAtomic(dst string, data []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
	}
}

// remove deletes a cache file, ignoring errors (it may already be gone).
func (c *Cache) remove(p string) { _ = os.Remove(p) }
