package library

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	n := 0
	s.now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	return s, dir
}

func mustSave(t *testing.T, s *Store, fam, parent string) Version {
	t.Helper()
	v, err := s.Save(fam, parent, "l", []byte("text "+parent))
	if err != nil {
		t.Fatalf("save under %q: %v", parent, err)
	}
	return v
}

func TestSaveChainAndBranch(t *testing.T) {
	s, _ := newStore(t)
	v1 := mustSave(t, s, "fam", "")
	v11 := mustSave(t, s, "fam", v1.Name)
	v12 := mustSave(t, s, "fam", v11.Name)
	b := mustSave(t, s, "fam", v11.Name)
	if v1.Name != "v1" || v11.Name != "v1.1" || v12.Name != "v1.2" || b.Name != "v1.1.1" {
		t.Fatalf("names: %s %s %s %s", v1.Name, v11.Name, v12.Name, b.Name)
	}
	if b.Parent != "v1.1" || b.Created == "" || b.Locked {
		t.Fatalf("bad version %+v", b)
	}
	if _, err := s.Save("fam", "", "x", []byte("y")); err == nil {
		t.Fatal("empty parent allowed in non-empty family")
	}
	if _, err := s.Save("fam", "v9", "x", []byte("y")); !errors.Is(err, ErrNoSuchVersion) {
		t.Fatalf("missing parent: %v", err)
	}
	fams, err := s.List()
	if err != nil || len(fams) != 1 {
		t.Fatalf("list: %v %v", fams, err)
	}
	var got []string
	for _, v := range fams[0].Versions {
		got = append(got, v.Name)
	}
	want := []string{"v1", "v1.1", "v1.2", "v1.1.1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tree order %v want %v", got, want)
	}
}

func TestLockedRefusesUpdate(t *testing.T) {
	s, dir := newStore(t)
	mustSave(t, s, "fam", "")
	if err := s.Update("fam", "v1", []byte("edited")); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("fam", "v1"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "fam", "v1", "brief.md"))
	if err := s.Update("fam", "v1", []byte("changed")); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "fam", "v1", "brief.md"))
	if !bytes.Equal(before, after) || string(after) != "edited" {
		t.Fatalf("bytes changed: %q", after)
	}
	v, _, err := s.Get("fam", "v1")
	if err != nil || !v.Locked {
		t.Fatalf("get: %+v %v", v, err)
	}
	if err := s.Lock("fam", "v9"); !errors.Is(err, ErrNoSuchVersion) {
		t.Fatalf("lock missing: %v", err)
	}
}

func TestConcurrentSavesSameParent(t *testing.T) {
	s, _ := newStore(t)
	mustSave(t, s, "fam", "")
	var mu sync.Mutex
	names := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := s.Save("fam", "v1", "l", []byte(fmt.Sprint("t", i)))
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			names[v.Name] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(names) != 20 {
		t.Fatalf("distinct names %d", len(names))
	}
	fams, _ := s.List()
	if len(fams[0].Versions) != 21 {
		t.Fatalf("versions %d", len(fams[0].Versions))
	}
}

func TestFindIdentical(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Save("fam", "", "l", []byte("a\nb\n")); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.FindIdentical("fam", []byte("a\nb\n")); !ok || v.Name != "v1" {
		t.Fatalf("exact: %v %v", v, ok)
	}
	if _, ok := s.FindIdentical("fam", []byte("a\r\nb\r\n")); ok {
		t.Fatal("CRLF matched")
	}
	if _, ok := s.FindIdentical("fam", []byte("a\nb")); ok {
		t.Fatal("trailing newline matched")
	}
	if _, ok := s.FindIdentical("nope", []byte("a")); ok {
		t.Fatal("missing family matched")
	}
}

