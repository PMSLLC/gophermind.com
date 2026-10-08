package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/brief"
)

const probeCanary = "CANARY-probe-5530"

func writeRepoFile(t *testing.T, repo, rel, body string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProbeReadsModuleVersionAndExportedNamesOnly(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "go.mod", "module example.com/acme // "+probeCanary+"\n\ngo 1.22.3\n")
	writeRepoFile(t, repo, "store/store.go", `package store

// `+probeCanary+` in a comment
type User struct{ ID string }
type hidden struct{}
const Limit = 3
var secretToken = "`+probeCanary+`"

func New() *User { _ = "`+probeCanary+`"; return nil }
func (u *User) Name() string { return "" }
func (u *User) private() {}
func helper() {}
`)
	writeRepoFile(t, repo, "store/store_test.go", "package store\nfunc TestOnlyHere() {}\n")
	writeRepoFile(t, repo, "vendor/x/x.go", "package x\nfunc Vendored() {}\n")
	writeRepoFile(t, repo, ".gophermind/run/x.go", "package x\nfunc Hidden() {}\n")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := probeFacts(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !f.RepoExists || !f.GitRepo || f.Module != "example.com/acme" || f.GoVersion != "1.22.3" {
		t.Errorf("facts = %+v", f)
	}
	if len(f.Packages) != 1 || f.Packages[0].Dir != "store" {
		t.Fatalf("packages = %+v", f.Packages)
	}
	if want := []string{"Limit", "New", "User", "User.Name"}; !reflect.DeepEqual(f.Packages[0].Exports, want) {
		t.Errorf("exports = %v, want %v", f.Packages[0].Exports, want)
	}
	raw, _ := factsJSON(f)
	if strings.Contains(raw, probeCanary) || strings.Contains(factsPrompt(f), probeCanary) {
		t.Error("text from a comment, a string literal or go.mod's comment reached the facts")
	}
}

func factsJSON(f factsFile) (string, error) {
	b, err := marshalIndent(f)
	return string(b), err
}

func TestProbeOfAMissingRepoIsNotAnError(t *testing.T) {
	f, err := probeFacts(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil || f.RepoExists || f.OS == "" || f.Arch == "" {
		t.Fatalf("%+v, %v", f, err)
	}
}

func TestProbeListsDeclaredNamesNeverValues(t *testing.T) {
	b := &brief.Brief{Front: brief.Frontmatter{
		Secrets: []brief.Secret{{Name: "B_TOKEN", Purpose: probeCanary}, {Name: "A_KEY"}},
		Network: []brief.Network{{Host: "api.example.com", Purpose: probeCanary}},
	}}
	f, _ := probeFacts(t.TempDir(), b)
	if !reflect.DeepEqual(f.SecretNames, []string{"A_KEY", "B_TOKEN"}) || !reflect.DeepEqual(f.Hosts, []string{"api.example.com"}) {
		t.Errorf("facts = %+v", f)
	}
	if strings.Contains(factsPrompt(f), probeCanary) {
		t.Error("a purpose text reached the facts")
	}
}

func TestProbeSkipsSymlinksAndOversizedFiles(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	writeRepoFile(t, outside, "out.go", "package out\nfunc Outside() {}\n")
	if err := os.Symlink(filepath.Join(outside, "out.go"), filepath.Join(repo, "link.go")); err != nil {
		t.Skip("symlinks are not available")
	}
	writeRepoFile(t, repo, "big.go", "package big\nfunc Big() {}\n"+strings.Repeat("// pad\n", probeMaxFileSize/7+10))
	f, _ := probeFacts(repo, nil)
	if len(f.Packages) != 0 {
		t.Errorf("a symlink or an oversized file was read: %+v", f.Packages)
	}
}

func TestFactTextAnswersFromTheKey(t *testing.T) {
	f := factsFile{RepoExists: true, GitRepo: true, Module: "example.com/acme", GoVersion: "1.22", OS: "linux", Arch: "arm64",
		Packages: []factPackage{{Dir: "a"}, {Dir: "b"}}, SecretNames: []string{"K"}}
	for key, want := range map[string]string{"go_module": "example.com/acme", "go_version": "1.22", "repo_is_git": "yes", "os": "linux", "arch": "arm64",
		"packages": "a, b", "secret_names": "K", "hosts": "none declared"} {
		if got, ok := factText(f, key); !ok || got != want {
			t.Errorf("factText(%q) = %q, %v; want %q", key, got, ok, want)
		}
	}
	if _, ok := factText(factsFile{}, "go_module"); ok {
		t.Error("no go.mod means go_module cannot be answered")
	}
	if _, ok := factText(f, "unknown"); ok {
		t.Error("an unknown key cannot be answered")
	}
}

func TestAnswerFactsSettlesOnlyAnswerableFactsWhosePrerequisitesAreSettled(t *testing.T) {
	s := qstore{Questions: []qrec{
		{ID: "q1", Kind: "fact", FactKey: "go_module", Status: qOpen},
		{ID: "q2", Kind: "fact", FactKey: "go_version", Status: qOpen}, // no go version in the facts
		{ID: "q3", Kind: "decision", Status: qOpen},
		{ID: "q4", Kind: "fact", FactKey: "os", Status: qOpen, DependsOn: []string{"q3"}},
	}}
	got := s.answerFacts(factsFile{Module: "example.com/acme", OS: "linux"}, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if !reflect.DeepEqual(got, []string{"q1"}) {
		t.Fatalf("settled %v, want [q1]", got)
	}
	if q := s.get("q1"); q.Answer != "example.com/acme" || q.AnsweredBy != byProbe {
		t.Errorf("q1 = %+v", q)
	}
	if s.get("q2").Status != qOpen || s.get("q4").Status != qOpen {
		t.Error("an unanswerable fact, or a fact waiting on an open decision, must stay open")
	}
}

func TestEnsureFactsIsStableAcrossResumes(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "go.mod", "module example.com/one\n\ngo 1.22\n")
	r := &run{dir: t.TempDir(), repo: repo}
	p := &Planner{}
	first, err := p.ensureFacts(r)
	if err != nil || first.Module != "example.com/one" {
		t.Fatalf("%+v, %v", first, err)
	}
	writeRepoFile(t, repo, "go.mod", "module example.com/two\n\ngo 1.23\n")
	second, err := p.ensureFacts(r)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("second = %+v, %v; want %+v", second, err, first)
	}
	loaded, err := loadFacts(r)
	if err != nil || !reflect.DeepEqual(first, loaded) {
		t.Errorf("loadFacts = %+v, %v", loaded, err)
	}
	fi, err := os.Stat(r.path(fileFacts))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("facts.json mode = %v, %v", fi, err)
	}
}

