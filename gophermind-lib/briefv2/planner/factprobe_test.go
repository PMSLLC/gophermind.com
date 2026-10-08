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
