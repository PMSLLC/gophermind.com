package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/vault"
)

const exampleDir = "../../gophermind-lib/briefv2/testdata/example"

func runBriefCmd(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, stdin)
	_ = w.Close()
	var out, errb bytes.Buffer
	code := runBrief(args, r, &out, &errb)
	return code, out.String(), errb.String()
}

func TestBriefValidate(t *testing.T) {
	code, out, errs := runBriefCmd(t, "", "validate", filepath.Join(exampleDir, "brief.md"))
	if code != 0 || !strings.Contains(out, "gm-2026-09-29-001") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if strings.Contains(errs, "warning") {
		t.Errorf("example brief should produce no warnings, got %q", errs)
	}
}

func TestBriefValidateVentureStudioBriefWithinWarningCeiling(t *testing.T) {
	code, out, errs := runBriefCmd(t, "", "validate", "../../gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md")
	if code != 0 || !strings.Contains(out, "gm-2026-09-29-002") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if n := strings.Count(errs, "warning:"); n > 3 {
		t.Errorf("%d warnings (ceiling 3): %q", n, errs)
	}
}

func TestBriefValidateEnvSecretOverlapExitsTwo(t *testing.T) {
	src, _ := os.ReadFile(filepath.Join(exampleDir, "brief.md"))
	p := filepath.Join(t.TempDir(), "overlap.md")
	body := strings.Replace(string(src), "env:\n", "env:\n  - name: CRM_API_KEY\n    purpose: dup\n", 1)
	_ = os.WriteFile(p, []byte(body), 0o600)
	code, _, errs := runBriefCmd(t, "", "validate", p)
	if code != 2 || !strings.Contains(errs, "ENV_SECRET_OVERLAP: CRM_API_KEY") {
		t.Errorf("code=%d err=%q", code, errs)
	}
}

func TestBriefValidateInvalidExitsTwoNamingTheField(t *testing.T) {
	src, _ := os.ReadFile(filepath.Join(exampleDir, "brief.md"))
	dir := t.TempDir()
	cases := map[string]string{
		"spec_version": strings.Replace(string(src), "spec_version: \"2.0\"\n", "", 1),
		"language":     strings.Replace(string(src), "language: go", "language: python", 1),
	}
	for field, body := range cases {
		p := filepath.Join(dir, field+".md")
		_ = os.WriteFile(p, []byte(body), 0o600)
		code, _, errs := runBriefCmd(t, "", "validate", p)
		if code != 2 || !strings.Contains(errs, field) {
			t.Errorf("%s: code=%d err=%q", field, code, errs)
		}
	}
}