func TestEnsureFactsDoesNotReprobeAfterCorruption(t *testing.T) {
	r := &run{dir: t.TempDir(), repo: t.TempDir()}
	writeRepoFile(t, r.dir, fileFacts, "{not json")
	if _, err := (&Planner{}).ensureFacts(r); err == nil {
		t.Fatal("a corrupt facts.json must be an error")
	}
	raw, _ := os.ReadFile(r.path(fileFacts))
	if string(raw) != "{not json" {
		t.Errorf("facts.json was overwritten: %q", raw)
	}
}

func TestProbeIgnoresSymlinkedGoModAndGit(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	writeRepoFile(t, outside, "go.mod", "module example.com/outside\n\ngo 1.22\n")
	if err := os.Symlink(filepath.Join(outside, "go.mod"), filepath.Join(repo, "go.mod")); err != nil {
		t.Skip("symlinks are not available")
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".git")); err != nil {
		t.Skip("symlinks are not available")
	}
	f, _ := probeFacts(repo, nil)
	if f.Module != "" || f.GoVersion != "" || f.GitRepo {
		t.Errorf("a symlink out of the repo was followed: %+v", f)
	}
}

func TestProbeLimitsAreEnforced(t *testing.T) {
	old1, old2, old3, old4 := probeMaxFiles, probeMaxPackages, probeMaxExports, probeMaxEntries
	defer func() { probeMaxFiles, probeMaxPackages, probeMaxExports, probeMaxEntries = old1, old2, old3, old4 }()

	repo := t.TempDir()
	for _, d := range []string{"a", "b", "c", "d"} {
		writeRepoFile(t, repo, d+"/x.go", "package x\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")
	}
	probeMaxFiles, probeMaxPackages, probeMaxExports, probeMaxEntries = 400, 2, 2, 100000
	f, _ := probeFacts(repo, nil)
	if len(f.Packages) != 2 {
		t.Errorf("probeMaxPackages: %d packages", len(f.Packages))
	}
	for _, p := range f.Packages {
		if len(p.Exports) != 2 {
			t.Errorf("probeMaxExports: %+v", p)
		}
	}
	probeMaxFiles, probeMaxPackages, probeMaxExports = 2, 200, 50
	f, _ = probeFacts(repo, nil)
	if len(f.Packages) != 2 || !f.Truncated {
		t.Errorf("probeMaxFiles: %+v", f)
	}
	probeMaxFiles, probeMaxEntries = 400, 3
	f, _ = probeFacts(repo, nil)
	if !f.Truncated || !strings.Contains(factsPrompt(f), "truncated") {
		t.Errorf("probeMaxEntries: %+v", f)
	}
	probeMaxEntries = 100000
	f, _ = probeFacts(repo, nil)
	if f.Truncated || strings.Contains(factsPrompt(f), "truncated") {
		t.Errorf("an untruncated probe says it is truncated: %+v", f)
	}
}
