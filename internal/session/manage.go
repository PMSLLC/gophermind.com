package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GC deletes saved sessions whose files are older than maxAge and returns the
// removed ids. A non-positive maxAge is a no-op.
func GC(maxAge time.Duration) ([]string, error) {
	if maxAge <= 0 {
		return nil, nil
	}
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return gcDir(dir, maxAge, time.Now())
}

// gcDir removes *.jsonl sessions in dir last modified before now-maxAge.
func gcDir(dir string, maxAge time.Duration, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := now.Add(-maxAge)
	var removed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return removed, err
			}
			removed = append(removed, strings.TrimSuffix(e.Name(), ".jsonl"))
		}
	}
	return removed, nil
}

// Export writes a saved session's history to dst.
func Export(id, dst string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return exportIn(dir, id, dst)
}

func exportIn(dir, id, dst string) error {
	if err := validID(id); err != nil {
		return err
	}
	src := filepath.Join(dir, id+".jsonl")
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("session %q not found", id)
	}
	return os.WriteFile(dst, data, 0o600)
}

// Import copies a session history file into the store under id.
func Import(src, id string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return importIn(dir, src, id)
}

func importIn(dir, src, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer f.Close()

	// Validate the first non-empty line is a JSON object so we don't import junk.
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	firstOK := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var probe map[string]any
		if json.Unmarshal([]byte(line), &probe) != nil {
			return fmt.Errorf("%s is not a valid session (JSONL) file", src)
		}
		firstOK = true
		break
	}
	if !firstOK {
		return fmt.Errorf("%s is empty", src)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0o600)
}
