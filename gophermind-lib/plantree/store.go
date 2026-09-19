package plantree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/lockfile"
)

var (
	// ErrNotFound is returned when a node does not exist.
	ErrNotFound = errors.New("plantree: node not found")
	// ErrExists is returned when creating a node that already exists.
	ErrExists = errors.New("plantree: node already exists")
)

// ConflictError reports an Update made against a stale revision.
type ConflictError struct {
	ID       string
	Expected int
	Actual   int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("plantree: %s changed: expected revision %d, found %d", e.ID, e.Expected, e.Actual)
}

// Repo is the on-disk plan tree. Every mutation takes one exclusive
// cross-process lock and replaces files atomically, so a reader never sees a
// half-written node. Reads take no lock, so a Walk, Summarize or NextActions
// running while several writes commit can see nodes from different moments,
// though each node is always consistent.
type Repo struct {
	dir string
}

// Open returns the repository whose tree lives in <planningDir>/plan.
func Open(planningDir string) *Repo {
	return &Repo{dir: filepath.Join(planningDir, "plan")}
}

func (r *Repo) metaPath(id string) (string, error) {
	rel, err := MetaPath(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.dir, filepath.FromSlash(rel)), nil
}

func (r *Repo) lock() (func(), error) {
	state := filepath.Join(r.dir, "_state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, err
	}
	return lockfile.Acquire(filepath.Join(state, "write.lock"))
}

func (r *Repo) read(id string) (Node, error) {
	p, err := r.metaPath(id)
	if err != nil {
		return Node{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Node{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Node{}, err
	}
	n, err := Decode(b)
	if err != nil {
		return Node{}, fmt.Errorf("%s: %w", p, err)
	}
	if n.ID != id {
		return Node{}, fmt.Errorf("plantree: %s holds id %q, expected %q", p, n.ID, id)
	}
	return n, nil
}

func (r *Repo) write(n Node) error {
	p, err := r.metaPath(n.ID)
	if err != nil {
		return err
	}
	b, err := Encode(n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return lockfile.WriteAtomic(p, b, 0o644)
}

// Get returns the node with the given id.
func (r *Repo) Get(id string) (Node, error) { return r.read(id) }

// Init writes the root node under the write lock. It fails with ErrExists if one is present.
func (r *Repo) Init(root Node) error {
	if root.ID != RootID {
		return fmt.Errorf("plantree: Init needs the root node, got %q", root.ID)
	}
	if err := Validate(root); err != nil {
		return err
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := r.read(RootID); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, RootID)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.write(root)
}

// Create adds a new node under the write lock. Its parent must exist and it must start at
// revision 1.
func (r *Repo) Create(n Node) error {
	if n.ID == RootID {
		return errors.New("plantree: use Init for the root")
	}
	if err := Validate(n); err != nil {
		return err
	}
	if n.NodeRevision != 1 {
		return fmt.Errorf("plantree: new node %s must start at revision 1", n.ID)
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	parent, err := ParentID(n.ID)
	if err != nil {
		return err
	}
	if _, err := r.read(parent); err != nil {
		return fmt.Errorf("plantree: parent of %s: %w", n.ID, err)
	}
	if _, err := r.read(n.ID); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, n.ID)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.write(n)
}

// Update applies mutate to the node under the write lock. mutate must not
// call any Repo method: the lock is not reentrant, so the call would block
// forever. It fails with a
// *ConflictError if the node's revision is not expectedRevision, so a stale
// caller can never overwrite newer data. id and parent_ref are immutable.
func (r *Repo) Update(id string, expectedRevision int, mutate func(*Node) error) (Node, error) {
	unlock, err := r.lock()
	if err != nil {
		return Node{}, err
	}
	defer unlock()
	cur, err := r.read(id)
	if err != nil {
		return Node{}, err
	}
	if cur.NodeRevision != expectedRevision {
		return Node{}, &ConflictError{ID: id, Expected: expectedRevision, Actual: cur.NodeRevision}
	}
	next := cur
	if err := mutate(&next); err != nil {
		return Node{}, err
	}
	if next.ID != cur.ID {
		return Node{}, fmt.Errorf("plantree: %s: id is immutable", id)
	}
	next.NodeRevision = cur.NodeRevision + 1
	if err := Validate(next); err != nil {
		return Node{}, err
	}
	if err := r.write(next); err != nil {
		return Node{}, err
	}
	return next, nil
}

// Children returns id's direct children in numeric order. A directory is a
// member only if it is correctly named and holds a document.
func (r *Repo) Children(id string) ([]Node, error) {
	rel, ok, err := ContainerDir(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(r.dir, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Node
	for _, e := range entries {
		if !e.IsDir() || !segmentRE.MatchString(e.Name()) {
			continue
		}
		childID := e.Name()
		if id != RootID {
			childID = id + "." + e.Name()
		}
		if _, err := ParseID(childID); err != nil {
			continue // not a valid id at this position, so not a member
		}
		n, err := r.read(childID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Walk visits every node in pre-order, siblings in numeric order.
func (r *Repo) Walk(fn func(Node) error) error {
	root, err := r.read(RootID)
	if err != nil {
		return err
	}
	return r.walk(root, fn)
}

func (r *Repo) walk(n Node, fn func(Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	kids, err := r.Children(n.ID)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := r.walk(k, fn); err != nil {
			return err
		}
	}
	return nil
}
