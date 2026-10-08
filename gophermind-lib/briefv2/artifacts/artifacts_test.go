package artifacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/artifacts"
)

const canary = "s3cr3t-canary-value-9f2"

func scrubCanary(s string) string {
	if strings.Contains(s, canary) {
		return "(redacted)"
	}
	return s
}

func meta() artifacts.Meta {
	return artifacts.Meta{Provider: "mini", Model: "qwen", Revision: 0, Order: 1, Verdict: "fail", Class: "build", StartedAt: time.Unix(1, 0).UTC()}
}

func TestSaveWritesTheFourFilesAndNumbersAttempts(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 1<<20, scrubCanary)
	dir1, err := w.Save("fn-a", meta(), artifacts.Files{Source: []byte("package p\n"), Raw: "```go\npackage p\n```", CheckOutput: "build failed"})
	if err != nil {
		t.Fatal(err)
	}
	dir2, err := w.Save("fn-a", meta(), artifacts.Files{Raw: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if dir1 != filepath.Join(run, "attempts", "fn-a", "1") || dir2 != filepath.Join(run, "attempts", "fn-a", "2") {
		t.Fatalf("dirs = %s, %s", dir1, dir2)
	}
	for name, want := range map[string]string{"reply.go": "package p\n", "reply.txt": "```go\npackage p\n```", "check-output.txt": "build failed"} {
		b, err := os.ReadFile(filepath.Join(dir1, name))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", name, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir1, "attempt.json")); err != nil {
		t.Errorf("attempt.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, "reply.go")); !os.IsNotExist(err) {
		t.Errorf("an empty Source must not create reply.go: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir1, "reply.go")); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSaveNeverWritesASecret(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 1<<20, scrubCanary)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{
		Source: []byte("var k = \"" + canary + "\""), Raw: "token " + canary, CheckOutput: "got " + canary,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(run, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), canary) {
			t.Errorf("%s holds the canary secret", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
	if string(b) != "(redacted)" {
		t.Errorf("a file that held a secret must be replaced whole by the redaction mark, got %q", b)
	}
}

func TestSaveCapsEachFileAndMarksTheCut(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 100, nil)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{CheckOutput: strings.Repeat("é", 200)})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "check-output.txt"))
	if !strings.Contains(string(b), "[truncated:") || len(b) > 100+64 {
		t.Errorf("capped file = %d bytes: %q", len(b), b)
	}
	if !strings.HasPrefix(string(b), "éé") {
		t.Errorf("the cut must fall on a rune boundary: %q", b[:8])
	}
}

func TestSaveCapDoesNotHideASecretAcrossTheCut(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 12, scrubCanary)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{CheckOutput: "0123456789" + canary})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "check-output.txt"))
	if strings.Contains(string(b), canary[:6]) {
		t.Errorf("a secret straddling the cut leaked its start: %q", b)
	}
}

func TestSaveOffAndNilWriteNothing(t *testing.T) {
	run := t.TempDir()
	for _, w := range []*artifacts.Writer{nil, artifacts.New(run, false, 100, nil)} {
		dir, err := w.Save("fn-a", meta(), artifacts.Files{Raw: "x"})
		if err != nil || dir != "" {
			t.Errorf("Save = %q, %v; want nothing", dir, err)
		}
	}
	if ents, _ := os.ReadDir(run); len(ents) != 0 {
		t.Errorf("an off writer created %d entries", len(ents))
	}
}

func TestSaveRefusesAnUnsafeNodeID(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 100, nil)
	for _, id := range []string{"../x", "/abs", "a/b", "", "UP"} {
		if _, err := w.Save(id, meta(), artifacts.Files{Raw: "x"}); err == nil {
			t.Errorf("Save accepted node id %q", id)
		}
	}
	if ents, _ := os.ReadDir(filepath.Dir(run)); len(ents) > 1 {
		for _, e := range ents {
			if e.Name() == "x" {
				t.Error("a node id escaped the run folder")
			}
		}
	}
}
