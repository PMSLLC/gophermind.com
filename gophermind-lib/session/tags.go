package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// tagsFile is the name of the JSON map (session id -> []tag) in the store dir.
const tagsFile = "tags.json"

// SetTags replaces the tag set for a session id.
func SetTags(id string, tags []string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return setTagsIn(dir, id, tags)
}

// AddTags adds tags to a session id, deduplicating against existing ones.
func AddTags(id string, tags []string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return addTagsIn(dir, id, tags)
}

// Tags returns the tags for a session id (empty when none).
func Tags(id string) []string {
	dir, err := Dir()
	if err != nil {
		return nil
	}
	return tagsIn(dir, id)
}

func setTagsIn(dir, id string, tags []string) error {
	if err := validID(id); err != nil {
		return err
	}
	m := loadTags(dir)
	m[id] = normalizeTags(tags)
	return writeTags(dir, m)
}

func addTagsIn(dir, id string, tags []string) error {
	if err := validID(id); err != nil {
		return err
	}
	m := loadTags(dir)
	m[id] = normalizeTags(append(m[id], tags...))
	return writeTags(dir, m)
}

func tagsIn(dir, id string) []string {
	return loadTags(dir)[id]
}

// normalizeTags sorts tags and removes empties and duplicates.
func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func loadTags(dir string) map[string][]string {
	m := map[string][]string{}
	data, err := os.ReadFile(filepath.Join(dir, tagsFile))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func writeTags(dir string, m map[string][]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, tagsFile), data, 0o600)
}

// Filter narrows a session listing. A zero-value field means "no constraint".
type Filter struct {
	Tag   string    // require this tag
	Since time.Time // keep sessions modified at/after this time
	Until time.Time // keep sessions modified at/before this time
}

// FilterInfos returns the subset of infos matching f. tagsOf resolves a
// session's tags (injected so the filter is testable without disk).
func FilterInfos(infos []Info, f Filter, tagsOf func(id string) []string) []Info {
	var out []Info
	for _, in := range infos {
		if f.Tag != "" && !contains(tagsOf(in.ID), f.Tag) {
			continue
		}
		if !f.Since.IsZero() && in.ModTime.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && in.ModTime.After(f.Until) {
			continue
		}
		out = append(out, in)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
