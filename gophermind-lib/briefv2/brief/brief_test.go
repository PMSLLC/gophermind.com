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

const venturePath = "../testdata/ai-venture-studio-server-brief.md"

func TestParseExampleEnvBlock(t *testing.T) {
	b, err := brief.Parse(loadExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Front.Env) != 2 {
		t.Fatalf("env = %+v", b.Front.Env)
	}
	crm, listen := b.Front.Env[0], b.Front.Env[1]
	if crm.Name != "CRM_BASE_URL" || crm.Default == nil || *crm.Default != "https://api.crm.example.com" {
		t.Errorf("CRM_BASE_URL = %+v", crm)
	}
	if listen.Name != "LISTEN_ADDR" || listen.Default == nil || *listen.Default != ":8080" {
		t.Errorf("LISTEN_ADDR = %+v", listen)
	}
}

func TestEnvSecretOverlapRejected(t *testing.T) {
	ex := string(loadExample(t))
	both := strings.Replace(ex, "env:\n", "env:\n  - name: CRM_API_KEY\n    purpose: same name as a secret\n", 1)
	_, err := brief.Parse([]byte(both))
	if err == nil || !strings.Contains(err.Error(), "ENV_SECRET_OVERLAP: CRM_API_KEY") {
		t.Fatalf("want ENV_SECRET_OVERLAP: CRM_API_KEY, got %v", err)
	}
	if _, ok := err.(*brief.InvalidError); !ok {
		t.Errorf("want *InvalidError (exit 2), got %T", err)
	}
}

func TestExampleBriefHasNoScanWarnings(t *testing.T) {
	b, err := brief.Parse(loadExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if w := b.UndeclaredSecrets(); len(w) != 0 {
		t.Errorf("example brief should be quiet, got %+v", w)
	}
}

func TestScanRule(t *testing.T) {
	ex := string(loadExample(t))
	with := func(line string) *brief.Brief {
		t.Helper()
		b, err := brief.Parse([]byte(strings.Replace(ex, "## Overview\n", "## Overview\n\n"+line+"\n", 1)))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tokens := func(b *brief.Brief) string {
		var out []string
		for _, w := range b.UndeclaredSecrets() {
			out = append(out, w.Token)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	cases := []struct{ name, line, want string }{
		{"secret suffix flagged", "Mail goes through SENDGRID_KEY.", "SENDGRID_KEY"},
		{"every suffix", "A_KEY B_SECRET C_TOKEN D_URL E_DSN F_PASSWORD G_PASSPHRASE", "A_KEY B_SECRET C_TOKEN D_URL E_DSN F_PASSWORD G_PASSPHRASE"},
		{"ordinary words are quiet", "Stored as JSONB, updated with PUT, returns TOKEN_REUSED.", ""},
		{"wording trigger flags any token on the line", "Read the credential from FOO_BAR.", "FOO_BAR"},
		{"wording is case insensitive", "The Environment Variable BAZ_QUX holds it.", "BAZ_QUX"},
		{"secret word trigger", "This is a Secret named ZED_ONE.", "ZED_ONE"},
		{"declared env name is exempt even with a secret suffix", "Calls $CRM_BASE_URL/v1/contacts.", ""},
		{"declared secret name is exempt", "Uses CRM_API_KEY for auth; the credential is CRM_API_KEY.", ""},
		{"duplicate token on one line warns once", "SENDGRID_KEY and SENDGRID_KEY again.", "SENDGRID_KEY"},
		{"bare suffix is not a token", "The _KEY suffix and KEY alone are fine.", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokens(with(c.line)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestWarningLineNumbersUnderCRLF(t *testing.T) {
	ex := string(loadExample(t))
	src := strings.Replace(ex, "## Overview\n", "## Overview\n\nUses SENDGRID_KEY here.\n", 1)
	crlf := "\ufeff" + strings.ReplaceAll(src, "\n", "\r\n")
	b, err := brief.Parse([]byte(crlf))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(src, "\n")
	ws := b.UndeclaredSecrets()
	if len(ws) != 1 || !strings.Contains(lines[ws[0].Line-1], "SENDGRID_KEY") {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestVentureStudioBrief(t *testing.T) {
	src, err := os.ReadFile(venturePath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := brief.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if b.Front.ID != "gm-2026-09-29-002" || len(b.Features) != 19 {
		t.Errorf("id=%s features=%d", b.Front.ID, len(b.Features))
	}
	names := func(n int, get func(i int) string) string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, get(i))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	if got := names(len(b.Front.Secrets), func(i int) string { return b.Front.Secrets[i].Name }); got != "DATABASE_URL JWT_SIGNING_KEY STUDIO_LLM_API_KEY TEST_DATABASE_URL" {
		t.Errorf("secrets = %s", got)
	}
	if got := names(len(b.Front.Env), func(i int) string { return b.Front.Env[i].Name }); got != "DATA_DIR LISTEN_ADDR LOG_LEVEL STUDIO_FAKE_NOW STUDIO_LLM_BASE_URL STUDIO_LLM_MODEL" {
		t.Errorf("env = %s", got)
	}
	// The handoff sets a ceiling of 3 warnings on this brief.
	if w := b.UndeclaredSecrets(); len(w) > 3 {
		t.Errorf("%d warnings (ceiling 3): %+v", len(w), w)
	}
}
