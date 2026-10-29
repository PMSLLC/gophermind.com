package brief_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
)

const cleanAcceptance = "- `go build ./...` succeeds.\n- `go vet ./...` reports nothing.\n- `go test ./...` passes.\n- `printf 'a\\n' | tool -` prints `x`.\n"

// lintBrief is a small brief whose Features bullet, Architecture and
// Acceptance text are the parts a test varies.
func lintBrief(feature, arch, acceptance string) []byte {
	return []byte("---\nspec_version: \"2.0\"\nid: gm-2026-10-28-060\ntitle: t\nlanguage: go\nbase_branch: main\nlanding: commit\non_ambiguity: halt\n---\n\n" +
		"## Overview\n\no\n\n## Features\n\n### F\n\n- " + feature + "\n\n## Architecture\n\n" + arch + "\n\n## Data\n\nd\n\n## Constraints\n\n- c\n\n## Out of scope\n\n- o\n\n## Acceptance\n\n" + acceptance)
}

const cleanArch = "- `cmd/tool` (package `main`): wires it up. `Run` takes an `io.Reader`."

func TestLintIsEmptyForEveryExample(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(examplesDir, "[0-9][0-9]-*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := brief.Lint(raw); len(got) != 0 {
			t.Errorf("%s: %+v", filepath.Base(f), got)
		}
	}
}

func TestLintAcceptsACleanBrief(t *testing.T) {
	if got := brief.Lint(lintBrief("does a thing", cleanArch, cleanAcceptance)); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLintFlagsAcceptanceThatCannotRunTheBuiltBinary(t *testing.T) {
	for _, cmd := range []string{"go run . -", "go run ./cmd/tool", "go install ./...", "go build -o tool .", "./tool -", "printf 'a' | ./tool -"} {
		src := lintBrief("does a thing", cleanArch, cleanAcceptance+"- `"+cmd+"` prints `x`.\n")
		got := brief.Lint(src)
		if len(got) != 1 || !strings.Contains(got[0].Message, "built binary by name") {
			t.Errorf("%q: got %+v", cmd, got)
			continue
		}
		if line := strings.Count(string(src)[:strings.Index(string(src), "`"+cmd+"`")], "\n") + 1; got[0].Line != line {
			t.Errorf("%q: line %d, want %d", cmd, got[0].Line, line)
		}
	}
}

func TestLintFlagsMissingGoChecks(t *testing.T) {
	got := brief.Lint(lintBrief("does a thing", cleanArch, "- `go build ./...` succeeds.\n- `go test ./...` passes.\n- `printf 'a' | tool -` prints `x`.\n"))
	if len(got) != 1 || !strings.Contains(got[0].Message, "go vet ./...") {
		t.Fatalf("got %+v", got)
	}
}

func TestLintFlagsStdinWithoutAReader(t *testing.T) {
	arch := "- `cmd/tool` (package `main`): wires it up."
	got := brief.Lint(lintBrief("reads standard input and counts lines", arch, cleanAcceptance))
	if len(got) != 1 || !strings.Contains(got[0].Message, "io.Reader") {
		t.Fatalf("got %+v", got)
	}
	if got := brief.Lint(lintBrief("reads standard input and counts lines", cleanArch, cleanAcceptance)); len(got) != 0 {
		t.Errorf("with an io.Reader in Architecture: %+v", got)
	}
}

func TestLintFlagsABinaryWithNoCmdDirectory(t *testing.T) {
	got := brief.Lint(lintBrief("does a thing", "- `main` in the repository root.", cleanAcceptance))
	if len(got) != 1 || !strings.Contains(got[0].Message, "cmd/") {
		t.Fatalf("got %+v", got)
	}
	lib := "- `go build ./...` succeeds.\n- `go vet ./...` reports nothing.\n- `go test ./...` passes.\n"
	if got := brief.Lint(lintBrief("does a thing", "- `retry`: a library.", lib)); len(got) != 0 {
		t.Errorf("a library with no binary: %+v", got)
	}
}

func TestLintFlagsAVeryLongBrief(t *testing.T) {
	long := string(lintBrief("does a thing", cleanArch, cleanAcceptance)) + strings.Repeat("- filler\n", 420)
	got := brief.Lint([]byte(long))
	if len(got) != 1 || !strings.Contains(got[0].Message, "lines long") {
		t.Fatalf("got %+v", got)
	}
}

func TestLintIgnoresABriefItCannotSplit(t *testing.T) {
	if got := brief.Lint([]byte("# not a brief\n")); got != nil {
		t.Fatalf("got %+v, want nil: Check already reports that", got)
	}
}
