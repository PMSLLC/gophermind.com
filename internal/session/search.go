package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/internal/llm"
)

// snippetMax caps the length of the context snippet shown for a search hit.
const snippetMax = 100

// SearchHit is one session matching a full-text search.
type SearchHit struct {
	ID      string
	Matches int    // number of matching messages
	Snippet string // first matching message content, truncated
}

// Search scans all saved sessions for messages containing query (case-
// insensitive) and returns the matching sessions, most matches first. Encrypted
// sessions are skipped (their content can't be read without the key).
func Search(query string) ([]SearchHit, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return searchDir(dir, query)
}

func searchDir(dir, query string) ([]SearchHit, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, nil
	}

	var hits []SearchHit
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if h, ok := searchFile(filepath.Join(dir, e.Name()), id, needle); ok {
			hits = append(hits, h)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Matches > hits[j].Matches })
	return hits, nil
}

// searchFile scans one session file for messages whose content contains needle.
func searchFile(path, id, needle string) (SearchHit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SearchHit{}, false
	}
	defer f.Close()

	// Skip encrypted sessions — the magic prefix means opaque ciphertext.
	head := make([]byte, len(encMagic))
	if n, _ := f.Read(head); isEncrypted(head[:n]) {
		return SearchHit{}, false
	}
	if _, err := f.Seek(0, 0); err != nil {
		return SearchHit{}, false
	}

	hit := SearchHit{ID: id}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if strings.Contains(strings.ToLower(m.Content), needle) {
			hit.Matches++
			if hit.Snippet == "" {
				hit.Snippet = truncate(oneLine(m.Content), snippetMax)
			}
		}
	}
	return hit, hit.Matches > 0
}
