package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jbrahy/bubblecomplete"
	"github.com/jbrahy/bubblecomplete/ngram"
)

// candidatesEqual compares the fields that matter for completion rendering:
// Text (the tail to insert), Display (menu label), and Replace. Desc is
// intentionally not asserted everywhere since it's descriptive only.
func candidatesEqual(t *testing.T, got, want []bubblecomplete.Candidate) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].Text != want[i].Text || got[i].Display != want[i].Display || got[i].Replace != want[i].Replace {
			t.Errorf("candidate[%d] = %#v, want Text=%q Display=%q Replace=%d",
				i, got[i], want[i].Text, want[i].Display, want[i].Replace)
		}
	}
}

func TestCommandProviderSingleMatchGhost(t *testing.T) {
	p := newCommandProvider()
	input := "/pr"
	got := p.Suggest(input, len(input))
	want := []bubblecomplete.Candidate{
		{Text: "oject", Display: "/project <name>", Desc: "start the guided new-project flow", Replace: 0},
	}
	candidatesEqual(t, got, want)
	if got[0].Desc != want[0].Desc {
		t.Errorf("Desc = %q, want %q", got[0].Desc, want[0].Desc)
	}
}

func TestCommandProviderMenuOnMultipleMatches(t *testing.T) {
	p := newCommandProvider()
	input := "/p"
	got := p.Suggest(input, len(input))
	// "/project" and "/phase" both start with "/p".
	want := []bubblecomplete.Candidate{
		{Text: "roject", Display: "/project <name>", Replace: 0},
		{Text: "hase", Display: "/phase <cmd>", Replace: 0},
	}
	candidatesEqual(t, got, want)
}

func TestCommandProviderRootSlashMenu(t *testing.T) {
	p := newCommandProvider()
	input := "/"
	got := p.Suggest(input, len(input))
	if len(got) != len(slashCommands) {
		t.Fatalf("len = %d, want %d (all registry entries)", len(got), len(slashCommands))
	}
}

func TestCommandProviderExactFullMatchReturnsNil(t *testing.T) {
	p := newCommandProvider()
	input := "/help"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (exact match, nothing to add)", input, got)
	}
}

func TestCommandProviderInactiveWithSpace(t *testing.T) {
	p := newCommandProvider()
	input := "/temp 0.5"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (space means args, not command name)", input, got)
	}
}

func TestCommandProviderInactiveWithoutSlash(t *testing.T) {
	p := newCommandProvider()
	input := "hello"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (not a command)", input, got)
	}
}

func TestFileProviderListsDirEntries(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "internal"))
	mustWriteFile(t, filepath.Join(dir, "internal", "alpha.go"), "")
	mustWriteFile(t, filepath.Join(dir, "internal", "beta.go"), "")
	mustWriteFile(t, filepath.Join(dir, "main.go"), "")
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "")

	withWorkdir(t, dir, func() {
		p := newFileProvider()

		// Trailing-slash token lists all (non-dotfile) entries in that dir.
		input := "internal/"
		got := p.Suggest(input, len(input))
		want := []bubblecomplete.Candidate{
			{Text: "alpha.go", Display: "alpha.go", Replace: 0},
			{Text: "beta.go", Display: "beta.go", Replace: 0},
		}
		candidatesEqual(t, got, want)
	})
}

func TestFileProviderSingleMatchGhost(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "internal"))
	mustWriteFile(t, filepath.Join(dir, "internal", "alpha.go"), "")
	mustWriteFile(t, filepath.Join(dir, "internal", "beta.go"), "")

	withWorkdir(t, dir, func() {
		p := newFileProvider()
		input := "internal/al"
		got := p.Suggest(input, len(input))
		want := []bubblecomplete.Candidate{
			{Text: "pha.go", Display: "alpha.go", Replace: 0},
		}
		candidatesEqual(t, got, want)
	})
}

func TestFileProviderBareWordReturnsNil(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "internal"))

	withWorkdir(t, dir, func() {
		p := newFileProvider()
		input := "intern"
		got := p.Suggest(input, len(input))
		if got != nil {
			t.Fatalf("Suggest(%q) = %#v, want nil (bare word, no path separator)", input, got)
		}
	})
}