func TestUnsafeFamilyRefused(t *testing.T) {
	s, dir := newStore(t)
	for _, f := range []string{"../x", "a/b", "", ".", "..", "-a", "a-", "A", "a b", string(bytes.Repeat([]byte("a"), 61))} {
		if _, err := s.Save(f, "", "l", []byte("x")); err == nil {
			t.Errorf("save accepted %q", f)
		}
		if _, _, err := s.Get(f, "v1"); err == nil {
			t.Errorf("get accepted %q", f)
		}
		if err := s.Update(f, "v1", []byte("x")); err == nil {
			t.Errorf("update accepted %q", f)
		}
		if err := s.Lock(f, "v1"); err == nil {
			t.Errorf("lock accepted %q", f)
		}
	}
	if _, _, err := s.Get("fam", "../v1"); err == nil {
		t.Error("unsafe version accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "x")); err == nil {
		t.Fatal("directory created outside dir")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("created %v", ents)
	}
}

func TestOversize(t *testing.T) {
	s, _ := newStore(t)
	big := make([]byte, 262144+1)
	if _, err := s.Save("fam", "", "l", big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("save: %v", err)
	}
	if _, err := s.Save("fam", "", "l", big[:262144]); err != nil {
		t.Fatalf("at limit: %v", err)
	}
	if err := s.Update("fam", "v1", big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("update: %v", err)
	}
}

func TestLockForRun(t *testing.T) {
	s, _ := newStore(t)
	mustSave(t, s, "fam", "")
	boom := errors.New("boom")
	if err := s.LockForRun("fam", "v1", func(Version, []byte) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if v, _, _ := s.Get("fam", "v1"); v.Locked {
		t.Fatal("locked after fn error")
	}
	if err := s.LockForRun("fam", "v9", func(Version, []byte) error { return nil }); !errors.Is(err, ErrNoSuchVersion) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.LockForRun("fam", "v1", func(Version, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := s.Get("fam", "v1"); !v.Locked {
		t.Fatal("not locked after fn success")
	}
}

func TestLockForRunConcurrentUpdate(t *testing.T) {
	for i := 0; i < 30; i++ {
		s, dir := newStore(t)
		mustSave(t, s, "fam", "")
		var seen []byte
		var seenV Version
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.LockForRun("fam", "v1", func(v Version, text []byte) error {
				seen, seenV = text, v
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = s.Update("fam", "v1", []byte(fmt.Sprint("edit", j)))
			}
		}()
		wg.Wait()
		v, text, err := s.Get("fam", "v1")
		if err != nil || !v.Locked {
			t.Fatalf("not locked: %v", err)
		}
		disk, _ := os.ReadFile(filepath.Join(dir, "fam", "v1", "brief.md"))
		if !bytes.Equal(seen, disk) || seenV.Name != "v1" {
			t.Fatalf("fn text %q differs from disk %q", seen, disk)
		}
		if !bytes.Equal(seen, text) {
			t.Fatalf("bytes changed after lock: ran on %q, now %q", seen, text)
		}
	}
}

func TestLockForRunPassesStoredText(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Save("fam", "", "l", []byte("exact\r\n")); err != nil {
		t.Fatal(err)
	}
	called := false
	err := s.LockForRun("fam", "v1", func(v Version, text []byte) error {
		called = true
		if v.Name != "v1" || string(text) != "exact\r\n" {
			t.Errorf("got %+v %q", v, text)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err %v called %v", err, called)
	}
}

func TestFailedSaveCleansUp(t *testing.T) {
	s, dir := newStore(t)
	mustSave(t, s, "fam", "")
	s.afterMkdir = func() error { return errors.New("disk full") }
	if _, err := s.Save("fam", "v1", "l", []byte("x")); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(dir, "fam", "v1.1")); !os.IsNotExist(err) {
		t.Fatalf("orphan left: %v", err)
	}
	s.afterMkdir = nil
	v, err := s.Save("fam", "v1", "l", []byte("x"))
	if err != nil || v.Name != "v1.1" {
		t.Fatalf("retry: %+v %v", v, err)
	}
}

func TestOrphanDirReclaimed(t *testing.T) {
	s, dir := newStore(t)
	mustSave(t, s, "fam", "")
	orphan := filepath.Join(dir, "fam", "v1.1")
	if err := os.Mkdir(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(orphan, "junk"), []byte("j"), 0o644)
	v, err := s.Save("fam", "v1", "l", []byte("fresh"))
	if err != nil || v.Name != "v1.1" {
		t.Fatalf("save: %+v %v", v, err)
	}
	if _, text, err := s.Get("fam", "v1.1"); err != nil || string(text) != "fresh" {
		t.Fatalf("get: %q %v", text, err)
	}
	if _, err := os.Stat(filepath.Join(orphan, "junk")); !os.IsNotExist(err) {
		t.Fatal("orphan contents survived")
	}
}

func TestCorruptMetaNotReclaimed(t *testing.T) {
	s, dir := newStore(t)
	mustSave(t, s, "fam", "")
	vd := filepath.Join(dir, "fam", "v1.1")
	if err := os.Mkdir(vd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(vd, "brief.md"), []byte("precious"), 0o644)
	os.WriteFile(filepath.Join(vd, "meta.json"), []byte("{trunc"), 0o644)
	if _, err := s.Save("fam", "v1", "l", []byte("new")); err == nil || !strings.Contains(err.Error(), "v1.1") {
		t.Fatalf("expected error naming dir, got %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(vd, "brief.md")); err != nil || string(b) != "precious" {
		t.Fatalf("brief lost: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(vd, "meta.json")); err != nil {
		t.Fatalf("meta lost: %v", err)
	}
}