func TestBriefValidateMissingFileExitsOne(t *testing.T) {
	if code, _, _ := runBriefCmd(t, "", "validate", filepath.Join(t.TempDir(), "nope.md")); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestBriefVaultSetAndList(t *testing.T) {
	old := vaultOptions
	vaultOptions = vault.Options{WorkFactor: 10}
	t.Cleanup(func() { vaultOptions = old })
	p := filepath.Join(t.TempDir(), "v.age")
	t.Setenv("GOPHERMIND_VAULT_PATH", p)
	t.Setenv(vault.PassphraseEnv, "pw")

	code, _, errs := runBriefCmd(t, "canary-9f8e7d\n", "vault", "set", "CANARY")
	if code != 0 {
		t.Fatalf("set: code=%d err=%q", code, errs)
	}
	code, out, _ := runBriefCmd(t, "", "vault", "list")
	if code != 0 || strings.TrimSpace(out) != "CANARY" {
		t.Fatalf("list: code=%d out=%q", code, out)
	}
	v, err := vault.Open(p, "pw", vaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v.Get(vault.HarnessScope, "CANARY"); !ok || got != "canary-9f8e7d" {
		t.Errorf("stored value = %q, %v", got, ok)
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("canary-9f8e7d")) {
		t.Error("vault file contains the plaintext value")
	}
	if strings.Contains(out+errs, "canary-9f8e7d") {
		t.Error("value leaked to output")
	}
}

func TestBriefVaultSetStoresPipedMultiLineValueWhole(t *testing.T) {
	old := vaultOptions
	vaultOptions = vault.Options{WorkFactor: 10}
	t.Cleanup(func() { vaultOptions = old })
	for _, c := range []struct{ name, stdin, want string }{
		{"multi-line", "line1\nline2\nline3\n", "line1\nline2\nline3"},
		{"crlf stripped once", "line1\r\nline2\r\n", "line1\r\nline2"},
		{"no trailing newline", "line1\nline2", "line1\nline2"},
	} {
		p := filepath.Join(t.TempDir(), "v.age")
		t.Setenv("GOPHERMIND_VAULT_PATH", p)
		t.Setenv(vault.PassphraseEnv, "pw")
		if code, _, errs := runBriefCmd(t, c.stdin, "vault", "set", "PEM"); code != 0 {
			t.Fatalf("%s: code=%d err=%q", c.name, code, errs)
		}
		v, err := vault.Open(p, "pw", vaultOptions)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := v.Get(vault.HarnessScope, "PEM"); got != c.want {
			t.Errorf("%s: stored %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBriefVaultSetEmptyStdinFails(t *testing.T) {
	t.Setenv("GOPHERMIND_VAULT_PATH", filepath.Join(t.TempDir(), "v.age"))
	t.Setenv(vault.PassphraseEnv, "pw")
	if code, _, _ := runBriefCmd(t, "", "vault", "set", "PEM"); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestBriefTreeCheckEmptyDirFails(t *testing.T) {
	code, out, errs := runBriefCmd(t, "", "tree", "check", t.TempDir())
	if code != 1 {
		t.Errorf("code=%d out=%q err=%q, want 1", code, out, errs)
	}
}

func TestBriefTreeCheckOnExampleSubset(t *testing.T) {
	src := filepath.Join(exampleDir, "tree", "gm-2026-09-29-001")
	dst := t.TempDir()
	for _, rel := range []string{"root.json", "types/component.json", "types/fn-validation-error-error.json",
		"registration/component.json", "registration/fn-validate-email.json", "registration/fn-validate-username.json"} {
		b, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o700)
		_ = os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	}
	code, out, errs := runBriefCmd(t, "", "tree", "check", dst)
	if code != 0 || !strings.Contains(out, "6 nodes") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestBriefTreeCheckFullExampleReportsDanglingDependency(t *testing.T) {
	code, _, errs := runBriefCmd(t, "", "tree", "check", filepath.Join(exampleDir, "tree", "gm-2026-09-29-001"))
	if code != 1 || !strings.Contains(errs, "unknown node") {
		t.Errorf("the partial example tree must fail with an unknown-node error (deviation D4): code=%d err=%q", code, errs)
	}
}

func TestBriefUnknownSubcommandPrintsUsage(t *testing.T) {
	code, _, errs := runBriefCmd(t, "", "run", "x.md")
	if code != 1 || !strings.Contains(errs, "usage") {
		t.Errorf("code=%d err=%q", code, errs)
	}
}

func TestBriefValidateDuplicateAndReservedExitTwo(t *testing.T) {
	src, _ := os.ReadFile(filepath.Join(exampleDir, "brief.md"))
	for name, tc := range map[string]struct{ old, add, want string }{
		"reserved":  {"env:\n", "  - name: HTTP_PROXY\n    purpose: p\n", "HTTP_PROXY is reserved for the harness"},
		"duplicate": {"secrets:\n", "  - name: CRM_API_KEY\n    purpose: p\n", "secret CRM_API_KEY is declared twice"},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "b.md")
			body := strings.Replace(string(src), tc.old, tc.old+tc.add, 1)
			_ = os.WriteFile(p, []byte(body), 0o600)
			code, _, errs := runBriefCmd(t, "", "validate", p)
			if code != 2 || !strings.Contains(errs, tc.want) {
				t.Errorf("code=%d err=%q", code, errs)
			}
		})
	}
}

func TestBriefValidateWarningWording(t *testing.T) {
	src, err := os.ReadFile("../../gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "b.md")
	body := strings.Replace(string(src), "\n## ", "\n\nMail goes through SENDGRID_KEY.\n\n## ", 1)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, errs := runBriefCmd(t, "", "validate", p)
	if !strings.Contains(errs, "looks like a secret name but is not declared under secrets or env") {
		t.Errorf("err=%q", errs)
	}
}
