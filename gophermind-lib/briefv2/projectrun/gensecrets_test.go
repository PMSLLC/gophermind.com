package projectrun

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/vault"
)

type memStore struct {
	m       map[string]string
	failSet *testing.T
}

func newMem() *memStore { return &memStore{m: map[string]string{}} }

func (s *memStore) Get(scope, name string) (string, bool) {
	v, ok := s.m[scope+"|"+name]
	return v, ok
}

func (s *memStore) Set(scope, name, value string) error {
	if s.failSet != nil {
		s.failSet.Errorf("Set called: %s %s", scope, name)
	}
	s.m[scope+"|"+name] = value
	return nil
}

func briefWith(names ...string) *brief.Brief {
	b := &brief.Brief{}
	b.Front.ID = "p1"
	for _, n := range names {
		b.Front.Secrets = append(b.Front.Secrets, brief.Secret{Name: n})
	}
	return b
}

// seqReader yields a deterministic byte stream.
type seqReader struct{ n byte }

func (r *seqReader) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestGenerateShapes(t *testing.T) {
	h, err := Generate("hex32", &seqReader{})
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(h) {
		t.Fatalf("hex32 = %q, %v", h, err)
	}
	p, err := Generate("placeholder", &seqReader{})
	if err != nil || !regexp.MustCompile(`^gm-placeholder-[0-9a-f]{32}$`).MatchString(p) {
		t.Fatalf("placeholder = %q, %v", p, err)
	}
	if _, err := Generate("nope", &seqReader{}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := Generate("hex32", failReader{}); err == nil {
		t.Fatal("failing reader accepted")
	}
	if _, err := Generate("hex32", bytes.NewReader(make([]byte, 5))); err == nil {
		t.Fatal("short read accepted")
	}
	a, _ := Generate("hex32", nil)
	b, _ := Generate("hex32", nil)
	if a == "" || a == b {
		t.Fatal("nil reader must use crypto/rand and differ")
	}
}

func TestGeneratedWrittenToHarness(t *testing.T) {
	s := newMem()
	b := briefWith("A", "B", "C")
	got, err := ProvisionGenerated(s, b, map[string]string{"A": "hex32", "B": "placeholder"}, false, &seqReader{})
	if err != nil {
		t.Fatal(err)
	}
	want := []Provisioned{{"A", SourceHex32}, {"B", SourcePlaceholder}, {"C", SourceMissing}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if v, ok := s.Get(vault.HarnessScope, "A"); !ok || len(v) != 64 {
		t.Fatalf("A not stored: %q", v)
	}
	if v, ok := s.Get(vault.HarnessScope, "B"); !ok || !strings.HasPrefix(v, "gm-placeholder-") {
		t.Fatalf("B not stored")
	}
	if _, ok := s.Get(vault.HarnessScope, "C"); ok {
		t.Fatal("C must not be stored")
	}
}

func TestVaultBeatsGenerated(t *testing.T) {
	s := newMem()
	s.m[vault.HarnessScope+"|A"] = "real"
	got, err := ProvisionGenerated(s, briefWith("A"), map[string]string{"A": "hex32"}, false, &seqReader{})
	if err != nil || len(got) != 1 || got[0].Source != SourceVault {
		t.Fatalf("got %v, %v", got, err)
	}
	if v, _ := s.Get(vault.HarnessScope, "A"); v != "real" {
		t.Fatalf("harness value replaced: %q", v)
	}
}

func TestRunScopeOverwrittenOnFreshRun(t *testing.T) {
	s := newMem()
	run := vault.RunScope("p1")
	s.m[vault.HarnessScope+"|A"] = "new"
	s.m[run+"|A"] = "stale"
	s.m[vault.HarnessScope+"|B"] = "b"
	if _, err := ProvisionGenerated(s, briefWith("A", "B"), nil, false, &seqReader{}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(run, "A"); v != "new" {
		t.Fatalf("run scope not refreshed: %q", v)
	}
	if _, ok := s.Get(run, "B"); ok {
		t.Fatal("absent run value must be left for storeSecrets")
	}
}

func TestRunScopeKeptOnResume(t *testing.T) {
	s := newMem()
	run := vault.RunScope("p1")
	s.m[vault.HarnessScope+"|A"] = "new"
	s.m[run+"|A"] = "old"
	got, err := ProvisionGenerated(s, briefWith("A", "G"), map[string]string{"G": "hex32"}, true, &seqReader{})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(run, "A"); v != "old" {
		t.Fatalf("run scope touched on resume: %q", v)
	}
	if v, _ := s.Get(vault.HarnessScope, "A"); v != "new" {
		t.Fatal("harness touched")
	}
	if _, ok := s.Get(vault.HarnessScope, "G"); !ok || got[1].Source != SourceHex32 {
		t.Fatal("missing harness value must still be generated on resume")
	}
}

func TestSourcesReadOnly(t *testing.T) {
	s := newMem()
	s.failSet = t
	s.m[vault.HarnessScope+"|V"] = "x"
	got := Sources(s, briefWith("V", "G", "P", "M"), map[string]string{"G": "hex32"}, true)
	want := []Provisioned{{"V", SourceVault}, {"G", SourceHex32}, {"P", SourcePrompt}, {"M", SourcePrompt}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = Sources(s, briefWith("M"), nil, false)
	if got[0].Source != SourceMissing {
		t.Fatalf("got %v", got)
	}
	got = Sources(nil, briefWith("M"), map[string]string{"M": "placeholder"}, false)
	if got[0].Source != SourcePlaceholder {
		t.Fatalf("got %v", got)
	}
}

func TestGeneratedValueNeverPrinted(t *testing.T) {
	canary, err := Generate("hex32", &seqReader{})
	if err != nil {
		t.Fatal(err)
	}
	s := newMem()
	got, err := ProvisionGenerated(s, briefWith("A"), map[string]string{"A": "hex32"}, false, &seqReader{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(got), canary) || strings.Contains(fmt.Sprintf("%+v %#v", got, got), canary) {
		t.Fatal("generated value in returned list")
	}
	// A store that fails on Set: the error must not carry the value.
	fs := &failStore{memStore: newMem()}
	_, err = ProvisionGenerated(fs, briefWith("A"), map[string]string{"A": "hex32"}, false, &seqReader{})
	if err == nil || strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "A") {
		t.Fatalf("bad error: %v", err)
	}
	// Unknown kind and failing reader errors.
	for _, rd := range []io.Reader{failReader{}, &seqReader{}} {
		kind := "hex32"
		if _, ok := rd.(*seqReader); ok {
			kind = "bogus"
		}
		_, err = ProvisionGenerated(newMem(), briefWith("A"), map[string]string{"A": kind}, false, rd)
		if err == nil || strings.Contains(err.Error(), canary) {
			t.Fatalf("bad error: %v", err)
		}
	}
}

type failStore struct{ *memStore }

func (f *failStore) Set(scope, name, value string) error {
	return fmt.Errorf("write refused for %s", value)
}

func TestProvisionNilStore(t *testing.T) {
	if got, err := ProvisionGenerated(nil, briefWith(), nil, false, nil); err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ProvisionGenerated(nil, briefWith("A"), nil, false, nil); err == nil {
		t.Fatal("nil store with declared secrets must error")
	}
}
