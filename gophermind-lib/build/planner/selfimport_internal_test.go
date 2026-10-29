package planner

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const selfImportSrc = `package cli

import (
	"bytes"
	"testing"

	cli "example.com/slugify/internal/cli"
)

func TestParseMax(t *testing.T) {
	var b bytes.Buffer
	v, err := cli.ParseMax("42", &b)
	if err != nil || v != 42 {
		t.Fatal(cli.Usage())
	}
}
`

// A test in package cli imported example.com/slugify/internal/cli, its own
// package. Go refuses that as an import cycle, so the build stopped before any
// function was written. The import and the qualifier it needed are removed.
func TestSelfImportIsRemovedFromASamePackageTest(t *testing.T) {
	got := dropSelfImport(selfImportSrc, "example.com/slugify/internal/cli")
	if strings.Contains(got, "example.com/slugify/internal/cli") {
		t.Errorf("the self import is still there:\n%s", got)
	}
	if strings.Contains(got, "cli.ParseMax") || strings.Contains(got, "cli.Usage") {
		t.Errorf("a qualifier of the removed import is still there:\n%s", got)
	}
	if !strings.Contains(got, "ParseMax(\"42\", &b)") || !strings.Contains(got, "t.Fatal(Usage())") {
		t.Errorf("the calls were not kept:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", got, parser.AllErrors); err != nil {
		t.Errorf("the result does not parse: %v\n%s", err, got)
	}
}

func TestSelfImportWithoutAnAliasAndOtherImportsAreHandled(t *testing.T) {
	src := "package cli\n\nimport (\n\t\"example.com/slugify/internal/cli\"\n\t\"example.com/slugify/internal/slug\"\n)\n\nfunc TestX() { _ = cli.A(slug.B()) }\n"
	got := dropSelfImport(src, "example.com/slugify/internal/cli")
	if strings.Contains(got, "internal/cli\"") || !strings.Contains(got, "internal/slug") || !strings.Contains(got, "A(slug.B())") {
		t.Errorf("got:\n%s", got)
	}
}

func TestExternalTestPackageAndNoSelfImportAreLeftAlone(t *testing.T) {
	ext := strings.Replace(selfImportSrc, "package cli\n", "package cli_test\n", 1)
	if got := dropSelfImport(ext, "example.com/slugify/internal/cli"); got != ext {
		t.Errorf("an external test package was changed:\n%s", got)
	}
	plain := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"
	if got := dropSelfImport(plain, "example.com/slugify/internal/cli"); got != plain {
		t.Errorf("a file with no self import was changed:\n%s", got)
	}
	if got := dropSelfImport("not go", "example.com/slugify/internal/cli"); got != "not go" {
		t.Errorf("text that is not Go was changed: %q", got)
	}
}
