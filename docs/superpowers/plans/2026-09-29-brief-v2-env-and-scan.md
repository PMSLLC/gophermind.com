# Brief v2: env block, overlap rejection, secret scan, exec environment

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the branch in line with the updated v2 handoff: the frontmatter `env` block, `ENV_SECRET_OVERLAP` rejection, the tightened secret-name scan, and the exact environment handed to commands.

**Architecture:** Task 1 updates the `brief` package and its fixtures (schema copy, `Env` field, overlap check, new scan rule) and the CLI tests. Task 2 adds a small pure package `execenv` that builds the `exec.Cmd` environment, plus README updates. No model calls, no executor.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), existing packages `schema`, `brief`, `vault`.

**Spec:** the updated handoff, `docs/briefv2/handoff/` after Task 1 replaces it (from `~/Downloads/gophermind-v2-handoff (1).zip`): BUILD_PLAN.md item 2 (the `env` block, the exact scan algorithm, `ENV_SECRET_OVERLAP`, the `exec.Cmd` environment), SPEC.md (frontmatter table, decomposer rule 7, the Secret names row), README.md non-negotiable 4. Prior plan: `docs/superpowers/plans/2026-09-29-brief-v2-foundations.md`.

## Global Constraints

- Pure Go, no cgo, no new dependency.
- Secret values never appear in error messages, logs, node files, or test output. A `secrets` name never has a default and is never sourced from `env` or harness config.
- Every package has table-driven tests using temp dirs. No test touches the network.
- Do not modify `plantree`, `plan`, `orchestrate`, `phaseflow`, or `freellm`.
- New documents use no em dashes and no emojis. Code is gofmt-clean and `go vet` clean.
- `go build ./...` at the repo root is broken at baseline (missing gitignored `desktop/frontend/dist`); use `go build ./cmd/... ./gophermind-lib/...`.
- Commit messages end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`. Stage explicit paths only. No push, no `--gw-force`, no `git stash`.

## Decisions and deviations

| # | Handoff says | Resolution here |
|---|---|---|
| E1 | Scan rule has no stoplist (the old `HTTP`, `JSON`, ... list is gone) | Implement the rule exactly: a token is flagged only if undeclared and (a) ends in `_KEY`, `_SECRET`, `_TOKEN`, `_URL`, `_DSN`, `_PASSWORD`, `_PASSPHRASE`, or (b) sits on a line containing "secret", "credential" or "environment variable" (case-insensitive). The old stoplist code is removed. On the venture-studio fixture this yields 2 warnings (`POST` and `BAD_CREDENTIALS` on one "credential" line); the handoff's ceiling is 3 |
| E2 | Harness config `[v2.env]` overrides env defaults | Config format is still undecided (see the foundations plan, D6), so `execenv.Build` takes the overrides as a `map[string]string` argument; wiring it to config is a later plan |
| E3 | The brief copy in `~/Downloads/ai-venture-studio-server-brief.md` | That copy is the OLD version (no `env`, 8 warnings under the new rule). The fixture is the new one from `~/Downloads/files (6).zip` (47,943 bytes) |
| E4 | `HTTP_PROXY`/`HTTPS_PROXY`/`GOPHERMIND_NODE` always present | The proxy does not exist yet (item 12), so `ProxyURL` is optional: empty means the two proxy variables are omitted. `GOPHERMIND_NODE` is always set |

## Review Focus

1. A secret name can never receive a default or an override, and an `env` name equal to a secret name is rejected with exactly `ENV_SECRET_OVERLAP: <name>` (Task 1 loader, Task 2 builder).
2. Nothing from the harness's own process environment leaks into the command environment, in particular `GOPHERMIND_VAULT_PASSPHRASE`. (Task 2)
3. An override for an undeclared env name, or for a secret name, is an error rather than silently ignored. (Task 2)
4. The scan does not flag ordinary words (`JSONB`, `PUT`, `TOKEN_REUSED`) but does flag an undeclared `SENDGRID_KEY`; declared `env` names are exempt even when they end in `_URL`. (Task 1)
5. A secret source that returns extra, missing or malformed entries is an error, not a partial environment. (Task 2)

## File Structure

```text
docs/briefv2/handoff/**                              replaced with the updated zip
gophermind-lib/briefv2/schema/brief-frontmatter.schema.json   replaced (adds env)
gophermind-lib/briefv2/testdata/example/**           replaced (examples/brief.md uses env)
gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md   new fixture
gophermind-lib/briefv2/brief/brief.go, brief_test.go  Env field, overlap check, new scan
gophermind-lib/briefv2/execenv/execenv.go, execenv_test.go   command environment builder
cmd/gophermind/brief_test.go                          updated CLI tests
docs/briefv2/README.md                                documents env, overlap, scan, exec environment
```

---

### Task 1: `env` block, overlap rejection, new scan rule, updated fixtures

**Files:**
- Modify: `gophermind-lib/briefv2/brief/brief.go`, `gophermind-lib/briefv2/brief/brief_test.go`, `cmd/gophermind/brief_test.go`
- Replace: `docs/briefv2/handoff/**`, `gophermind-lib/briefv2/schema/brief-frontmatter.schema.json`, `gophermind-lib/briefv2/testdata/example/**`
- Create: `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md`

**Interfaces:**
- Consumes: `schema.Validate(schema.KindBrief, ...)` (now accepts `env`).
- Produces: `brief.EnvVar{Name, Purpose string; Default *string}`; `Frontmatter.Env []EnvVar`; `Parse` returns `*InvalidError` with the exact text `ENV_SECRET_OVERLAP: <name>` when a name is in both lists; `UndeclaredSecrets()` implements the new rule.

- [ ] **Step 1: Replace the handoff copy and fixtures**

```bash
cd /Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/.worktrees/briefv2-foundations
rm -rf "$TMPDIR/gm-v2b" "$TMPDIR/gm-files6" && mkdir -p "$TMPDIR/gm-v2b" "$TMPDIR/gm-files6"
unzip -q "$HOME/Downloads/gophermind-v2-handoff (1).zip" -d "$TMPDIR/gm-v2b"
unzip -q "$HOME/Downloads/files (6).zip" -d "$TMPDIR/gm-files6"
H="$TMPDIR/gm-v2b/gophermind-v2-handoff"
rm -rf docs/briefv2/handoff && cp -R "$H" docs/briefv2/handoff
cp "$H/schema/brief-frontmatter.schema.json" gophermind-lib/briefv2/schema/
rm -rf gophermind-lib/briefv2/testdata/example && cp -R "$H/examples" gophermind-lib/briefv2/testdata/example
cp "$TMPDIR/gm-files6/ai-venture-studio-server-brief.md" gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md
wc -c gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md   # expect 47943
git status --short | head -20
```

Expected: 47943 bytes. The schema test still passes; tests that assumed the old example brief will fail until Step 3 (that is expected).

- [ ] **Step 2: Write the failing tests** (`brief_test.go`; replace the old `TestUndeclaredSecretWarnings` and add the rest; keep the other tests)

```go
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
	lines := strings.Split(strings.ReplaceAll(src, "\n", "\n"), "\n")
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
```

Also update the imports of `brief_test.go` if needed (`sort`, `os`, `strings` are already used). Delete the old `TestUndeclaredSecretWarnings` (its 5 expected error-code warnings no longer apply).

`cmd/gophermind/brief_test.go`: change `TestBriefValidate` to expect NO `warning` lines on the example brief and stdout containing `gm-2026-09-29-001`; add:

```go
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
```

- [ ] **Step 3: Run to verify failure**

Run: `cd gophermind-lib && go test ./briefv2/brief/ 2>&1 | head -20` and `cd .. && go test ./cmd/gophermind/ -run TestBrief 2>&1 | head -20`
Expected: FAIL (`Frontmatter` has no `Env`; old scan rule flags error codes).

- [ ] **Step 4: Implement** (`brief.go`)

Add the type and field:

```go
type EnvVar struct {
	Name    string  `json:"name"`
	Purpose string  `json:"purpose"`
	Default *string `json:"default"`
}
```

Add `Env []EnvVar `json:"env"`` to `Frontmatter` (after `Network`).

In `Parse`, immediately after `json.Unmarshal(js, &b.Front)` succeeds:

```go
	secretNames := map[string]bool{}
	for _, s := range b.Front.Secrets {
		secretNames[s.Name] = true
	}
	for _, e := range b.Front.Env {
		if secretNames[e.Name] {
			return nil, invalid("ENV_SECRET_OVERLAP: %s", e.Name)
		}
	}
```

Replace the scan section (delete `stoplist`; keep `Warning`):

```go
var (
	tokenRE        = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\b`)
	secretSuffixes = []string{"_KEY", "_SECRET", "_TOKEN", "_URL", "_DSN", "_PASSWORD", "_PASSPHRASE"}
	secretWords    = []string{"secret", "credential", "environment variable"}
)

func hasSecretSuffix(tok string) bool {
	for _, s := range secretSuffixes {
		if strings.HasSuffix(tok, s) {
			return true
		}
	}
	return false
}

// UndeclaredSecrets scans the body for tokens that look like secret names but
// are declared under neither secrets nor env. A token is flagged when it ends
// in a secret-ish suffix, or when it sits on a line that mentions "secret",
// "credential" or "environment variable" (case-insensitive). Warn-only: it
// never edits the brief and never changes an exit code.
func (b *Brief) UndeclaredSecrets() []Warning {
	declared := map[string]bool{}
	for _, s := range b.Front.Secrets {
		declared[s.Name] = true
	}
	for _, e := range b.Front.Env {
		declared[e.Name] = true
	}
	var out []Warning
	for i, line := range strings.Split(b.body, "\n") {
		lower := strings.ToLower(line)
		wording := false
		for _, w := range secretWords {
			if strings.Contains(lower, w) {
				wording = true
				break
			}
		}
		seen := map[string]bool{}
		for _, tok := range tokenRE.FindAllString(line, -1) {
			if declared[tok] || seen[tok] || !(wording || hasSecretSuffix(tok)) {
				continue
			}
			seen[tok] = true
			out = append(out, Warning{Token: tok, Line: b.bodyLine + i})
		}
	}
	return out
}
```

Update the doc comment at the top of the package/`Parse` if it mentions the stoplist.

- [ ] **Step 5: Run everything**

```bash
cd gophermind-lib && go test ./briefv2/... -race 2>&1 | tail -10
cd .. && go test ./cmd/gophermind/ 2>&1 | tail -3
gofmt -l cmd gophermind-lib/briefv2; go vet ./cmd/... ./gophermind-lib/briefv2/...
go run ./cmd/gophermind brief validate gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md; echo "exit=$?"
```

Expected: all PASS; `gofmt` prints nothing; the smoke run prints at most 3 `warning:` lines then `ok: gm-2026-09-29-002 (AI Venture Studio Server), 19 features`, exit 0. If the warning count is above 3, stop and report the actual tokens.

- [ ] **Step 6: Commit**

```bash
git add docs/briefv2/handoff gophermind-lib/briefv2/schema/brief-frontmatter.schema.json gophermind-lib/briefv2/testdata gophermind-lib/briefv2/brief cmd/gophermind/brief_test.go
git commit -m "feat(briefv2): env block, ENV_SECRET_OVERLAP, and the tightened secret-name scan"
```

---

### Task 2: Command environment builder and README

**Files:**
- Create: `gophermind-lib/briefv2/execenv/execenv.go`, `gophermind-lib/briefv2/execenv/execenv_test.go`
- Modify: `docs/briefv2/README.md`

**Interfaces:**
- Consumes: `brief.EnvVar`.
- Produces: `execenv.Inputs{Env []brief.EnvVar; Overrides map[string]string; Secrets []string; SecretValues func(names []string) ([]string, error); ProxyURL, NodeID string}` and `execenv.Build(in Inputs) ([]string, error)` returning a sorted `NAME=value` slice for `exec.Cmd.Env` and nothing else. `SecretValues` is expected to be `vault.Env` bound to a scope (`func(names []string) ([]string, error) { return v.Env(vault.RunScope(id), names) }`).

- [ ] **Step 1: Write the failing test** (`execenv_test.go`)

```go
package execenv_test

import (
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/execenv"
)

func strp(s string) *string { return &s }

func secrets(vals map[string]string) func([]string) ([]string, error) {
	return func(names []string) ([]string, error) {
		var out []string
		for _, n := range names {
			out = append(out, n+"="+vals[n])
		}
		return out, nil
	}
}

func base() execenv.Inputs {
	return execenv.Inputs{
		Env: []brief.EnvVar{
			{Name: "LISTEN_ADDR", Purpose: "p", Default: strp(":8080")},
			{Name: "LOG_LEVEL", Purpose: "p"},
		},
		Secrets:      []string{"JWT_SIGNING_KEY"},
		SecretValues: secrets(map[string]string{"JWT_SIGNING_KEY": "s3cret-value"}),
		ProxyURL:     "http://node-fn-a@127.0.0.1:8480",
		NodeID:       "fn-a",
	}
}

func TestBuildExactEnvironment(t *testing.T) {
	t.Setenv("HARNESS_ONLY", "leak")
	t.Setenv("GOPHERMIND_VAULT_PASSPHRASE", "leak-pass")
	got, err := execenv.Build(base())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GOPHERMIND_NODE=fn-a",
		"HTTPS_PROXY=http://node-fn-a@127.0.0.1:8480",
		"HTTP_PROXY=http://node-fn-a@127.0.0.1:8480",
		"JWT_SIGNING_KEY=s3cret-value",
		"LISTEN_ADDR=:8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	for _, kv := range got {
		if strings.Contains(kv, "leak") {
			t.Errorf("process environment leaked: %q", kv)
		}
	}
}

func TestOverrideBeatsDefaultAndFillsMissingDefault(t *testing.T) {
	in := base()
	in.Overrides = map[string]string{"LISTEN_ADDR": ":9090", "LOG_LEVEL": "debug"}
	got, err := execenv.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "LISTEN_ADDR=:9090") || !strings.Contains(joined, "LOG_LEVEL=debug") || strings.Contains(joined, ":8080") {
		t.Errorf("got %q", got)
	}
}

func TestEnvWithoutDefaultOrOverrideIsOmitted(t *testing.T) {
	got, _ := execenv.Build(base())
	for _, kv := range got {
		if strings.HasPrefix(kv, "LOG_LEVEL=") {
			t.Errorf("LOG_LEVEL has no default and must be omitted, got %q", kv)
		}
	}
}

func TestEmptyDefaultIsSetNotOmitted(t *testing.T) {
	in := base()
	in.Env = []brief.EnvVar{{Name: "EMPTY_OK", Purpose: "p", Default: strp("")}}
	got, err := execenv.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(got, "\n"), "EMPTY_OK=\n") && !contains(got, "EMPTY_OK=") {
		t.Errorf("an explicit empty default must be set, got %q", got)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestNoProxyVarsWhenProxyURLEmpty(t *testing.T) {
	in := base()
	in.ProxyURL = ""
	got, _ := execenv.Build(in)
	for _, kv := range got {
		if strings.Contains(kv, "PROXY") {
			t.Errorf("proxy var present without a ProxyURL: %q", kv)
		}
	}
	if !contains(got, "GOPHERMIND_NODE=fn-a") {
		t.Error("GOPHERMIND_NODE must always be set")
	}
}

func TestRejections(t *testing.T) {
	cases := map[string]struct {
		mut  func(*execenv.Inputs)
		want string
	}{
		"override for undeclared name": {func(in *execenv.Inputs) { in.Overrides = map[string]string{"NOPE": "x"} }, "NOPE"},
		"override for a secret name":   {func(in *execenv.Inputs) { in.Overrides = map[string]string{"JWT_SIGNING_KEY": "x"} }, "JWT_SIGNING_KEY"},
		"env name equals secret name": {func(in *execenv.Inputs) {
			in.Env = append(in.Env, brief.EnvVar{Name: "JWT_SIGNING_KEY", Purpose: "p", Default: strp("d")})
		}, "ENV_SECRET_OVERLAP: JWT_SIGNING_KEY"},
		"duplicate env name": {func(in *execenv.Inputs) {
			in.Env = append(in.Env, brief.EnvVar{Name: "LISTEN_ADDR", Purpose: "p"})
		}, "LISTEN_ADDR"},
		"bad node id": {func(in *execenv.Inputs) { in.NodeID = "../x" }, "node id"},
		"secrets without a source": {func(in *execenv.Inputs) { in.SecretValues = nil }, "SecretValues"},
		"source returns an extra entry": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return []string{"JWT_SIGNING_KEY=a", "OTHER=b"}, nil }
		}, "secret source"},
		"source returns a malformed entry": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return []string{"nonsense"}, nil }
		}, "secret source"},
		"source returns too few entries": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return nil, nil }
		}, "secret source"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := base()
			c.mut(&in)
			_, err := execenv.Build(in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
			if strings.Contains(err.Error(), "s3cret-value") {
				t.Error("error leaked a secret value")
			}
		})
	}
}

func TestSourceErrorPassesThrough(t *testing.T) {
	in := base()
	in.SecretValues = func([]string) ([]string, error) { return nil, errFake }
	if _, err := execenv.Build(in); err != errFake {
		t.Fatalf("got %v", err)
	}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

var errFake error = fakeErr("vault: secret X is not set in scope run/gm")
```

Fix while transcribing: `TestEmptyDefaultIsSetNotOmitted` should assert simply `contains(got, "EMPTY_OK=")`; delete the `strings.Contains(... "EMPTY_OK=\n")` clause. Keep the tests otherwise as written.

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/execenv/ 2>&1 | head`
Expected: FAIL, package does not compile (`undefined: execenv.Build`).

- [ ] **Step 3: Implement** (`execenv.go`)

```go
// Package execenv builds the exact environment handed to an exec.Cmd running a
// node's commands: the brief's declared env (defaults, then config overrides),
// the declared secrets (values from the vault), the proxy variables, and the
// node id. Nothing from the harness process environment is ever included, and
// a secret name never gets a default or an override.
package execenv

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Inputs struct {
	// Env is the brief's declared non-secret environment.
	Env []brief.EnvVar
	// Overrides comes from harness config ([v2.env]); every key must be a
	// declared env name.
	Overrides map[string]string
	// Secrets are the declared secret names; SecretValues returns NAME=value
	// entries for exactly those names (normally vault.Env bound to a scope).
	Secrets      []string
	SecretValues func(names []string) ([]string, error)
	// ProxyURL is optional until the proxy exists; empty omits the proxy vars.
	ProxyURL string
	NodeID   string
}

// Build returns the sorted NAME=value environment for exec.Cmd.Env.
func Build(in Inputs) ([]string, error) {
	secrets := map[string]bool{}
	for _, s := range in.Secrets {
		secrets[s] = true
	}
	declared := map[string]bool{}
	for _, e := range in.Env {
		if secrets[e.Name] {
			return nil, fmt.Errorf("ENV_SECRET_OVERLAP: %s", e.Name)
		}
		if declared[e.Name] {
			return nil, fmt.Errorf("execenv: env %s is declared twice", e.Name)
		}
		declared[e.Name] = true
	}
	for name := range in.Overrides {
		if secrets[name] {
			return nil, fmt.Errorf("execenv: %s is a secret and cannot be set from config", name)
		}
		if !declared[name] {
			return nil, fmt.Errorf("execenv: override for undeclared env %s", name)
		}
	}
	if !idRE.MatchString(in.NodeID) {
		return nil, fmt.Errorf("execenv: invalid node id %q", in.NodeID)
	}

	var out []string
	for _, e := range in.Env {
		if v, ok := in.Overrides[e.Name]; ok {
			out = append(out, e.Name+"="+v)
		} else if e.Default != nil {
			out = append(out, e.Name+"="+*e.Default)
		}
	}
	if len(in.Secrets) > 0 {
		if in.SecretValues == nil {
			return nil, errors.New("execenv: secrets are declared but no SecretValues source was given")
		}
		vals, err := in.SecretValues(in.Secrets)
		if err != nil {
			return nil, err
		}
		if len(vals) != len(in.Secrets) {
			return nil, fmt.Errorf("execenv: secret source returned %d entries for %d declared secrets", len(vals), len(in.Secrets))
		}
		for _, kv := range vals {
			name, _, ok := strings.Cut(kv, "=")
			if !ok || !secrets[name] {
				return nil, errors.New("execenv: secret source returned a malformed or undeclared entry")
			}
			out = append(out, kv)
		}
	}
	if in.ProxyURL != "" {
		out = append(out, "HTTP_PROXY="+in.ProxyURL, "HTTPS_PROXY="+in.ProxyURL)
	}
	out = append(out, "GOPHERMIND_NODE="+in.NodeID)
	sort.Strings(out)
	return out, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/execenv/ -race -v 2>&1 | tail -20`
Expected: PASS. Then `go test ./briefv2/... -race`, `gofmt -l briefv2`, `go vet ./briefv2/...`.

- [ ] **Step 5: Update `docs/briefv2/README.md`**

Add short sections (plain text, no em dashes, no emojis): the `env` block (fields, optional `default`, may not share a name with a secret, rejected with exit 2 and `ENV_SECRET_OVERLAP: <name>`); the secret-name scan rule exactly as in Decision E1 including that warnings never change the exit code; the command environment produced by `execenv.Build` (declared env with defaults and config overrides, declared secrets from the vault, `HTTP_PROXY`/`HTTPS_PROXY` when a proxy URL is given, `GOPHERMIND_NODE`, nothing else, a secret never has a default); and change the D5 row of the deviation table to say the handoff update resolved the scan noise (venture-studio fixture: 2 warnings, ceiling 3). Mention decisions E1 to E4 in one short list.

- [ ] **Step 6: Commit**

```bash
git add gophermind-lib/briefv2/execenv docs/briefv2/README.md
git commit -m "feat(briefv2): command environment builder with secrets/env separation"
```

---

## Self-Review

**Spec coverage (BUILD_PLAN item 2, updated):** `env` parsing into `Frontmatter.Env` (Task 1); `ENV_SECRET_OVERLAP` exit 2 (Task 1 loader and CLI test); the exact scan algorithm with suffix list, line wording trigger, declared exemptions, warn-only, and the fixture ceiling (Task 1); the `exec.Cmd` environment (Task 2, as a pure builder; harness-config wiring and the real vault-bound source are later plans, E2). SPEC.md rule 7 (a secret never gets a default) is enforced in Task 2. The item 10 executor line is a note for the executor plan.

**Placeholders:** none; the one transcription note in Task 2 Step 1 names the exact clause to delete.

**Type consistency:** `brief.EnvVar` (Name, Purpose, Default *string) is used by `Frontmatter.Env` and `execenv.Inputs.Env`; `execenv.Build`'s error text `ENV_SECRET_OVERLAP: <name>` matches the loader's.
