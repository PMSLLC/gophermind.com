package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gophermind/internal/llm"
)

// titleMax caps how many runes of the first user message are shown as a title.
const titleMax = 60

// Info summarizes one saved session for the `sessions` listing.
type Info struct {
	ID       string
	Path     string
	Size     int64
	ModTime  time.Time
	Messages int
	Title    string // first user message, truncated
	Name     string // custom display name (rename), empty when unset
}

// List returns all saved sessions, newest first.
func List() ([]Info, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return listDir(dir)
}

// listDir scans a directory for *.jsonl session files. A missing directory is
// treated as "no sessions" rather than an error.
func listDir(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var infos []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		path := filepath.Join(dir, e.Name())
		fi, err := e.Info()
		if err != nil {
			continue
		}
		msgs, title := summarize(path)
		infos = append(infos, Info{
			ID:       id,
			Path:     path,
			Size:     fi.Size(),
			ModTime:  fi.ModTime(),
			Messages: msgs,
			Title:    title,
			Name:     nameInDir(dir, id),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ModTime.After(infos[j].ModTime) })
	return infos, nil
}

// summarize reads a session file and returns its message count and a title (the
// first user message, truncated). Malformed lines are skipped so a partially
// written session still lists.
func summarize(path string) (count int, title string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, ""
	}
	defer f.Close()

	// Encrypted sessions can't be summarized without the key; label them.
	head := make([]byte, len(encMagic))
	if n, _ := f.Read(head); isEncrypted(head[:n]) {
		return 0, "(encrypted)"
	}
	if _, err := f.Seek(0, 0); err != nil {
		return 0, ""
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		count++
		if title != "" {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(line), &m) == nil && m.Role == "user" && m.Content != "" {
			title = truncate(oneLine(m.Content), titleMax)
		}
	}
	return count, title
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Remove deletes a saved session by id.
func Remove(id string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return removeIn(dir, id)
}

// removeIn deletes a session file from a specific directory, validating the id
// so it can never escape the sessions directory.
func removeIn(dir, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".jsonl")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("session %q not found", id)
	}
	// Best-effort: drop the sidecar name so a reused id does not inherit it.
	_ = os.Remove(filepath.Join(dir, id+".name"))
	return os.Remove(path)
}
