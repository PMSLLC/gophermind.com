package brief_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
)

const examplePath = "../testdata/example/brief.md"

func loadExample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseExample(t *testing.T) {
	b, err := brief.Parse(loadExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if b.Front.ID != "gm-2026-09-29-001" || b.Front.Language != "go" || b.Front.Landing != "commit" {
		t.Fatalf("frontmatter: %+v", b.Front)
	}
	if got := b.Front.WorkBranchName(); got != "gm/gm-2026-09-29-001" {
		t.Errorf("work branch = %q", got)
	}
	for _, s := range []string{"Overview", "Features", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"} {
		if strings.TrimSpace(b.Sections[s]) == "" {
			t.Errorf("section %q empty", s)
		}
	}
	if len(b.Features) == 0 || b.Features[0].Name != "Registration" {
		t.Errorf("features: %+v", b.Features)
	}
	if len(b.Front.Secrets) != 1 || b.Front.Secrets[0].Name != "CRM_API_KEY" {
		t.Errorf("secrets: %+v", b.Front.Secrets)
	}
}

func TestParseRejections(t *testing.T) {
	ex := string(loadExample(t))
	cases := map[string]struct {
		src  string
		want string
	}{
		"missing spec_version": {strings.Replace(ex, "spec_version: \"2.0\"\n", "", 1), "spec_version"},
		"python":               {strings.Replace(ex, "language: go", "language: python", 1), "language"},
		"no frontmatter":       {"# just a title\n", "frontmatter"},
		"unterminated":         {"---\nid: x\n", "frontmatter"},
		"missing section":      {strings.Replace(ex, "## Data", "## Datum", 1), "Data"},
		"empty section":        {strings.Replace(ex, "## Out of scope", "## Out of scope\n\n## Zzz", 1), "Out of scope"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := brief.Parse([]byte(c.src))
			var inv *brief.InvalidError
			if err == nil {
				t.Fatal("expected an error")
			}
			if !asInvalid(err, &inv) {
				t.Fatalf("want *InvalidError, got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func asInvalid(err error, target **brief.InvalidError) bool {
	e, ok := err.(*brief.InvalidError)
	if ok {
		*target = e
	}
	return ok
}

func TestCRLFAndBOMParseTheSame(t *testing.T) {
	ex := string(loadExample(t))
	crlf := string([]byte{0xef, 0xbb, 0xbf}) + strings.ReplaceAll(ex, "\n", "\r\n")
	a, err := brief.Parse([]byte(ex))
	if err != nil {
		t.Fatal(err)
	}
	b, err := brief.Parse([]byte(crlf))
	if err != nil {
		t.Fatalf("CRLF+BOM: %v", err)
	}
	if a.Front.ID != b.Front.ID || a.Sections["Overview"] != b.Sections["Overview"] {
		t.Error("CRLF/BOM brief parsed differently")
	}
}

func TestHeadingInsideFenceIsNotASection(t *testing.T) {
	ex := string(loadExample(t))
	fenced := strings.Replace(ex, "## Data", "## Data\n\n```\n## Not A Section\n```\n", 1)
	b, err := brief.Parse([]byte(fenced))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Sections["Not A Section"]; ok {
		t.Error("fenced heading became a section")
	}
	if !strings.Contains(b.Sections["Data"], "## Not A Section") {
		t.Error("fenced text should stay inside Data")
	}
}

func TestDuplicateSectionRejected(t *testing.T) {
	ex := string(loadExample(t))
	dup := ex + "\n## Overview\n\nagain\n"
	if _, err := brief.Parse([]byte(dup)); err == nil || !strings.Contains(err.Error(), "Overview") {
		t.Fatalf("want a duplicate-section error naming Overview, got %v", err)
	}
}

func TestUndeclaredSecretWarnings(t *testing.T) {
	src := loadExample(t)
	b, err := brief.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	for _, w := range b.UndeclaredSecrets() {
		tokens = append(tokens, w.Token)
		if !strings.Contains(lines[w.Line-1], w.Token) {
			t.Errorf("warning line %d does not contain %s", w.Line, w.Token)
		}
	}
	sort.Strings(tokens)
	want := "EMAIL_INVALID EMAIL_REQUIRED EMAIL_TAKEN USERNAME_CHARS USERNAME_LENGTH"
	if got := strings.Join(tokens, " "); got != want {
		t.Errorf("warnings = %q, want %q (CRM_API_KEY is declared and must not warn)", got, want)
	}

	extra := strings.Replace(string(src), "## Overview\n", "## Overview\n\nUses SENDGRID_KEY for mail.\n", 1)
	b2, err := brief.Parse([]byte(extra))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range b2.UndeclaredSecrets() {
		found = found || w.Token == "SENDGRID_KEY"
	}
	if !found {
		t.Error("undeclared SENDGRID_KEY should warn")
	}
}
