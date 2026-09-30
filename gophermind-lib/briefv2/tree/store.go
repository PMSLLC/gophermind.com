package tree

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/briefv2/schema"
)

// Store keeps one JSON file per node under dir (a run directory).
type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: dir} }

var notNodes = map[string]bool{"contracts.json": true, "answers.json": true, "approval.json": true, "report.json": true,
	"requirements.json": true, "coverage.json": true, "dependencies.json": true}

// Write validates n against the node schema and writes it atomically.
func (s *Store) Write(n Node) error {
	raw, err := n.Marshal()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.KindNode, raw); err != nil {
		return fmt.Errorf("tree: node %s: %w", n.ID, err)
	}
	if err := n.checkRefs(); err != nil {
		return err
	}
	full := filepath.Join(s.dir, filepath.FromSlash(n.Path()))
	if rel, err := filepath.Rel(s.dir, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("tree: node %s: path %q escapes the store", n.ID, n.Path())
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// WriteAll rejects cycles and malformed structure, recomputes waves, then
// writes every node. Nothing is written when a check fails.
func (s *Store) WriteAll(t *Tree) error {
	if err := t.CheckStructure(); err != nil {
		return err
	}
	if err := t.AssignWaves(); err != nil {
		return err
	}
	for _, id := range t.ids() {
		if err := s.Write(t.Nodes[id]); err != nil {
			return err
		}
	}
	return nil
}

// Load reads every node file, skipping run artifacts, logs/ and the planner's
// _state/ working folder, and rejects a node that is stored somewhere other
// than its own Path().
func (s *Store) Load() (*Tree, error) {
	var nodes []Node
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "logs" || rel == "_state" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		rootLevel := !strings.Contains(rel, "/")
		if !strings.HasSuffix(name, ".json") || (rootLevel && notNodes[name]) || strings.HasSuffix(name, ".runtime.json") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n, err := ParseNode(raw)
		if err != nil {
			return fmt.Errorf("tree: %s: %w", rel, err)
		}
		if rel != n.Path() {
			return fmt.Errorf("tree: node %q is stored at %s, expected %s", n.ID, rel, n.Path())
		}
		nodes = append(nodes, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewTree(nodes)
}
