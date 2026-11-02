package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxTextBytes is the largest version text the store accepts (256 KiB).
const MaxTextBytes = 262144

var (
	// ErrLocked is returned when a locked version would be changed.
	ErrLocked = errors.New("version is locked")
	// ErrNoSuchVersion is returned when a family or version does not exist.
	ErrNoSuchVersion = errors.New("no such version")
	// ErrTooLarge is returned when version text exceeds MaxTextBytes.
	ErrTooLarge = errors.New("version text too large")
)

var (
	familyRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,58}[a-z0-9])?$`)
	versionRe = regexp.MustCompile(`^v[0-9]+[a-z]?(\.[0-9]+[a-z]?)*$`)
)

// createdLayout is fixed width with nanoseconds, so Created strings sort in
// creation order even for versions saved within the same second.
const createdLayout = "2006-01-02T15:04:05.000000000Z"

// Version is one saved brief in a family.
type Version struct {
	Family, Name, Parent, Label, Created string
	Locked                               bool
}

// Family is a named set of versions in tree order.
type Family struct {
	Name     string
	Versions []Version
}

type meta struct {
	Family   string `json:"family"`
	Name     string `json:"name"`
	Parent   string `json:"parent"`
	Label    string `json:"label"`
	Created  string `json:"created"`
	LockedAt string `json:"locked_at"`
}

// Store keeps versions as <dir>/<family>/<version>/{brief.md,meta.json}.
type Store struct {
	dir string
	now func() time.Time
	// afterMkdir is a test hook run after a new version directory is made.
	afterMkdir func() error

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Open returns a store rooted at dir, creating it if needed.
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: abs, now: time.Now, locks: map[string]*sync.Mutex{}}, nil
}

func (s *Store) famLock(family string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.locks[family]
	if m == nil {
		m = &sync.Mutex{}
		s.locks[family] = m
	}
	return m
}

func checkFamily(family string) error {
	if !familyRe.MatchString(family) {
		return fmt.Errorf("invalid family %q", family)
	}
	return nil
}

func checkVersion(name string) error {
	if !versionRe.MatchString(name) {
		return fmt.Errorf("invalid version name %q", name)
	}
	return nil
}

// within confirms p resolves inside the store dir.
func (s *Store) within(p string) (string, error) {
	p = filepath.Clean(p)
	if !strings.HasPrefix(p, s.dir+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the store", p)
	}
	return p, nil
}

func (s *Store) famDir(family string) (string, error) {
	if err := checkFamily(family); err != nil {
		return "", err
	}
	return s.within(filepath.Join(s.dir, family))
}

func (s *Store) verDir(family, name string) (string, error) {
	if err := checkVersion(name); err != nil {
		return "", err
	}
	fd, err := s.famDir(family)
	if err != nil {
		return "", err
	}
	return s.within(filepath.Join(fd, name))
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func readMeta(vdir string) (meta, error) {
	var m meta
	b, err := os.ReadFile(filepath.Join(vdir, "meta.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

func writeMeta(vdir string, m meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(vdir, "meta.json"), append(b, '\n'))
}

func (m meta) version() Version {
	return Version{Family: m.Family, Name: m.Name, Parent: m.Parent, Label: m.Label,
		Created: m.Created, Locked: m.LockedAt != ""}
}

// metas reads every version of a family. Caller holds the family lock or accepts a snapshot.
func (s *Store) metas(fdir string) ([]meta, error) {
	ents, err := os.ReadDir(fdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []meta
	for _, e := range ents {
		if !e.IsDir() || !versionRe.MatchString(e.Name()) {
			continue
		}
		m, err := readMeta(filepath.Join(fdir, e.Name()))
		if err != nil || m.Name != e.Name() {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Save creates the next version under parent. An empty parent is allowed only
// for a family with no versions, which yields v1.
func (s *Store) Save(family, parent, label string, text []byte) (Version, error) {
	fdir, err := s.famDir(family)
	if err != nil {
		return Version{}, err
	}
	if len(text) > MaxTextBytes {
		return Version{}, ErrTooLarge
	}
	if parent != "" {
		if err := checkVersion(parent); err != nil {
			return Version{}, err
		}
	}
	l := s.famLock(family)
	l.Lock()
	defer l.Unlock()

	ms, err := s.metas(fdir)
	if err != nil {
		return Version{}, err
	}
	var all, children []string
	hasParent := false
	for _, m := range ms {
		all = append(all, m.Name)
		if m.Parent == parent {
			children = append(children, m.Name)
		}
		if m.Name == parent {
			hasParent = true
		}
	}
	if parent == "" && len(ms) > 0 {
		return Version{}, fmt.Errorf("family %q already has versions; a parent is required", family)
	}
	if parent != "" && !hasParent {
		return Version{}, ErrNoSuchVersion
	}
	name, err := NextName(parent, all, children)
	if err != nil {
		return Version{}, err
	}
	vdir, err := s.verDir(family, name)
	if err != nil {
		return Version{}, err
	}
	if err := os.MkdirAll(fdir, 0o755); err != nil {
		return Version{}, err
	}
	// A directory with no valid meta.json is an orphan of a failed save. We
	// hold the family lock and it is not in metas, so it is safe to reclaim.
	if _, statErr := os.Lstat(vdir); statErr == nil {
		if _, merr := readMeta(vdir); merr != nil {
			if !errors.Is(merr, fs.ErrNotExist) {
				return Version{}, fmt.Errorf("version directory %s has an unreadable meta.json, not reclaiming: %w", vdir, merr)
			}
			if err := removeInside(fdir, vdir); err != nil {
				return Version{}, err
			}
		}
	}
	if err := os.Mkdir(vdir, 0o755); err != nil {
		return Version{}, err
	}
	m := meta{Family: family, Name: name, Parent: parent, Label: label,
		Created: s.now().UTC().Format(createdLayout)}
	if err := s.fillVersion(vdir, m, text); err != nil {
		if rerr := removeInside(fdir, vdir); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return Version{}, err
	}
	return m.version(), nil
}

func (s *Store) fillVersion(vdir string, m meta, text []byte) error {
	if s.afterMkdir != nil {
		if err := s.afterMkdir(); err != nil {
			return err
		}
	}
	if err := writeAtomic(filepath.Join(vdir, "brief.md"), text); err != nil {
		return err
	}
	return writeMeta(vdir, m)
}

// removeInside deletes target, which must be an absolute path strictly inside base.
func removeInside(base, target string) error {
	if target == "" || base == "" || !filepath.IsAbs(target) || !filepath.IsAbs(base) ||
		!strings.HasPrefix(filepath.Clean(target), filepath.Clean(base)+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %q outside %q", target, base)
	}
	return os.RemoveAll(filepath.Clean(target))
}

// Update rewrites a draft's text in place. A locked version returns ErrLocked.
func (s *Store) Update(family, name string, text []byte) error {
	vdir, err := s.verDir(family, name)
	if err != nil {
		return err
	}
	if len(text) > MaxTextBytes {
		return ErrTooLarge
	}
	l := s.famLock(family)
	l.Lock()
	defer l.Unlock()
	m, err := readMeta(vdir)
	if err != nil {
		return ErrNoSuchVersion
	}
	if m.LockedAt != "" {
		return ErrLocked
	}
	return writeAtomic(filepath.Join(vdir, "brief.md"), text)
}

// Get returns a version and its text.
func (s *Store) Get(family, name string) (Version, []byte, error) {
	vdir, err := s.verDir(family, name)
	if err != nil {
		return Version{}, nil, err
	}
	l := s.famLock(family)
	l.Lock()
	defer l.Unlock()
	m, err := readMeta(vdir)
	if err != nil {
		return Version{}, nil, ErrNoSuchVersion
	}
	b, err := os.ReadFile(filepath.Join(vdir, "brief.md"))
	if err != nil {
		return Version{}, nil, ErrNoSuchVersion
	}
	return m.version(), b, nil
}

// Lock marks a version immutable. Locking a locked version is a no-op.
func (s *Store) Lock(family, name string) error {
	return s.LockForRun(family, name, func(Version, []byte) error { return nil })
}

// LockForRun reads the version under the family lock, passes it and its exact
// text to fn, and locks the version only if fn returns nil. If fn fails its
// error is returned and the version stays a draft. The text fn sees is the text
// that gets locked. fn must not call back into the Store for the same family
// (the family lock is held, so it would deadlock).
func (s *Store) LockForRun(family, name string, fn func(v Version, text []byte) error) error {
	vdir, err := s.verDir(family, name)
	if err != nil {
		return err
	}
	l := s.famLock(family)
	l.Lock()
	defer l.Unlock()
	m, err := readMeta(vdir)
	if err != nil {
		return ErrNoSuchVersion
	}
	text, err := os.ReadFile(filepath.Join(vdir, "brief.md"))
	if err != nil {
		return ErrNoSuchVersion
	}
	if err := fn(m.version(), text); err != nil {
		return err
	}
	if m.LockedAt != "" {
		return nil
	}
	m.LockedAt = s.now().UTC().Format(time.RFC3339)
	return writeMeta(vdir, m)
}

// FindIdentical returns a version whose text is byte-for-byte equal to text.
func (s *Store) FindIdentical(family string, text []byte) (Version, bool) {
	fdir, err := s.famDir(family)
	if err != nil {
		return Version{}, false
	}
	l := s.famLock(family)
	l.Lock()
	defer l.Unlock()
	ms, err := s.metas(fdir)
	if err != nil {
		return Version{}, false
	}
	for _, m := range treeOrder(ms) {
		b, err := os.ReadFile(filepath.Join(fdir, m.Name, "brief.md"))
		if err == nil && bytes.Equal(b, text) {
			return m.version(), true
		}
	}
	return Version{}, false
}

// List returns every family sorted by name, versions in tree order.
func (s *Store) List() ([]Family, error) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Family
	for _, e := range ents {
		if !e.IsDir() || checkFamily(e.Name()) != nil {
			continue
		}
		l := s.famLock(e.Name())
		l.Lock()
		ms, err := s.metas(filepath.Join(s.dir, e.Name()))
		l.Unlock()
		if err != nil {
			return nil, err
		}
		if len(ms) == 0 {
			continue
		}
		f := Family{Name: e.Name()}
		for _, m := range treeOrder(ms) {
			f.Versions = append(f.Versions, m.version())
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// treeOrder puts parents before children; siblings go by creation then name.
// Versions whose parent is missing are treated as roots.
func treeOrder(ms []meta) []meta {
	have := map[string]bool{}
	for _, m := range ms {
		have[m.Name] = true
	}
	kids := map[string][]meta{}
	for _, m := range ms {
		p := m.Parent
		if !have[p] {
			p = ""
		}
		kids[p] = append(kids[p], m)
	}
	for _, k := range kids {
		sort.Slice(k, func(i, j int) bool {
			if k[i].Created != k[j].Created {
				return k[i].Created < k[j].Created
			}
			return k[i].Name < k[j].Name
		})
	}
	var out []meta
	var walk func(p string)
	walk = func(p string) {
		for _, m := range kids[p] {
			out = append(out, m)
			walk(m.Name)
		}
	}
	walk("")
	return out
}