func TestFileProviderDotSlashListsCwdWithDirTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "internal"))
	mustWriteFile(t, filepath.Join(dir, "main.go"), "")
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "")

	withWorkdir(t, dir, func() {
		p := newFileProvider()
		input := "./"
		got := p.Suggest(input, len(input))
		want := []bubblecomplete.Candidate{
			{Text: "internal/", Display: "internal/", Replace: 0},
			{Text: "main.go", Display: "main.go", Replace: 0},
		}
		candidatesEqual(t, got, want)
		// dotfile excluded
		for _, c := range got {
			if c.Display == ".hidden" {
				t.Errorf("dotfile .hidden should be excluded, got %#v", got)
			}
		}
	})
}

func TestFileProviderNoMatchReturnsNil(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "internal"))

	withWorkdir(t, dir, func() {
		p := newFileProvider()
		input := "internal/zzz"
		got := p.Suggest(input, len(input))
		if got != nil {
			t.Fatalf("Suggest(%q) = %#v, want nil (no matching entries)", input, got)
		}
	})
}

func TestFileProviderMissingDirReturnsNil(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		p := newFileProvider()
		input := "nosuchdir/frag"
		got := p.Suggest(input, len(input))
		if got != nil {
			t.Fatalf("Suggest(%q) = %#v, want nil (missing dir ignored)", input, got)
		}
	})
}

func TestRecallProviderMostRecentPrefixMatch(t *testing.T) {
	history := func() []string { return []string{"write tests", "write the file"} }
	p := newRecallProvider(history)
	input := "write t"
	got := p.Suggest(input, len(input))
	want := []bubblecomplete.Candidate{
		{Text: "he file", Display: "write the file", Replace: 0},
	}
	candidatesEqual(t, got, want)
}

func TestRecallProviderNoMatchReturnsNil(t *testing.T) {
	history := func() []string { return []string{"write tests", "write the file"} }
	p := newRecallProvider(history)
	input := "totally different"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (no prefix match)", input, got)
	}
}

func TestRecallProviderInactiveForCommand(t *testing.T) {
	history := func() []string { return []string{"write tests", "write the file"} }
	p := newRecallProvider(history)
	input := "/x"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (command input)", input, got)
	}
}

func TestRecallProviderEmptyInputReturnsNil(t *testing.T) {
	history := func() []string { return []string{"write tests"} }
	p := newRecallProvider(history)
	got := p.Suggest("", 0)
	if got != nil {
		t.Fatalf("Suggest(\"\") = %#v, want nil", got)
	}
}

func TestMarkovProviderNoTrailingSpaceAddsLeadingSpace(t *testing.T) {
	model := ngram.New()
	model.TrainAll([]string{"write the tests", "write the tests", "write the docs"})
	p := newMarkovProvider(model)

	input := "write the"
	got := p.Suggest(input, len(input))
	want := []bubblecomplete.Candidate{
		{Text: " tests", Display: " tests", Replace: 0},
	}
	candidatesEqual(t, got, want)
}

func TestMarkovProviderTrailingSpaceNoLeadingSpace(t *testing.T) {
	model := ngram.New()
	model.TrainAll([]string{"write the tests", "write the tests", "write the docs"})
	p := newMarkovProvider(model)

	input := "write the "
	got := p.Suggest(input, len(input))
	want := []bubblecomplete.Candidate{
		{Text: "tests", Display: "tests", Replace: 0},
	}
	candidatesEqual(t, got, want)
}

func TestMarkovProviderUnknownContextReturnsNil(t *testing.T) {
	model := ngram.New() // untrained: no data at any n-gram level
	p := newMarkovProvider(model)

	input := "anything at all"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (untrained model, no prediction)", input, got)
	}
}

func TestMarkovProviderInactiveForCommand(t *testing.T) {
	model := ngram.New()
	model.TrainAll([]string{"write the tests", "write the tests"})
	p := newMarkovProvider(model)

	input := "/x"
	got := p.Suggest(input, len(input))
	if got != nil {
		t.Fatalf("Suggest(%q) = %#v, want nil (command input)", input, got)
	}
}

func TestMarkovProviderEmptyInputReturnsNil(t *testing.T) {
	model := ngram.New()
	model.TrainAll([]string{"write the tests", "write the tests"})
	p := newMarkovProvider(model)

	got := p.Suggest("", 0)
	if got != nil {
		t.Fatalf("Suggest(\"\") = %#v, want nil", got)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
