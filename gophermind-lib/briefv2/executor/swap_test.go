package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/pathsafe"
)

// swapRig is the greeter after Wave 0 (tests, types and stubs committed) with
// a Swap over fn-greet.
func swapRig(t *testing.T) (*rig, *Leaf, *Swap, []byte) {
	t.Helper()
	g := newRig(t)
	g.startWave0()
	l := g.plan.Leaf("fn-greet")
	stub, err := stubFor(g.plan.Contracts, g.plan.Policy(), l)
	if err != nil {
		t.Fatal(err)
	}
	return g, l, NewSwap(g.repo, l, g.git, stub), stub
}

func (g *rig) read(rel string) string {
	g.t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.repo, filepath.FromSlash(rel)))
	if err != nil {
		g.t.Fatal(err)
	}
	return string(raw)
}

func (g *rig) abs(rel string) string { return filepath.Join(g.repo, filepath.FromSlash(rel)) }

func TestStubSwapOnFailureRestoresStub(t *testing.T) {
	t.Run("enter then fail", func(t *testing.T) {
		g, l, s, stub := swapRig(t)
		if err := s.Enter([]byte(good("fn-greet"))); err != nil {
			t.Fatal(err)
		}
		if fileExists(g.abs(l.StubFile)) {
			t.Fatal("the stub is still there after Enter")
		}
		if g.read(l.File) != good("fn-greet") {
			t.Fatal("the real file is not what Enter was given")
		}
		if err := s.Fail(); err != nil {
			t.Fatal(err)
		}
		if fileExists(g.abs(l.File)) {
			t.Fatal("the real file survived Fail")
		}
		if g.read(l.StubFile) != string(stub) {
			t.Fatal("the stub is not back byte for byte")
		}
		if err := s.Fail(); err != nil {
			t.Fatalf("second Fail: %v", err)
		}
		if g.read(l.StubFile) != string(stub) || fileExists(g.abs(l.File)) {
			t.Fatal("a second Fail changed the tree")
		}
		if d, _ := g.git.Dirty(); len(d) != 0 {
			t.Fatalf("tree dirty after Fail: %v", d)
		}
	})
	t.Run("enter can be repeated after a fail", func(t *testing.T) {
		g, l, s, _ := swapRig(t)
		for i := 0; i < 3; i++ {
			if err := s.Enter([]byte(bad("fn-greet", 1))); err != nil {
				t.Fatal(err)
			}
			if err := s.Fail(); err != nil {
				t.Fatal(err)
			}
		}
		if st, _ := s.State(); st != SwapStubOnly {
			t.Fatalf("state = %v, want stub only", st)
		}
		if fileExists(g.abs(l.File)) {
			t.Fatal("real file left")
		}
	})
	t.Run("read-only parent errors and leaves the stub", func(t *testing.T) {
		g, l, s, stub := swapRig(t)
		dir := filepath.Dir(g.abs(l.File))
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o755)
		if err := s.Enter([]byte(good("fn-greet"))); err == nil {
			t.Fatal("Enter succeeded in a read-only directory")
		}
		os.Chmod(dir, 0o755)
		if g.read(l.StubFile) != string(stub) || fileExists(g.abs(l.File)) {
			t.Fatal("the stub must be in place and no real file written")
		}
	})
	t.Run("replace failing after the stub was removed puts the stub back", func(t *testing.T) {
		g, l, s, stub := swapRig(t)
		// A directory at the real file's path makes Replace refuse after Remove(stub) succeeded.
		if err := os.Mkdir(g.abs(l.File), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.Enter([]byte(good("fn-greet"))); err == nil {
			t.Fatal("Enter wrote over a directory")
		}
		if g.read(l.StubFile) != string(stub) {
			t.Fatal("the stub was not rewritten after the failed Enter")
		}
	})
	t.Run("reopen mode restores the committed content and writes no stub", func(t *testing.T) {
		g, l, s, _ := swapRig(t)
		if err := s.Enter([]byte(good("fn-greet"))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pass("greet"); err != nil {
			t.Fatal(err)
		}
		committed := g.read(l.File)
		r := NewSwap(g.repo, l, g.git, []byte("package greet\n"))
		r.Reopen()
		if err := r.Enter([]byte(bad("fn-greet", 1))); err != nil {
			t.Fatal(err)
		}
		if g.read(l.File) == committed {
			t.Fatal("Enter did not write the new source")
		}
		if err := r.Fail(); err != nil {
			t.Fatal(err)
		}
		if g.read(l.File) != committed {
			t.Fatal("reopen Fail did not restore the committed file")
		}
		if fileExists(g.abs(l.StubFile)) {
			t.Fatal("reopen Fail wrote a stub")
		}
		if d, _ := g.git.Dirty(); len(d) != 0 {
			t.Fatalf("dirty after reopen Fail: %v", d)
		}
	})
}

func TestSwapRefusesSymlinkedParent(t *testing.T) {
	g, l, s, _ := swapRig(t)
	outside := t.TempDir()
	dir := filepath.Dir(g.abs(l.File))
	if err := os.Rename(dir, dir+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Enter([]byte(good("fn-greet"))); err == nil {
		t.Fatal("Enter wrote through a symlinked directory")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something was written outside the repo: %v", entries)
	}
	if err := s.Fail(); err == nil {
		t.Log("Fail through a symlinked directory also refuses")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("Fail wrote outside the repo: %v", entries)
	}
}

func TestLeafCommitRemovesStub(t *testing.T) {
	g, l, s, _ := swapRig(t)
	before, _ := g.git.Head()
	if err := s.Enter([]byte(good("fn-greet"))); err != nil {
		t.Fatal(err)
	}
	h, err := s.Pass("Greet")
	if err != nil {
		t.Fatal(err)
	}
	if h == before || h == "" {
		t.Fatalf("Pass returned %q", h)
	}
	out := g.gitCmd("show", "--name-status", "--format=", h)
	got := strings.Fields(strings.ReplaceAll(out, "\t", " "))
	want := []string{"A", l.File, "D", l.StubFile}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("commit changes = %q, want %q", got, want)
	}
	if st := g.gitCmd("status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("status not clean: %q", st)
	}
	if st, _ := s.State(); st != SwapRealOnly {
		t.Fatalf("state after Pass = %v", st)
	}
	// After a pass, Fail must never take the committed file away.
	if err := s.Fail(); err != nil {
		t.Fatal(err)
	}
	if !fileExists(g.abs(l.File)) || fileExists(g.abs(l.StubFile)) {
		t.Fatal("Fail after Pass changed the committed leaf")
	}
}

func TestPassRefusesWrongState(t *testing.T) {
	_, _, s, _ := swapRig(t)
	if _, err := s.Pass("x"); err == nil {
		t.Fatal("Pass committed with no real file")
	}
	if _, err := s.PassRepair(1); err == nil {
		t.Fatal("PassRepair committed with no real file")
	}
}

func TestPassRepairCommits(t *testing.T) {
	g, l, s, _ := swapRig(t)
	if err := s.Enter([]byte(good("fn-greet"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pass("Greet"); err != nil {
		t.Fatal(err)
	}
	r := NewSwap(g.repo, l, g.git, nil)
	r.Reopen()
	if err := r.Enter([]byte(good("fn-greet") + "\n// repaired\n")); err != nil {
		t.Fatal(err)
	}
	h, err := r.PassRepair(1)
	if err != nil {
		t.Fatal(err)
	}
	out := g.gitCmd("show", "--name-status", "--format=", h)
	if strings.Join(strings.Fields(out), " ") != "M "+l.File {
		t.Fatalf("repair commit = %q", out)
	}
}

func TestSwapStateFromDisk(t *testing.T) {
	g, l, s, stub := swapRig(t)
	set := func(stubThere, realThere bool) {
		t.Helper()
		os.Remove(g.abs(l.StubFile))
		os.Remove(g.abs(l.File))
		if stubThere {
			if err := pathsafe.Replace(g.repo, l.StubFile, stub); err != nil {
				t.Fatal(err)
			}
		}
		if realThere {
			if err := pathsafe.Replace(g.repo, l.File, []byte(good("fn-greet"))); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		stub, real bool
		want       SwapState
	}{{true, false, SwapStubOnly}, {false, true, SwapRealOnly}, {true, true, SwapBoth}, {false, false, SwapNeither}} {
		set(tc.stub, tc.real)
		if got, err := s.State(); err != nil || got != tc.want {
			t.Errorf("stub=%v real=%v: state = %v, %v; want %v", tc.stub, tc.real, got, err, tc.want)
		}
	}
	// A symlink in place of either file is not a state.
	os.Remove(g.abs(l.StubFile))
	os.Remove(g.abs(l.File))
	if err := os.Symlink("/etc/hosts", g.abs(l.File)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.State(); err == nil {
		t.Fatal("a symlink was reported as a state")
	}
}

func TestResumeNormalizesStubAndReal(t *testing.T) {
	realSrc := good("fn-greet")
	cases := []struct {
		name            string
		stubThere, real bool
		wantStub        bool
		wantReal        bool
	}{
		{"stub only stays", true, false, true, false},
		{"real only stays", false, true, false, true},
		{"both removes the stub", true, true, false, true},
		{"neither writes the stub", false, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, l, s, stub := swapRig(t)
			if err := WriteTypesForTest(g); err != nil {
				t.Fatal(err)
			}
			os.Remove(g.abs(l.StubFile))
			os.Remove(g.abs(l.File))
			if tc.stubThere {
				if err := pathsafe.Replace(g.repo, l.StubFile, stub); err != nil {
					t.Fatal(err)
				}
			}
			if tc.real {
				if err := pathsafe.Replace(g.repo, l.File, []byte(realSrc)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Normalize(); err != nil {
				t.Fatal(err)
			}
			if fileExists(g.abs(l.StubFile)) != tc.wantStub || fileExists(g.abs(l.File)) != tc.wantReal {
				t.Fatalf("after Normalize stub=%v real=%v", fileExists(g.abs(l.StubFile)), fileExists(g.abs(l.File)))
			}
			if tc.wantReal && g.read(l.File) != realSrc {
				t.Fatal("the real file changed")
			}
			if tc.wantStub {
				want, err := stubFor(g.plan.Contracts, g.plan.Policy(), l)
				if err != nil || g.read(l.StubFile) != string(want) {
					t.Fatalf("the stub is not stubFor (%v)", err)
				}
			}
			// Only fn-greet's files matter to the build of this package when the other leaf has no file.
			os.Remove(g.abs(g.plan.Leaf("fn-farewell").StubFile))
			if out, err := goIn(t, g.repo, "build", "./internal/greet"); err != nil {
				t.Fatalf("go build after Normalize: %v\n%s", err, out)
			}
		})
	}
}

// WriteTypesForTest makes sure the contract types are on disk; startWave0 already did this.
func WriteTypesForTest(g *rig) error {
	_, err := WriteTypes(g.repo, g.plan.Contracts, g.plan.Policy())
	return err
}

func TestReplyCannotNamePath(t *testing.T) {
	g, l, s, _ := swapRig(t)
	snap, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	evil := "package greet\n\n// FILE: ../evil.go\n// FILE: /etc/passwd.go\n// path: internal/greet/other.go\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	if err := s.Enter([]byte(evil)); err != nil {
		t.Fatal(err)
	}
	stray, err := snap.Stray(g.repo, g.git, l.File, l.StubFile)
	if err != nil || len(stray) != 0 {
		t.Fatalf("Stray = %v, %v; want none", stray, err)
	}
	if fileExists(filepath.Join(g.repo, "..", "evil.go")) || fileExists("/etc/passwd.go") || fileExists(g.abs("internal/greet/other.go")) {
		t.Fatal("a path named in the reply was written")
	}
	entries, _ := os.ReadDir(filepath.Dir(g.abs(l.File)))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, n := range names {
		if strings.HasPrefix(n, ".") {
			t.Fatalf("temp file left behind: %v", names)
		}
	}
}
