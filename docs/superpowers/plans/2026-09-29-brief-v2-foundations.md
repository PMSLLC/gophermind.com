# Brief v2 Foundations Implementation Plan (build items 1 to 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the offline foundations of the v2 "brief to build" feature: embedded schemas, the brief loader, the encrypted vault, the task tree store with wave assignment, contract slicing, and a `gophermind brief` command group that exercises them. No model calls, no executor.

**Architecture:** New packages under `gophermind-lib/briefv2/` (schema, brief, vault, tree, contract, rundir), all pure Go, one package per responsibility, tested with temp dirs and fixtures. The CLI lives in `cmd/gophermind/brief.go` and is dispatched early in `main.go` (same pattern as `free`). The existing `/project` pipeline (`plantree`, `plan`, `orchestrate`, `phaseflow`) is not touched.

**Tech Stack:** Go 1.25 (module `gophermind/gophermind-lib`), `github.com/santhosh-tekuri/jsonschema/v6`, `filippo.io/age` (scrypt passphrase), `gopkg.in/yaml.v3`, `golang.org/x/term` (already a dependency).

**Spec:** `docs/briefv2/handoff/SPEC.md` and `docs/briefv2/handoff/BUILD_PLAN.md` (copied from `gophermind-v2-handoff.zip` in Task 1). BUILD_PLAN.md wins where they differ. This plan covers BUILD_PLAN items 1 to 4 only. Items 5 to 15 (planner, blackboard, provider and router, human gates, packer, executor, resume, proxy, git landing, report, live view) are separate plans after the checkpoint report.

## Global Constraints

- Pure Go, no cgo, single binary. No new dependency beyond the three listed above (user-approved: go-git, age, jsonschema; YAML only for brief frontmatter). Do not add a TOML library or a `gophermind.yaml` config.
- Secret values never appear in error messages, logs, node files, or test output. Vault errors name the secret, never its value.
- Every package has table-driven tests using temp dirs. No test touches the network.
- Node and brief IDs must match the schemas' patterns; nothing builds a path from an unvalidated ID.
- Do not modify `plantree`, `plan`, `orchestrate`, `phaseflow`, or `freellm`.
- New documents use no em dashes and no emojis.
- Commit after each task. Commit messages end with the attribution trailer required by the session.

## Decisions and deviations from the handoff (read before Task 1)

The handoff's stated tests cannot all pass against its own example data. These resolutions are chosen because they reproduce every value in the examples that can be checked. Each is called out again in the task that owns it. **Report all of them at the checkpoint.**

| # | Handoff says | Problem | Resolution in this plan |
|---|---|---|---|
| D1 | CLI is `gophermind run`, `resume`, `status`, `report`, `validate`, `vault`, `tree` | `run`, `resume`, `status`, `report` already exist as commands | Namespace everything as `gophermind brief <sub>` (`brief validate`, `brief vault set`, `brief tree check`, later `brief run`, ...) |
| D2 | Wave rule: components take the max wave of their children; function with empty `depends_on` has no defined wave | The example has `registration` at wave 1 with a child at wave 2, and `fn-validation-error-error` (no deps) at wave 0 | `wave = 0` if `depends_on` is empty, else `1 + max(wave of depends_on)`, for every kind. Children do not affect a node's wave. Readiness (children verified) is separate |
| D3 | Slicing `fn-register-handler` yields "the seven entries shown in its node file" | The stated algorithm over the shipped `contracts.json` yields 10 entries (4 types, 6 functions). The node file's 7 entries are hand-written, include a `CRM` interface and a `Server` struct that `contracts.json` does not define, and omit 5 signatures | The golden test asserts the algorithm's 10 entries. The shipped `contracts.json` is used unchanged. Open item for John: the contract is missing `Server` and `CRM` types that `fn-server-new` relies on |
| D4 | "Every file under examples/tree validates", waves reproduced | The example tree is a partial excerpt: only 7 node files, while `depends_on` and `children` reference ~10 more | Per-file schema validation runs on all 7. Tree-level tests use the 6-node subset whose dependencies exist, plus synthetic trees. `Load` does not require `children` to resolve |
| D5 | Secret scan regex `\b[A-Z][A-Z0-9_]{2,}\b` minus a stoplist | It flags 5 error codes in the example brief (`EMAIL_INVALID` etc.) | Keep the regex; warnings only, never blocking. Test pins the 5 expected warnings |
| D6 | Config in `gophermind.yaml`, then TOML | `GOPHERMIND.toml` is not parsed by any Go code and there is no TOML library | Items 1 to 4 need no harness config. Vault path defaults to `<config.Dir()>/vault.age`. Config format is decided in the provider/router plan |
| D7 | Blackboard: reuse the recursive agent system's | Not in this repo; John did not say where it lives | Assumption: build the SQLite backend in the blackboard plan (item 6). Not needed here |

## Review Focus

1. A brief saved with CRLF line endings or a UTF-8 BOM parses identically to an LF file. (Task 2 tests it.)
2. A `## ` line inside a fenced code block is not a section heading. (Task 2)
3. A wrong vault passphrase fails clearly and never overwrites or truncates the existing vault file; concurrent `Set` calls in one process lose no writes. (Task 3)
4. A node file stored in the wrong directory, or with a path-like ID, is rejected at load instead of read. (Task 4)
5. A `uses` cycle between contract functions terminates, an unknown ID errors, and a node never lists its own signature. (Task 5)

## File Structure

```text
docs/briefv2/handoff/                      copy of the extracted zip (reference for later plans)
gophermind-lib/briefv2/
  schema/{task-node,brief-frontmatter,contract}.schema.json   embedded
  schema/schema.go, schema_test.go         Validate(kind, jsonBytes)
  testdata/example/{brief.md,contracts.json,tree/...}          fixtures copied from examples/
  brief/brief.go, brief_test.go            frontmatter, sections, secret-name scan
  vault/vault.go, vault_test.go            age-encrypted secret store, prompts
  tree/node.go, tree.go, store.go (+ tests)   nodes, cycle check, waves, readiness, file store
  contract/contract.go, contract_test.go   load, Slice, Diff, Affected
  rundir/rundir.go, rundir_test.go         .gophermind/<id>/ layout
cmd/gophermind/brief.go, brief_test.go     `gophermind brief ...`
cmd/gophermind/main.go                     one dispatch line
docs/briefv2/README.md                     what exists, CLI, deviations
```

---

### Task 1: Dependencies, embedded schemas, `schema.Validate`

**Files:**
- Create: `docs/briefv2/handoff/**` (copy), `gophermind-lib/briefv2/schema/*.schema.json` (3), `gophermind-lib/briefv2/schema/schema.go`, `gophermind-lib/briefv2/schema/schema_test.go`, `gophermind-lib/briefv2/testdata/example/**`
- Modify: `gophermind-lib/go.mod`, `gophermind-lib/go.sum`

**Interfaces:**
- Produces: `schema.Kind` with consts `KindBrief`, `KindNode`, `KindContract`; `func Validate(kind Kind, doc []byte) error` (doc is JSON; returns the library's validation error whose text names the failing property path).

- [ ] **Step 1: Copy the handoff and fixtures into the repo**

```bash
cd /Users/jbrahy/OtherProjects/PMSLLC/gophermind.com
rm -rf "$TMPDIR/gm-v2" && mkdir -p "$TMPDIR/gm-v2" docs/briefv2 gophermind-lib/briefv2/schema gophermind-lib/briefv2/testdata
unzip -q ~/Downloads/gophermind-v2-handoff.zip -d "$TMPDIR/gm-v2"
H="$TMPDIR/gm-v2/gophermind-v2-handoff"
cp -R "$H" docs/briefv2/handoff
cp "$H"/schema/*.json gophermind-lib/briefv2/schema/
cp -R "$H/examples" gophermind-lib/briefv2/testdata/example
# flatten the tree fixture path used by tests
ls gophermind-lib/briefv2/testdata/example/tree/gm-2026-09-29-001
```

Expected: `contracts.json  registration  root.json  types`.

- [ ] **Step 2: Add dependencies**

```bash
cd gophermind-lib
go get github.com/santhosh-tekuri/jsonschema/v6@latest filippo.io/age@latest gopkg.in/yaml.v3@latest
```

- [ ] **Step 3: Write the failing test** (`gophermind-lib/briefv2/schema/schema_test.go`)

```go
package schema_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/schema"
)

const exampleTree = "../testdata/example/tree/gm-2026-09-29-001"

func TestExampleNodesValidate(t *testing.T) {
	n := 0
	err := filepath.WalkDir(exampleTree, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "contracts.json" {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if verr := schema.Validate(schema.KindNode, raw); verr != nil {
			t.Errorf("%s: %v", p, verr)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("expected 7 example node files, found %d", n)
	}
}

func TestExampleContractsValidate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(exampleTree, "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		t.Fatal(err)
	}
}

// mutate loads a fixture, applies fn to its decoded form, and re-encodes it.
func mutate(t *testing.T, rel string, fn func(map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(exampleTree, rel))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRejects(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
		kind schema.Kind
	}{
		{"function test without command", mutate(t, "registration/fn-validate-email.json", func(m map[string]any) {
			for _, x := range m["tests"].([]any) {
				delete(x.(map[string]any), "command")
			}
		}), schema.KindNode},
		{"component without parent", mutate(t, "registration/component.json", func(m map[string]any) { delete(m, "parent") }), schema.KindNode},
		{"bad status", mutate(t, "root.json", func(m map[string]any) { m["status"] = "done" }), schema.KindNode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := schema.Validate(c.kind, c.doc); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestBriefErrorsNameTheField(t *testing.T) {
	cases := map[string]string{
		"spec_version": `{"id":"gm-2026-09-29-001","title":"t","language":"go","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
		"language":     `{"spec_version":"2.0","id":"gm-2026-09-29-001","title":"t","language":"python","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
	}
	for field, doc := range cases {
		err := schema.Validate(schema.KindBrief, []byte(doc))
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: want an error naming the field, got %v", field, err)
		}
	}
}

func TestUnknownKindAndBadJSON(t *testing.T) {
	if err := schema.Validate("nope", []byte(`{}`)); err == nil {
		t.Error("unknown kind must error")
	}
	if err := schema.Validate(schema.KindNode, []byte(`{`)); err == nil {
		t.Error("malformed JSON must error")
	}
}
```

- [ ] **Step 4: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/schema/ 2>&1 | head`
Expected: FAIL, `undefined: schema.Validate` (package has no non-test files yet).

- [ ] **Step 5: Implement** (`gophermind-lib/briefv2/schema/schema.go`)

```go
// Package schema embeds the three v2 JSON Schemas and validates documents
// against them. Every node write, contract write, and brief load goes through
// Validate: an invalid document is a planner bug, not a runtime condition.
package schema

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed task-node.schema.json brief-frontmatter.schema.json contract.schema.json
var files embed.FS

// Kind selects which embedded schema Validate checks against.
type Kind string

const (
	KindBrief    Kind = "brief"
	KindNode     Kind = "node"
	KindContract Kind = "contract"
)

var sources = map[Kind]struct{ file, id string }{
	KindBrief:    {"brief-frontmatter.schema.json", "https://gophermind.local/schema/brief-frontmatter/2.0"},
	KindNode:     {"task-node.schema.json", "https://gophermind.local/schema/task-node/2.0"},
	KindContract: {"contract.schema.json", "https://gophermind.local/schema/contract/2.0"},
}

var (
	once     sync.Once
	compiled map[Kind]*jsonschema.Schema
	initErr  error
)

func compile() {
	c := jsonschema.NewCompiler()
	for _, s := range sources {
		raw, err := files.ReadFile(s.file)
		if err != nil {
			initErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
		if err := c.AddResource(s.id, doc); err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
	}
	compiled = map[Kind]*jsonschema.Schema{}
	for k, s := range sources {
		sch, err := c.Compile(s.id)
		if err != nil {
			initErr = fmt.Errorf("schema %s: %w", s.file, err)
			return
		}
		compiled[k] = sch
	}
}

// Validate checks the JSON document doc against the schema for kind. The
// returned error's text names the failing property path (for example
// "missing property 'spec_version'").
func Validate(kind Kind, doc []byte) error {
	once.Do(compile)
	if initErr != nil {
		return initErr
	}
	sch, ok := compiled[kind]
	if !ok {
		return fmt.Errorf("schema: unknown kind %q", kind)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("schema: not valid JSON: %w", err)
	}
	return sch.Validate(v)
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/schema/ -v 2>&1 | tail -15`
Expected: PASS for all five tests. If `TestExampleNodesValidate` reports a schema error on a shipped example file, stop and report it (the handoff claims they validate).

- [ ] **Step 7: Confirm both modules still build**

Run: `cd /Users/jbrahy/OtherProjects/PMSLLC/gophermind.com && go build ./... && (cd gophermind-lib && go build ./... && go mod tidy)`
Expected: no errors. If the root module needs the new deps, run `go mod tidy` there too.

- [ ] **Step 8: Commit**

```bash
git add docs/briefv2 gophermind-lib/briefv2 gophermind-lib/go.mod gophermind-lib/go.sum go.mod go.sum
git commit -m "feat(briefv2): embed v2 schemas and validate documents against them"
```

---

### Task 2: Brief loader

**Files:**
- Create: `gophermind-lib/briefv2/brief/brief.go`, `gophermind-lib/briefv2/brief/brief_test.go`

**Interfaces:**
- Consumes: `schema.Validate(schema.KindBrief, []byte)`.
- Produces: `brief.Parse(src []byte) (*Brief, error)`; `type Brief struct { Front Frontmatter; Sections map[string]string; Features []Feature }`; `type Frontmatter` (fields below); `(*Brief).UndeclaredSecrets() []Warning`; `type Warning struct { Token string; Line int }`; `type InvalidError struct{ Reason string }` (the CLI maps it to exit code 2); `(Frontmatter).WorkBranchName() string`.

- [ ] **Step 1: Write the failing test** (`brief_test.go`)

```go
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
	crlf := "\ufeff" + strings.ReplaceAll(ex, "\n", "\r\n")
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/brief/ 2>&1 | head`
Expected: FAIL, package does not compile (`undefined: brief.Parse`).

- [ ] **Step 3: Implement** (`brief.go`)

```go
// Package brief parses a v2 brief: YAML frontmatter validated against the
// embedded schema, fixed H2 sections, and a warn-only scan for secret-looking
// names the frontmatter did not declare.
package brief

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/schema"
)

// InvalidError means the brief itself is wrong (the CLI exits 2), as opposed
// to an I/O failure.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, a...)}
}

type Secret struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

type Network struct {
	Host     string `json:"host"`
	Purpose  string `json:"purpose"`
	Critical bool   `json:"critical"`
}

type Budget struct {
	MaxContextTokens int  `json:"max_context_tokens"`
	MaxRevisions     *int `json:"max_revisions"`
}

type Frontmatter struct {
	SpecVersion        string    `json:"spec_version"`
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Language           string    `json:"language"`
	Repo               string    `json:"repo"`
	BaseBranch         string    `json:"base_branch"`
	WorkBranch         string    `json:"work_branch"`
	Landing            string    `json:"landing"`
	OnAmbiguity        string    `json:"on_ambiguity"`
	MilestoneApprovals bool      `json:"milestone_approvals"`
	Secrets            []Secret  `json:"secrets"`
	Network            []Network `json:"network"`
	Budget             *Budget   `json:"budget"`
}

// WorkBranchName is work_branch, or gm/<id> when unset.
func (f Frontmatter) WorkBranchName() string {
	if f.WorkBranch != "" {
		return f.WorkBranch
	}
	return "gm/" + f.ID
}

type Feature struct {
	Name string
	Body string
}

type Brief struct {
	Front    Frontmatter
	Sections map[string]string // H2 text -> body
	Features []Feature         // H3 blocks under ## Features, in order

	body     string
	bodyLine int // 1-based file line of the first body line
}

var requiredSections = []string{"Overview", "Features", "Architecture", "Data", "Constraints", "Out of scope", "Acceptance"}

// Parse splits, validates, and sections a brief. Failures caused by the
// brief's content are *InvalidError.
func Parse(src []byte) (*Brief, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil, invalid("brief must start with a --- frontmatter block")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, invalid("frontmatter block is not closed with ---")
	}
	front := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return nil, invalid("frontmatter is not valid YAML: %v", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, invalid("frontmatter cannot be converted to JSON: %v", err)
	}
	if err := schema.Validate(schema.KindBrief, js); err != nil {
		return nil, invalid("frontmatter: %v", err)
	}
	b := &Brief{body: body, bodyLine: end + 2}
	if err := json.Unmarshal(js, &b.Front); err != nil {
		return nil, invalid("frontmatter: %v", err)
	}

	sections, features, err := parseSections(body)
	if err != nil {
		return nil, err
	}
	for _, name := range requiredSections {
		if strings.TrimSpace(sections[name]) == "" {
			return nil, invalid("missing or empty section \"## %s\"", name)
		}
	}
	if len(features) == 0 {
		return nil, invalid("\"## Features\" must contain at least one \"### Feature\" block")
	}
	b.Sections, b.Features = sections, features
	return b, nil
}

func parseSections(body string) (map[string]string, []Feature, error) {
	sections := map[string]string{}
	var features []Feature
	var secName, featName string
	var secLines, featLines []string
	var dupErr error

	flushFeat := func() {
		if featName != "" {
			features = append(features, Feature{Name: featName, Body: strings.TrimSpace(strings.Join(featLines, "\n"))})
		}
		featName, featLines = "", nil
	}
	flushSec := func() {
		flushFeat()
		if secName != "" {
			if _, dup := sections[secName]; dup && dupErr == nil {
				dupErr = invalid("duplicate section \"## %s\"", secName)
			}
			sections[secName] = strings.TrimSpace(strings.Join(secLines, "\n"))
		}
		secName, secLines = "", nil
	}

	inFence := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence {
			if strings.HasPrefix(line, "## ") {
				flushSec()
				secName = strings.TrimSpace(line[3:])
				continue
			}
			if secName == "Features" && strings.HasPrefix(line, "### ") {
				flushFeat()
				featName = strings.TrimSpace(line[4:])
				secLines = append(secLines, line)
				continue
			}
		}
		secLines = append(secLines, line)
		if featName != "" {
			featLines = append(featLines, line)
		}
	}
	flushSec()
	return sections, features, dupErr
}

// Warning is an undeclared secret-looking token and its 1-based file line.
type Warning struct {
	Token string
	Line  int
}

var (
	tokenRE  = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\b`)
	stoplist = map[string]bool{"HTTP": true, "JSON": true, "UUID": true, "URL": true, "API": true, "CRM": true, "ID": true, "GET": true, "POST": true, "UTC": true, "TODO": true}
)

// UndeclaredSecrets scans the body for UPPER_SNAKE tokens that are neither
// stoplisted nor declared under secrets. Warn-only: it never edits the brief.
func (b *Brief) UndeclaredSecrets() []Warning {
	declared := map[string]bool{}
	for _, s := range b.Front.Secrets {
		declared[s.Name] = true
	}
	var out []Warning
	for i, line := range strings.Split(b.body, "\n") {
		seen := map[string]bool{}
		for _, tok := range tokenRE.FindAllString(line, -1) {
			if stoplist[tok] || declared[tok] || seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, Warning{Token: tok, Line: b.bodyLine + i})
		}
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/brief/ -v 2>&1 | tail -20`
Expected: PASS. If `TestUndeclaredSecretWarnings` shows a different token set, report the actual set at the checkpoint instead of editing the expected list silently.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/briefv2/brief
git commit -m "feat(briefv2): brief loader with schema-validated frontmatter and section parsing"
```

---

### Task 3: Vault

**Files:**
- Create: `gophermind-lib/briefv2/vault/vault.go`, `gophermind-lib/briefv2/vault/vault_test.go`

**Interfaces:**
- Produces: `vault.HarnessScope` (`"harness"`), `vault.RunScope(id string) string` (`"run/<id>"`), `type Options struct{ WorkFactor int }` (0 means age's default), `vault.Open(path, passphrase string, opts Options) (*Vault, error)`, `(*Vault).Set(scope, name, value string) error`, `Get(scope, name string) (string, bool)`, `Names(scope string) []string` (sorted), `Env(scope string, names []string) ([]string, error)` (returns `NAME=value` for `exec.Cmd.Env` only; never touches `os.Environ`), `vault.PassphraseEnv` (`"GOPHERMIND_VAULT_PASSPHRASE"`), `vault.Passphrase(prompt string, in *os.File, out io.Writer) (string, error)`, `vault.ReadSecret(prompt string, in *os.File, out io.Writer) (string, error)`.

- [ ] **Step 1: Write the failing test** (`vault_test.go`)

```go
package vault_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/vault"
)

var fast = vault.Options{WorkFactor: 10}

func newVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", "vault.age")
	v, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	return v, p
}

func TestRoundTripAndEncryptedAtRest(t *testing.T) {
	v, p := newVault(t)
	const secret = "canary-9f8e7d"
	if err := v.Set(vault.HarnessScope, "CANARY", secret); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("CANARY")) {
		t.Fatal("vault file contains plaintext")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	v2, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v2.Get(vault.HarnessScope, "CANARY"); !ok || got != secret {
		t.Fatalf("Get = %q, %v", got, ok)
	}
	if names := v2.Names(vault.HarnessScope); len(names) != 1 || names[0] != "CANARY" {
		t.Errorf("Names = %v", names)
	}
}

func TestWrongPassphraseFailsAndKeepsFile(t *testing.T) {
	v, p := newVault(t)
	if err := v.Set(vault.HarnessScope, "KEY_ONE", "value-one"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	_, err := vault.Open(p, "wrong", fast)
	if err == nil {
		t.Fatal("wrong passphrase must fail")
	}
	if strings.Contains(err.Error(), "value-one") {
		t.Error("error leaked a secret value")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Error("failed Open modified the vault file")
	}
}

func TestSetRejectsBadNames(t *testing.T) {
	v, _ := newVault(t)
	for _, name := range []string{"", "lower", "1ABC", "A-B", "../X"} {
		if err := v.Set(vault.HarnessScope, name, "v"); err == nil {
			t.Errorf("name %q should be rejected", name)
		}
	}
	if err := v.Set(vault.HarnessScope, "OK_NAME", ""); err == nil {
		t.Error("empty value should be rejected")
	}
}

func TestEnvIsScopedAndDoesNotTouchProcessEnv(t *testing.T) {
	v, _ := newVault(t)
	run := vault.RunScope("gm-2026-09-29-001")
	_ = v.Set(run, "CRM_API_KEY", "run-secret")
	_ = v.Set(vault.HarnessScope, "GROQ_API_KEY", "harness-secret")
	env, err := v.Env(run, []string{"CRM_API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0] != "CRM_API_KEY=run-secret" {
		t.Fatalf("env = %v", env)
	}
	if os.Getenv("CRM_API_KEY") != "" {
		t.Error("Env leaked into the process environment")
	}
	if _, err := v.Env(run, []string{"GROQ_API_KEY"}); err == nil || strings.Contains(err.Error(), "harness-secret") {
		t.Errorf("harness secrets must not resolve in run scope, and errors must not include values: %v", err)
	}
}

func TestConcurrentSetsLoseNothing(t *testing.T) {
	v, p := newVault(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := v.Set(vault.HarnessScope, fmt.Sprintf("KEY_%02d", i), "v"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	v2, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(v2.Names(vault.HarnessScope)); n != 20 {
		t.Fatalf("persisted %d names, want 20", n)
	}
}

func TestPassphraseFromEnvAndPipe(t *testing.T) {
	t.Setenv(vault.PassphraseEnv, "from-env")
	got, err := vault.Passphrase("pw: ", os.Stdin, &bytes.Buffer{})
	if err != nil || got != "from-env" {
		t.Fatalf("Passphrase = %q, %v", got, err)
	}
	r, w, _ := os.Pipe()
	_, _ = w.WriteString("piped-value\n")
	_ = w.Close()
	val, err := vault.ReadSecret("v: ", r, &bytes.Buffer{})
	if err != nil || val != "piped-value" {
		t.Fatalf("ReadSecret = %q, %v", val, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/vault/ 2>&1 | head`
Expected: FAIL, package does not compile.

- [ ] **Step 3: Implement** (`vault.go`)

```go
// Package vault stores secrets in one age-encrypted file (scrypt passphrase).
// Two scopes share the file: "harness" (provider keys) and "run/<id>" (secrets
// a brief declared). Values are handed to commands only through Env, which
// returns a slice for exec.Cmd.Env and never touches the process environment.
package vault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"filippo.io/age"
	"golang.org/x/term"
)

const (
	HarnessScope  = "harness"
	PassphraseEnv = "GOPHERMIND_VAULT_PASSPHRASE"
)

// RunScope is the scope for a brief's declared secrets.
func RunScope(id string) string { return "run/" + id }

// Options tunes the vault. WorkFactor 0 uses age's default (18); tests pass a
// small value so scrypt stays fast.
type Options struct{ WorkFactor int }

type Vault struct {
	path string
	pass string
	opts Options

	mu   sync.Mutex
	data map[string]map[string]string
}

var nameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Open decrypts the vault at path, or returns an empty one when the file does
// not exist yet. A wrong passphrase is an error and leaves the file untouched.
func Open(path, passphrase string, opts Options) (*Vault, error) {
	v := &Vault{path: path, pass: passphrase, opts: opts, data: map[string]map[string]string{}}
	enc, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", path, err)
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(enc), id)
	if err != nil {
		return nil, fmt.Errorf("vault: cannot decrypt %s (wrong passphrase?): %w", path, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("vault: cannot decrypt %s: %w", path, err)
	}
	if err := json.Unmarshal(plain, &v.data); err != nil {
		return nil, fmt.Errorf("vault: %s is corrupt: %w", path, err)
	}
	return v, nil
}

// Set stores value under scope/name and persists the whole vault.
func (v *Vault) Set(scope, name, value string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("vault: secret name %q must match %s", name, nameRE)
	}
	if value == "" {
		return fmt.Errorf("vault: empty value for %s", name)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data[scope] == nil {
		v.data[scope] = map[string]string{}
	}
	old, had := v.data[scope][name]
	v.data[scope][name] = value
	if err := v.save(); err != nil {
		if had {
			v.data[scope][name] = old
		} else {
			delete(v.data[scope], name)
		}
		return err
	}
	return nil
}

func (v *Vault) Get(scope, name string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.data[scope][name]
	return s, ok
}

// Names lists secret names in scope, sorted. Never values.
func (v *Vault) Names(scope string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(v.data[scope]))
	for n := range v.data[scope] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Env returns NAME=value pairs for exactly the named secrets in scope, for use
// as exec.Cmd.Env entries. A missing name is an error naming the secret only.
func (v *Vault) Env(scope string, names []string) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(names))
	for _, n := range names {
		val, ok := v.data[scope][n]
		if !ok {
			return nil, fmt.Errorf("vault: secret %s is not set in scope %s", n, scope)
		}
		out = append(out, n+"="+val)
	}
	return out, nil
}

// save must be called with v.mu held.
func (v *Vault) save() error {
	plain, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	rec, err := age.NewScryptRecipient(v.pass)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if v.opts.WorkFactor > 0 {
		rec.SetWorkFactor(v.opts.WorkFactor)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rec)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	dir := filepath.Dir(v.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vault-*")
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), v.path)
}

// Passphrase returns GOPHERMIND_VAULT_PASSPHRASE when set, otherwise prompts
// on in with echo off.
func Passphrase(prompt string, in *os.File, out io.Writer) (string, error) {
	if v := os.Getenv(PassphraseEnv); v != "" {
		return v, nil
	}
	return ReadSecret(prompt, in, out)
}

// ReadSecret reads one secret with echo off when in is a terminal, or one line
// when in is a pipe (so `echo value | gophermind brief vault set NAME` works).
func ReadSecret(prompt string, in *os.File, out io.Writer) (string, error) {
	fd := int(in.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(out, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("vault: no value on stdin and no terminal to prompt (set %s for the passphrase)", PassphraseEnv)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/vault/ -race -v 2>&1 | tail -15`
Expected: PASS, including `-race`.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/briefv2/vault gophermind-lib/go.mod gophermind-lib/go.sum
git commit -m "feat(briefv2): age-encrypted vault with harness and per-run scopes"
```

---

### Task 4: Task tree (nodes, cycle check, waves, readiness, file store)

**Files:**
- Create: `gophermind-lib/briefv2/tree/node.go`, `tree.go`, `store.go`, and `node_test.go`, `tree_test.go`, `store_test.go`

**Interfaces:**
- Consumes: `schema.Validate(schema.KindNode, []byte)`.
- Produces:
  - `type Kind string` with `KindRoot`, `KindComponent`, `KindFunction`.
  - `type Node struct { ID string; Kind Kind; Parent string; Children, DependsOn []string; Wave *int }` (plus a private decoded document so unknown-to-Go fields survive round trips).
  - `tree.ParseNode(raw []byte) (Node, error)`; `(Node).Marshal() ([]byte, error)`; `(*Node).SetWave(int)`; `(Node).Path() string` (slash-separated relative path: `root.json`, `<id>/component.json`, `<parent>/<id>.json`).
  - `tree.NewTree(nodes []Node) (*Tree, error)` (duplicate IDs error); `(*Tree).CycleCheck() error`; `(*Tree).ComputeWaves() (map[string]int, error)`; `(*Tree).AssignWaves() error` (overwrites `Wave` with the computed value); `(*Tree).CheckWaves() error` (errors when a set wave disagrees); `(*Tree).Ready(id string, verified func(string) bool) bool`.
  - `tree.NewStore(dir string) *Store`; `(*Store).Write(n Node) error`; `(*Store).WriteAll(t *Tree) error` (cycle check, assign waves, write each); `(*Store).Load() (*Tree, error)`.

Deviation D2 applies: wave = 0 with no dependencies, else 1 + max over `depends_on`, for every kind.

- [ ] **Step 1: Write the failing tests**

`node_test.go`:

```go
package tree_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

const ex = "../testdata/example/tree/gm-2026-09-29-001"

func readNode(t *testing.T, rel string) tree.Node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ex, rel))
	if err != nil {
		t.Fatal(err)
	}
	n, err := tree.ParseNode(raw)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return n
}

// fn builds a valid function node with the given wave (schema requires one).
func fn(t *testing.T, id, parent string, wave int, deps ...string) tree.Node {
	t.Helper()
	dj := "[]"
	if len(deps) > 0 {
		dj = "["
		for i, d := range deps {
			if i > 0 {
				dj += ","
			}
			dj += fmt.Sprintf("%q", d)
		}
		dj += "]"
	}
	raw := fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"function","parent":%q,"title":"t","description":"d","brief_ref":"#x","status":"pending","wave":%d,"depends_on":%s,
"contract":{"package":"p","file":"p/%s.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`, id, parent, wave, dj, id)
	n, err := tree.ParseNode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParseExampleNodes(t *testing.T) {
	root := readNode(t, "root.json")
	if root.Kind != tree.KindRoot || root.Path() != "root.json" || len(root.Children) != 4 {
		t.Errorf("root: %+v", root)
	}
	c := readNode(t, "registration/component.json")
	if c.Path() != "registration/component.json" || c.Wave == nil || *c.Wave != 1 || c.DependsOn[0] != "types" {
		t.Errorf("component: %+v", c)
	}
	f := readNode(t, "registration/fn-validate-email.json")
	if f.Path() != "registration/fn-validate-email.json" || f.Parent != "registration" {
		t.Errorf("function: %+v", f)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	if _, err := tree.ParseNode([]byte(`{"id":"x"}`)); err == nil {
		t.Fatal("schema-invalid node must be rejected")
	}
}

func TestMarshalKeepsUnmodelledFields(t *testing.T) {
	n := readNode(t, "registration/fn-register-handler.json")
	n.SetWave(7)
	raw, err := n.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := tree.ParseNode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if *back.Wave != 7 || len(back.DependsOn) != 5 {
		t.Errorf("round trip lost data: %+v", back)
	}
}
```

`tree_test.go`:

```go
package tree_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func mustTree(t *testing.T, nodes ...tree.Node) *tree.Tree {
	t.Helper()
	tr, err := tree.NewTree(nodes)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDuplicateIDs(t *testing.T) {
	if _, err := tree.NewTree([]tree.Node{fn(t, "a", "c", 0), fn(t, "a", "c", 0)}); err == nil {
		t.Fatal("duplicate IDs must error")
	}
}

func TestCycleRejectedWithBothIDs(t *testing.T) {
	tr := mustTree(t, fn(t, "fn-a", "c", 1, "fn-b"), fn(t, "fn-b", "c", 1, "fn-a"))
	err := tr.CycleCheck()
	if err == nil || !strings.Contains(err.Error(), "fn-a") || !strings.Contains(err.Error(), "fn-b") || !strings.Contains(err.Error(), "->") {
		t.Fatalf("want a cycle path naming both nodes, got %v", err)
	}
	if _, err := tr.ComputeWaves(); err == nil {
		t.Error("ComputeWaves must refuse a cyclic tree")
	}
}

func TestSelfCycle(t *testing.T) {
	if err := mustTree(t, fn(t, "fn-a", "c", 1, "fn-a")).CycleCheck(); err == nil {
		t.Fatal("self dependency is a cycle")
	}
}

func TestWavesAreOnePlusMaxOfDependencies(t *testing.T) {
	tr := mustTree(t,
		fn(t, "fn-a", "c", 0),
		fn(t, "fn-b", "c", 1, "fn-a"),
		fn(t, "fn-c", "c", 0),
		fn(t, "fn-d", "c", 2, "fn-b", "fn-c"),
	)
	w, err := tr.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"fn-a": 0, "fn-b": 1, "fn-c": 0, "fn-d": 2}
	for id, v := range want {
		if w[id] != v {
			t.Errorf("wave(%s) = %d, want %d", id, w[id], v)
		}
	}
	if err := tr.CheckWaves(); err != nil {
		t.Errorf("consistent waves flagged: %v", err)
	}
}

func TestHandSetWaveThatDisagreesIsAnError(t *testing.T) {
	tr := mustTree(t, fn(t, "fn-a", "c", 0), fn(t, "fn-b", "c", 5, "fn-a"))
	err := tr.CheckWaves()
	if err == nil || !strings.Contains(err.Error(), "fn-b") {
		t.Fatalf("want a wave disagreement naming fn-b, got %v", err)
	}
	if err := tr.AssignWaves(); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWaves(); err != nil {
		t.Errorf("AssignWaves should have repaired the wave: %v", err)
	}
}

func TestUnknownDependency(t *testing.T) {
	if _, err := mustTree(t, fn(t, "fn-a", "c", 1, "fn-missing")).ComputeWaves(); err == nil || !strings.Contains(err.Error(), "fn-missing") {
		t.Fatalf("want an unknown-dependency error, got %v", err)
	}
}

// The example tree is a partial excerpt, so only the nodes whose dependencies
// exist are checked (deviation D4). Their file waves must be reproduced.
func TestExampleSubsetWavesReproduced(t *testing.T) {
	tr := mustTree(t,
		readNode(t, "root.json"),
		readNode(t, "types/component.json"),
		readNode(t, "types/fn-validation-error-error.json"),
		readNode(t, "registration/component.json"),
		readNode(t, "registration/fn-validate-email.json"),
		readNode(t, "registration/fn-validate-username.json"),
	)
	if err := tr.CheckWaves(); err != nil {
		t.Fatalf("example waves not reproduced: %v", err)
	}
}

func TestReadiness(t *testing.T) {
	root := readNode(t, "root.json")
	comp := readNode(t, "types/component.json")
	leaf := readNode(t, "types/fn-validation-error-error.json")
	tr := mustTree(t, root, comp, leaf, readNode(t, "registration/fn-validate-email.json"))
	verified := map[string]bool{}
	is := func(id string) bool { return verified[id] }

	if !tr.Ready("fn-validation-error-error", is) {
		t.Error("a function with no dependencies is ready")
	}
	if tr.Ready("fn-validate-email", is) {
		t.Error("fn-validate-email needs fn-validation-error-error verified")
	}
	verified["fn-validation-error-error"] = true
	if !tr.Ready("fn-validate-email", is) || !tr.Ready("types", is) {
		t.Error("dependency and children verified should make both ready")
	}
	if tr.Ready("gm-2026-09-29-001", is) {
		t.Error("root needs every component verified")
	}
	if tr.Ready("nope", is) {
		t.Error("unknown node is never ready")
	}
}
```

`store_test.go`:

```go
package tree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func TestStoreWriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	tr := mustTree(t, fn(t, "fn-a", "comp", 9), fn(t, "fn-b", "comp", 9, "fn-a"))
	if err := s.WriteAll(tr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "comp", "fn-a.json")); err != nil {
		t.Fatalf("expected comp/fn-a.json: %v", err)
	}
	back, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes) != 2 {
		t.Fatalf("loaded %d nodes", len(back.Nodes))
	}
	if err := back.CheckWaves(); err != nil {
		t.Errorf("WriteAll must recompute waves: %v", err)
	}
	if w := *back.Nodes["fn-b"].Wave; w != 1 {
		t.Errorf("fn-b wave = %d, want 1", w)
	}
}

func TestWriteAllRejectsCycles(t *testing.T) {
	s := tree.NewStore(t.TempDir())
	tr := mustTree(t, fn(t, "fn-a", "c", 1, "fn-b"), fn(t, "fn-b", "c", 1, "fn-a"))
	if err := s.WriteAll(tr); err == nil {
		t.Fatal("cyclic tree must not be written")
	}
}

func TestLoadSkipsNonNodeFilesAndRuntime(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"contracts.json", "answers.json", "approval.json", "report.json", "comp/fn-a.runtime.json"} {
		p := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(`{"not":"a node"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 {
		t.Errorf("loaded %d nodes, want 1", len(tr.Nodes))
	}
}

func TestLoadRejectsNodeInWrongDirectory(t *testing.T) {
	dir := t.TempDir()
	n := fn(t, "fn-a", "comp", 0)
	raw, _ := n.Marshal()
	if err := os.MkdirAll(filepath.Join(dir, "elsewhere"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere", "fn-a.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.NewStore(dir).Load(); err == nil || !strings.Contains(err.Error(), "comp/fn-a.json") {
		t.Fatalf("want a wrong-location error naming the expected path, got %v", err)
	}
}

func TestWriteRejectsInvalidNode(t *testing.T) {
	n := fn(t, "fn-a", "comp", 0)
	n.SetWave(-1) // schema minimum is 0
	if err := tree.NewStore(t.TempDir()).Write(n); err == nil {
		t.Fatal("schema-invalid node must not be written")
	}
}

func TestPathLikeIDCannotEscapeTheStore(t *testing.T) {
	raw := `{"spec_version":"2.0","id":"../evil","kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending"}`
	if _, err := tree.ParseNode([]byte(raw)); err == nil {
		t.Fatal("an ID with path separators must be rejected by the schema")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/tree/ 2>&1 | head`
Expected: FAIL, package does not compile.

- [ ] **Step 3: Implement `node.go`**

```go
// Package tree holds the v2 task tree: node documents, the dependency graph
// (cycle check, waves, readiness), and the one-file-per-node store.
package tree

import (
	"encoding/json"

	"gophermind/gophermind-lib/briefv2/schema"
)

type Kind string

const (
	KindRoot      Kind = "root"
	KindComponent Kind = "component"
	KindFunction  Kind = "function"
)

// Node exposes the fields the tree logic needs and keeps the full decoded
// document so nothing else is lost on a round trip.
type Node struct {
	ID        string
	Kind      Kind
	Parent    string
	Children  []string
	DependsOn []string
	Wave      *int

	doc map[string]any
}

// ParseNode validates raw against the node schema and decodes it.
func ParseNode(raw []byte) (Node, error) {
	if err := schema.Validate(schema.KindNode, raw); err != nil {
		return Node{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Node{}, err
	}
	n := Node{doc: doc}
	n.ID, _ = doc["id"].(string)
	k, _ := doc["kind"].(string)
	n.Kind = Kind(k)
	n.Parent, _ = doc["parent"].(string)
	n.Children = strList(doc["children"])
	n.DependsOn = strList(doc["depends_on"])
	if f, ok := doc["wave"].(float64); ok {
		w := int(f)
		n.Wave = &w
	}
	return n, nil
}

func strList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// SetWave records w on both the struct and the document.
func (n *Node) SetWave(w int) {
	n.Wave = &w
	n.doc["wave"] = w
}

// Marshal renders the document as indented JSON.
func (n Node) Marshal() ([]byte, error) { return json.MarshalIndent(n.doc, "", "  ") }

// Path is the node's slash-separated path relative to the run directory.
func (n Node) Path() string {
	switch n.Kind {
	case KindRoot:
		return "root.json"
	case KindComponent:
		return n.ID + "/component.json"
	default:
		return n.Parent + "/" + n.ID + ".json"
	}
}
```

- [ ] **Step 4: Implement `tree.go`**

```go
package tree

import (
	"fmt"
	"sort"
	"strings"
)

type Tree struct{ Nodes map[string]Node }

func NewTree(nodes []Node) (*Tree, error) {
	t := &Tree{Nodes: make(map[string]Node, len(nodes))}
	for _, n := range nodes {
		if _, dup := t.Nodes[n.ID]; dup {
			return nil, fmt.Errorf("tree: duplicate node id %q", n.ID)
		}
		t.Nodes[n.ID] = n
	}
	return t, nil
}

func (t *Tree) ids() []string {
	ids := make([]string, 0, len(t.Nodes))
	for id := range t.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// CycleCheck reports the first dependency cycle as "a -> b -> a". Edges to
// nodes not in the tree are ignored here (ComputeWaves reports those).
func (t *Tree) CycleCheck() error {
	const (
		white = iota
		grey
		black
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case grey:
			i := 0
			for j, s := range stack {
				if s == id {
					i = j
				}
			}
			cycle := append(append([]string{}, stack[i:]...), id)
			return fmt.Errorf("tree: dependency cycle: %s", strings.Join(cycle, " -> "))
		case black:
			return nil
		}
		state[id] = grey
		stack = append(stack, id)
		for _, d := range t.Nodes[id].DependsOn {
			if _, ok := t.Nodes[d]; !ok {
				continue
			}
			if err := visit(d); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = black
		return nil
	}
	for _, id := range t.ids() {
		if state[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ComputeWaves returns wave(n) = 0 with no depends_on, else 1 + max over
// depends_on, for every node kind (deviation D2).
func (t *Tree) ComputeWaves() (map[string]int, error) {
	if err := t.CycleCheck(); err != nil {
		return nil, err
	}
	memo := map[string]int{}
	var wave func(id string) (int, error)
	wave = func(id string) (int, error) {
		if w, ok := memo[id]; ok {
			return w, nil
		}
		w := 0
		for _, d := range t.Nodes[id].DependsOn {
			if _, ok := t.Nodes[d]; !ok {
				return 0, fmt.Errorf("tree: node %q depends on unknown node %q", id, d)
			}
			dw, err := wave(d)
			if err != nil {
				return 0, err
			}
			if dw+1 > w {
				w = dw + 1
			}
		}
		memo[id] = w
		return w, nil
	}
	for _, id := range t.ids() {
		if _, err := wave(id); err != nil {
			return nil, err
		}
	}
	return memo, nil
}

// AssignWaves overwrites every node's wave with the computed value.
func (t *Tree) AssignWaves() error {
	w, err := t.ComputeWaves()
	if err != nil {
		return err
	}
	for id, v := range w {
		n := t.Nodes[id]
		n.SetWave(v)
		t.Nodes[id] = n
	}
	return nil
}

// CheckWaves errors when a node's recorded wave disagrees with the computed one.
func (t *Tree) CheckWaves() error {
	w, err := t.ComputeWaves()
	if err != nil {
		return err
	}
	for _, id := range t.ids() {
		n := t.Nodes[id]
		if n.Wave != nil && *n.Wave != w[id] {
			return fmt.Errorf("tree: node %q has wave %d but depends_on gives %d", id, *n.Wave, w[id])
		}
	}
	return nil
}

// Ready reports whether a node may start. A function needs every depends_on
// verified; a component needs every child verified; the root needs every
// component (its children) verified. Unknown nodes are never ready.
func (t *Tree) Ready(id string, verified func(string) bool) bool {
	n, ok := t.Nodes[id]
	if !ok {
		return false
	}
	need := n.DependsOn
	if n.Kind != KindFunction {
		need = n.Children
	}
	for _, d := range need {
		if !verified(d) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Implement `store.go`**

```go
package tree

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/briefv2/schema"
)

// Store keeps one JSON file per node under dir (a run directory).
type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: dir} }

var notNodes = map[string]bool{"contracts.json": true, "answers.json": true, "approval.json": true, "report.json": true}

// Write validates n against the node schema and writes it atomically.
func (s *Store) Write(n Node) error {
	raw, err := n.Marshal()
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.KindNode, raw); err != nil {
		return fmt.Errorf("tree: node %s: %w", n.ID, err)
	}
	full := filepath.Join(s.dir, filepath.FromSlash(n.Path()))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// WriteAll rejects cycles, recomputes waves, then writes every node.
func (s *Store) WriteAll(t *Tree) error {
	if err := t.AssignWaves(); err != nil {
		return err
	}
	for _, id := range t.ids() {
		if err := s.Write(t.Nodes[id]); err != nil {
			return err
		}
	}
	return nil
}

// Load reads every node file, skipping run artifacts, and rejects a node that
// is stored somewhere other than its own Path().
func (s *Store) Load() (*Tree, error) {
	var nodes []Node
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "logs" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".json") || notNodes[name] || strings.HasSuffix(name, ".runtime.json") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.dir, p)
		rel = filepath.ToSlash(rel)
		n, err := ParseNode(raw)
		if err != nil {
			return fmt.Errorf("tree: %s: %w", rel, err)
		}
		if rel != n.Path() {
			return fmt.Errorf("tree: node %q is stored at %s, expected %s", n.ID, rel, n.Path())
		}
		nodes = append(nodes, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewTree(nodes)
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/tree/ -race -v 2>&1 | tail -30`
Expected: PASS. If `TestExampleSubsetWavesReproduced` fails, the example waves disagree with deviation D2; stop and report the actual numbers.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/tree
git commit -m "feat(briefv2): task tree store with cycle check, wave assignment, readiness"
```

---

### Task 5: Contract loading and dependency slicing

**Files:**
- Create: `gophermind-lib/briefv2/contract/contract.go`, `contract_test.go`, `testdata/register-handler.golden`

**Interfaces:**
- Consumes: `schema.Validate(schema.KindContract, ...)`.
- Produces: `contract.Load(raw []byte) (*Contracts, error)`; `type Contracts` with `Types []Type`, `Functions []Function`, `Components []Component`, `Revision int`; `(*Contracts).Closure(dependsOn []string) (map[string]bool, error)`; `(*Contracts).Slice(dependsOn []string, self string) ([]string, error)`; `contract.Diff(old, new *Contracts) []string` (sorted IDs whose declaration changed, appeared, or disappeared); `(*Contracts).Affected(changed []string, deps map[string][]string) ([]string, error)` (sorted node IDs, from a map of node ID to its `depends_on`, whose closure includes any changed ID).

Slice algorithm (BUILD_PLAN item 4): closure of `depends_on` over `uses`; component IDs in `depends_on` are skipped; an ID that is neither a type, function, nor component is an error; remove `self`; types first then functions; within each group order by `uses` (a decl after what it uses), ties broken by position in `contracts.json`, and a `uses` cycle falls back to file order instead of failing; a type emits its `decl` verbatim; a function emits `// <doc>` line(s) then `signature`; de-duplicate.

- [ ] **Step 1: Write the failing test** (`contract_test.go`)

```go
package contract_test

import (
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
)

const contractsPath = "../testdata/example/tree/gm-2026-09-29-001/contracts.json"

func load(t *testing.T) *contract.Contracts {
	t.Helper()
	raw, err := os.ReadFile(contractsPath)
	if err != nil {
		t.Fatal(err)
	}
	c, err := contract.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var registerDeps = []string{"fn-validate-email", "fn-validate-username", "fn-memory-store-create", "fn-crm-push", "fn-server-new"}

func TestSliceRegisterHandlerMatchesGolden(t *testing.T) {
	got, err := load(t).Slice(registerDeps, "fn-register-handler")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/register-handler.golden")
	if err != nil {
		t.Fatal(err)
	}
	// entries are separated by a line containing only "-----"
	wantEntries := strings.Split(strings.TrimRight(string(want), "\n"), "\n-----\n")
	if len(got) != 10 {
		t.Fatalf("got %d entries, want 10 (4 types + 6 functions): %q", len(got), got)
	}
	for i := range wantEntries {
		if got[i] != wantEntries[i] {
			t.Errorf("entry %d:\n got: %q\nwant: %q", i, got[i], wantEntries[i])
		}
	}
}

func TestSliceOrdersTypesBeforeFunctionsAndDependenciesFirst(t *testing.T) {
	got, _ := load(t).Slice(registerDeps, "fn-register-handler")
	idx := func(sub string) int {
		for i, s := range got {
			if strings.Contains(s, sub) {
				return i
			}
		}
		t.Fatalf("no entry containing %q", sub)
		return -1
	}
	if !(idx("type ValidationError") < idx("type User") && idx("type User") < idx("type Store interface")) {
		t.Error("types must follow their uses")
	}
	if !(idx("type Store interface") < idx("func ValidateEmail")) {
		t.Error("all types come before any function")
	}
	if !(idx("func New(baseURL") < idx("func (c *Client) Push")) {
		t.Error("fn-crm-new must precede fn-crm-push, which uses it")
	}
}

func TestSliceRemovesSelfSkipsComponentsAndErrorsOnUnknown(t *testing.T) {
	c := load(t)
	got, err := c.Slice([]string{"fn-validate-email"}, "fn-validate-email")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if strings.Contains(s, "func ValidateEmail") {
			t.Error("a node must not list its own signature")
		}
	}
	if _, err := c.Slice([]string{"types"}, "x"); err != nil {
		t.Errorf("a component ID in depends_on is skipped, got %v", err)
	}
	if _, err := c.Slice([]string{"fn-nope"}, "x"); err == nil || !strings.Contains(err.Error(), "fn-nope") {
		t.Errorf("want an unknown-ID error, got %v", err)
	}
}

func TestSliceTerminatesOnUsesCycle(t *testing.T) {
	raw := `{"spec_version":"2.0","brief_id":"gm-2026-09-29-001","revision":0,"module":"m",
"conventions":{"layout":["l"],"naming":["n"],"errors":"e"},
"types":[{"id":"ta","package":"p","file":"p/a.go","decl":"type A struct{ B *B }","uses":["tb"]},
         {"id":"tb","package":"p","file":"p/a.go","decl":"type B struct{ A *A }","uses":["ta"]}],
"functions":[{"id":"fn-x","package":"p","file":"p/x.go","signature":"func X(a A)","doc":"X does x.","uses":["ta"]}],
"components":[{"id":"c","package":"p","exports":["fn-x"]}]}`
	c, err := contract.Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Slice([]string{"fn-x"}, "fn-y")
	if err != nil || len(got) != 3 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLoadRejectsDanglingUses(t *testing.T) {
	raw, _ := os.ReadFile(contractsPath)
	bad := strings.Replace(string(raw), `"uses": ["user"]`, `"uses": ["ghost"]`, 1)
	if _, err := contract.Load([]byte(bad)); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want a dangling-uses error naming ghost, got %v", err)
	}
}

func TestDiffAndAffected(t *testing.T) {
	old := load(t)
	raw, _ := os.ReadFile(contractsPath)
	changed := strings.Replace(string(raw), "Code    string", "Code    string // changed", 1)
	nw, err := contract.Load([]byte(changed))
	if err != nil {
		t.Fatal(err)
	}
	diff := contract.Diff(old, nw)
	if len(diff) != 1 || diff[0] != "validation-error" {
		t.Fatalf("Diff = %v", diff)
	}
	deps := map[string][]string{
		"fn-register-handler": registerDeps,
		"fn-validate-email":   {"fn-validation-error-error"},
		"fn-healthz-handler":  {"fn-server-new"},
		"fn-crm-new":          {},
	}
	aff, err := old.Affected(diff, deps)
	if err != nil {
		t.Fatal(err)
	}
	want := "fn-register-handler fn-validate-email"
	if got := strings.Join(aff, " "); got != want {
		t.Errorf("Affected = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Create the golden file**

Create `testdata/register-handler.golden` by running the implementation once it exists (Step 4) and then **reviewing every entry by hand against `contracts.json`** before committing. The expected 10 entries, in order, are: types `validation-error`, `validation-codes`, `user`, `store-interface` (each `decl` verbatim), then functions `fn-validate-email`, `fn-validate-username`, `fn-memory-store-create`, `fn-crm-new`, `fn-crm-push`, `fn-server-new`, each as `// <doc>` newline `<signature>`. Entries are separated by a line containing only `-----`. Do not regenerate the golden to make a failing test pass without re-reviewing it.

- [ ] **Step 3: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/contract/ 2>&1 | head`
Expected: FAIL, package does not compile.

- [ ] **Step 4: Implement** (`contract.go`)

```go
// Package contract loads the Wave 0 contracts.json artifact and derives each
// node's dependency_signatures from it. Models never write signatures; the
// harness slices them here.
package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/schema"
)

type Type struct {
	ID      string   `json:"id"`
	Package string   `json:"package"`
	File    string   `json:"file"`
	Decl    string   `json:"decl"`
	Uses    []string `json:"uses"`
}

type Function struct {
	ID        string   `json:"id"`
	Package   string   `json:"package"`
	File      string   `json:"file"`
	Signature string   `json:"signature"`
	Doc       string   `json:"doc"`
	Uses      []string `json:"uses"`
	Component string   `json:"component"`
}

type Component struct {
	ID      string   `json:"id"`
	Package string   `json:"package"`
	Exports []string `json:"exports"`
}

type Contracts struct {
	BriefID    string      `json:"brief_id"`
	Revision   int         `json:"revision"`
	Module     string      `json:"module"`
	Types      []Type      `json:"types"`
	Functions  []Function  `json:"functions"`
	Components []Component `json:"components"`

	typeIdx map[string]int
	fnIdx   map[string]int
	compSet map[string]bool
}

// Load validates raw against the contract schema and checks that every uses
// and exports entry resolves to a declared type or function.
func Load(raw []byte) (*Contracts, error) {
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		return nil, err
	}
	var c Contracts
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	c.typeIdx, c.fnIdx, c.compSet = map[string]int{}, map[string]int{}, map[string]bool{}
	for i, t := range c.Types {
		if _, dup := c.typeIdx[t.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", t.ID)
		}
		c.typeIdx[t.ID] = i
	}
	for i, f := range c.Functions {
		if _, dup := c.typeIdx[f.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", f.ID)
		}
		if _, dup := c.fnIdx[f.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", f.ID)
		}
		c.fnIdx[f.ID] = i
	}
	for _, comp := range c.Components {
		c.compSet[comp.ID] = true
	}
	for _, t := range c.Types {
		for _, u := range t.Uses {
			if !c.declared(u) {
				return nil, fmt.Errorf("contract: %s uses unknown id %q", t.ID, u)
			}
		}
	}
	for _, f := range c.Functions {
		for _, u := range f.Uses {
			if !c.declared(u) {
				return nil, fmt.Errorf("contract: %s uses unknown id %q", f.ID, u)
			}
		}
	}
	for _, comp := range c.Components {
		for _, e := range comp.Exports {
			if _, ok := c.fnIdx[e]; !ok {
				return nil, fmt.Errorf("contract: component %s exports unknown function %q", comp.ID, e)
			}
		}
	}
	return &c, nil
}

func (c *Contracts) declared(id string) bool {
	_, t := c.typeIdx[id]
	_, f := c.fnIdx[id]
	return t || f
}

func (c *Contracts) uses(id string) []string {
	if i, ok := c.typeIdx[id]; ok {
		return c.Types[i].Uses
	}
	if i, ok := c.fnIdx[id]; ok {
		return c.Functions[i].Uses
	}
	return nil
}

// Closure returns every type and function ID reachable from dependsOn through
// uses, including the dependsOn entries themselves. Component IDs are skipped;
// any other unknown ID is an error.
func (c *Contracts) Closure(dependsOn []string) (map[string]bool, error) {
	seen := map[string]bool{}
	stack := append([]string{}, dependsOn...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		if c.compSet[id] && !c.declared(id) {
			continue
		}
		if !c.declared(id) {
			return nil, fmt.Errorf("contract: unknown id %q", id)
		}
		seen[id] = true
		stack = append(stack, c.uses(id)...)
	}
	return seen, nil
}

// Slice returns the dependency_signatures for a node with the given
// depends_on. self is removed from the result.
func (c *Contracts) Slice(dependsOn []string, self string) ([]string, error) {
	closure, err := c.Closure(dependsOn)
	if err != nil {
		return nil, err
	}
	delete(closure, self)

	var typeIDs, fnIDs []string
	for _, t := range c.Types {
		if closure[t.ID] {
			typeIDs = append(typeIDs, t.ID)
		}
	}
	for _, f := range c.Functions {
		if closure[f.ID] {
			fnIDs = append(fnIDs, f.ID)
		}
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, id := range order(typeIDs, c.uses) {
		add(c.Types[c.typeIdx[id]].Decl)
	}
	for _, id := range order(fnIDs, c.uses) {
		f := c.Functions[c.fnIdx[id]]
		add(docComment(f.Doc) + f.Signature)
	}
	return out, nil
}

func docComment(doc string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		lines[i] = "// " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// order sorts ids (given in contracts.json order) so a decl follows the
// members of ids it uses. Ties keep file order. A cycle is broken by taking
// the first remaining id in file order rather than failing.
func order(ids []string, uses func(string) []string) []string {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	done := map[string]bool{}
	var out []string
	for len(out) < len(ids) {
		pick := ""
		for _, id := range ids {
			if done[id] {
				continue
			}
			ready := true
			for _, u := range uses(id) {
				if in[u] && !done[u] && u != id {
					ready = false
					break
				}
			}
			if ready {
				pick = id
				break
			}
		}
		if pick == "" {
			for _, id := range ids {
				if !done[id] {
					pick = id
					break
				}
			}
		}
		done[pick] = true
		out = append(out, pick)
	}
	return out
}

// Diff lists IDs whose declaration changed, was added, or was removed.
func Diff(old, nw *Contracts) []string {
	sig := func(c *Contracts) map[string]string {
		m := map[string]string{}
		for _, t := range c.Types {
			m[t.ID] = "T\x00" + t.Decl
		}
		for _, f := range c.Functions {
			m[f.ID] = "F\x00" + f.Signature + "\x00" + f.Doc
		}
		return m
	}
	a, b := sig(old), sig(nw)
	set := map[string]bool{}
	for id, v := range a {
		if b[id] != v {
			set[id] = true
		}
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			set[id] = true
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Affected returns the node IDs (keys of deps, mapping node ID to its
// depends_on) whose closure includes any changed ID.
func (c *Contracts) Affected(changed []string, deps map[string][]string) ([]string, error) {
	chg := map[string]bool{}
	for _, id := range changed {
		chg[id] = true
	}
	var out []string
	for node, d := range deps {
		cl, err := c.Closure(d)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}
		for id := range cl {
			if chg[id] {
				out = append(out, node)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
```

- [ ] **Step 5: Generate and hand-review the golden file**

Run a throwaway snippet (do not commit it) that prints `Slice(registerDeps, "fn-register-handler")` joined with `"\n-----\n"` into `testdata/register-handler.golden`. Compare each of the 10 entries with `contracts.json` by eye, per Step 2.

- [ ] **Step 6: Run to verify it passes**

Run: `cd gophermind-lib && go test ./briefv2/contract/ -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/contract
git commit -m "feat(briefv2): contract loading and dependency signature slicing"
```

---

### Task 6: Run directory, `gophermind brief` commands, docs

**Files:**
- Create: `gophermind-lib/briefv2/rundir/rundir.go`, `rundir_test.go`, `cmd/gophermind/brief.go`, `cmd/gophermind/brief_test.go`, `docs/briefv2/README.md`
- Modify: `cmd/gophermind/main.go` (one dispatch line next to the `free` block, before `cfg.Validate()`)

**Interfaces:**
- Consumes: `brief.Parse`, `vault.*`, `tree.NewStore(...).Load()`, `config.Dir()` (from `gophermind/gophermind-lib/config`).
- Produces: `rundir.Create(repoRoot, briefID string, briefSrc []byte) (string, error)` and `rundir.ErrExists`; `runBrief(args []string, in *os.File, out, errw io.Writer) int` with exit codes 0 ok, 1 error, 2 invalid brief.
- Commands: `gophermind brief validate <brief.md>`, `gophermind brief vault set <NAME>`, `gophermind brief vault list`, `gophermind brief tree check <run-dir>`. Everything else prints usage and exits 1 (the run, resume, status, report subcommands arrive in later plans). Deviation D1.

- [ ] **Step 1: Write the failing rundir test** (`rundir_test.go`)

```go
package rundir_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/rundir"
)

func TestCreateLayoutAndExclude(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("# brief"))
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(repo, ".gophermind", "gm-2026-09-29-001") {
		t.Errorf("dir = %s", dir)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "brief.md")); string(b) != "# brief" {
		t.Error("brief.md not copied")
	}
	if fi, err := os.Stat(filepath.Join(dir, "logs")); err != nil || !fi.IsDir() {
		t.Error("logs/ missing")
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Errorf("exclude = %q", ex)
	}
	if _, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("x")); !errors.Is(err, rundir.ErrExists) {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
	ex, _ = os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Error("exclude entry duplicated")
	}
}

func TestCreateRejectsBadID(t *testing.T) {
	for _, id := range []string{"", "../x", "gm-1", "gm-2026-09-29-001/../../x"} {
		if _, err := rundir.Create(t.TempDir(), id, nil); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
}

func TestCreateWithoutGitDirIsFine(t *testing.T) {
	if _, err := rundir.Create(t.TempDir(), "gm-2026-09-29-001", []byte("x")); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Implement rundir** (`rundir.go`)

```go
// Package rundir creates the .gophermind/<brief-id>/ layout inside a target
// repository and keeps it out of git via .git/info/exclude (never the repo's
// tracked .gitignore).
package rundir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrExists = errors.New("rundir: run directory already exists")

var idRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

// Create makes <repoRoot>/.gophermind/<briefID>/ with logs/ and a copy of the
// brief. It refuses to reuse an existing run directory.
func Create(repoRoot, briefID string, briefSrc []byte) (string, error) {
	if !idRE.MatchString(briefID) {
		return "", fmt.Errorf("rundir: invalid brief id %q", briefID)
	}
	dir := filepath.Join(repoRoot, ".gophermind", briefID)
	if _, err := os.Stat(dir); err == nil {
		return "", ErrExists
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), briefSrc, 0o600); err != nil {
		return "", err
	}
	if err := exclude(repoRoot); err != nil {
		return "", err
	}
	return dir, nil
}

func exclude(repoRoot string) error {
	info := filepath.Join(repoRoot, ".git", "info")
	if fi, err := os.Stat(info); err != nil || !fi.IsDir() {
		return nil
	}
	p := filepath.Join(info, "exclude")
	cur, _ := os.ReadFile(p)
	if strings.Contains(string(cur), ".gophermind/") {
		return nil
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(cur) > 0 && !strings.HasSuffix(string(cur), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + ".gophermind/\n")
	return err
}
```

Run: `cd gophermind-lib && go test ./briefv2/rundir/ -v`. Expected: PASS.

- [ ] **Step 3: Write the failing CLI test** (`cmd/gophermind/brief_test.go`)

```go
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
	if !strings.Contains(errs, "warning") || !strings.Contains(errs, "EMAIL_INVALID") {
		t.Errorf("expected undeclared-secret warnings on stderr, got %q", errs)
	}
	if strings.Contains(errs, "CRM_API_KEY") {
		t.Error("declared secret must not warn")
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
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("canary-9f8e7d")) {
		t.Error("vault file contains the plaintext value")
	}
	if strings.Contains(out+errs, "canary-9f8e7d") {
		t.Error("value leaked to output")
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
```

- [ ] **Step 4: Implement the command group** (`cmd/gophermind/brief.go`)

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/config"
)

// vaultOptions is a variable so tests can lower the scrypt work factor.
var vaultOptions vault.Options

const briefUsage = `usage:
  gophermind brief validate <brief.md>
  gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
  gophermind brief vault list
  gophermind brief tree check <run-dir>`

// runBrief implements `gophermind brief ...` and returns the process exit
// code: 0 ok, 1 error, 2 invalid brief.
func runBrief(args []string, in *os.File, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, briefUsage)
		return 1
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			fmt.Fprintln(errw, briefUsage)
			return 1
		}
		return briefValidate(args[1], out, errw)
	case "vault":
		return briefVault(args[1:], in, out, errw)
	case "tree":
		if len(args) == 3 && args[1] == "check" {
			return briefTreeCheck(args[2], out, errw)
		}
	}
	fmt.Fprintln(errw, briefUsage)
	return 1
}

func briefValidate(path string, out, errw io.Writer) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	b, err := brief.Parse(src)
	if err != nil {
		var inv *brief.InvalidError
		if errors.As(err, &inv) {
			fmt.Fprintf(errw, "invalid brief: %v\n", err)
			return 2
		}
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	for _, w := range b.UndeclaredSecrets() {
		fmt.Fprintf(errw, "warning: line %d: %s looks like a secret name but is not declared under secrets\n", w.Line, w.Token)
	}
	fmt.Fprintf(out, "ok: %s (%s), %d features\n", b.Front.ID, b.Front.Title, len(b.Features))
	return 0
}

func vaultPath() (string, error) {
	if p := os.Getenv("GOPHERMIND_VAULT_PATH"); p != "" {
		return p, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.age"), nil
}

func briefVault(args []string, in *os.File, out, errw io.Writer) int {
	if len(args) == 0 || (args[0] == "set" && len(args) != 2) || (args[0] == "list" && len(args) != 1) || (args[0] != "set" && args[0] != "list") {
		fmt.Fprintln(errw, briefUsage)
		return 1
	}
	path, err := vaultPath()
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	pass, err := vault.Passphrase("Vault passphrase: ", in, errw)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	v, err := vault.Open(path, pass, vaultOptions)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if args[0] == "list" {
		for _, n := range v.Names(vault.HarnessScope) {
			fmt.Fprintln(out, n)
		}
		return 0
	}
	val, err := vault.ReadSecret("Value for "+args[1]+": ", in, errw)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if err := v.Set(vault.HarnessScope, args[1], val); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "stored %s\n", args[1])
	return 0
}

func briefTreeCheck(dir string, out, errw io.Writer) int {
	tr, err := tree.NewStore(dir).Load()
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	if err := tr.CheckWaves(); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return 1
	}
	waves, _ := tr.ComputeWaves()
	top := 0
	for _, w := range waves {
		if w > top {
			top = w
		}
	}
	fmt.Fprintf(out, "ok: %d nodes, waves 0-%d\n", len(tr.Nodes), top)
	return 0
}
```

Note: when `vault list`/`set` reads the passphrase from a pipe on `in`, the value for `set` is read from the same pipe. In production the passphrase comes from the terminal or `GOPHERMIND_VAULT_PASSPHRASE`, so the stdin pipe carries only the value. Do not "fix" this by reading two lines.

- [ ] **Step 5: Wire the dispatch** (`cmd/gophermind/main.go`, immediately after the `if cmd == "free" { ... }` block and before `cfg.Validate()`)

```go
	// `gophermind brief ...` (v2 brief loader, vault, tree tools) runs before
	// Validate for the same reason as `free`: it needs no configured endpoint.
	if cmd == "brief" {
		os.Exit(runBrief(args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
```

- [ ] **Step 6: Run everything**

```bash
cd /Users/jbrahy/OtherProjects/PMSLLC/gophermind.com
go build ./... && go vet ./cmd/... ./gophermind-lib/briefv2/...
go test ./cmd/gophermind/ -run TestBrief -v 2>&1 | tail -25
(cd gophermind-lib && go test ./briefv2/... -race 2>&1 | tail -10)
gofmt -l cmd gophermind-lib/briefv2   # must print nothing
```

Expected: all PASS, `gofmt -l` prints nothing. The repo's pre-push gate also runs `gofmt` over the tree, so this matters.

- [ ] **Step 7: Smoke test the real binary**

```bash
go run ./cmd/gophermind brief validate gophermind-lib/briefv2/testdata/example/brief.md; echo "exit=$?"
```

Expected: five `warning:` lines, then `ok: gm-2026-09-29-001 (Acme Registration API), 1 features`, `exit=0` (the feature count is whatever the example has under `## Features`; it must match `len(Features)`).

- [ ] **Step 8: Write `docs/briefv2/README.md`**

Contents (plain text, no em dashes, no emojis): what v2 is in two sentences; the four commands from `briefUsage`; the package map from File Structure; the exit codes; the vault scopes and `GOPHERMIND_VAULT_PATH` / `GOPHERMIND_VAULT_PASSPHRASE`; and the D1 to D7 table copied from this plan so the deviations live next to the code.

- [ ] **Step 9: Commit**

```bash
git add gophermind-lib/briefv2/rundir cmd/gophermind/brief.go cmd/gophermind/brief_test.go cmd/gophermind/main.go docs/briefv2/README.md
git commit -m "feat(briefv2): gophermind brief validate, vault, tree check, and run directory"
```

---

## Checkpoint report (after Task 6, before any later item)

Report to John, in this order: (1) what was built and the test evidence; (2) each deviation D1 to D7 with the actual numbers observed; (3) the open question that the shipped `contracts.json` lacks `Server` and `CRM` types that `fn-server-new` depends on; (4) whether the go-git `Git` interface and the config-format decision are ready to be planned next. Do not start item 5 without John's go-ahead.

## Self-Review

**Spec coverage (BUILD_PLAN items 1 to 4):**
- Item 1 (CLI, run dir): Task 6. Exit codes 0 and 2 pinned by tests; `validate` python and missing-`spec_version` cases covered. Exit codes 3 and 4 belong to human gates (item 8), out of scope.
- Item 2 (loader, vault): Tasks 2 and 3. Secret-name scan, stoplist, scopes, echo-off prompt, env-var bypass, no-plaintext-at-rest. The "grep after an end-to-end run" test needs a run, so it is deferred to the executor plan; the at-rest and Env tests here cover the parts that exist.
- Item 3 (schema, tree, waves): Tasks 1 and 4. Embedded schemas, validate-on-write, path layout, DFS cycle check naming both IDs, wave assignment, hand-set disagreement error, readiness.
- Item 4 (contracts, slicing): Task 5. Closure, ordering, self removal, `Diff`/`Affected`. Marking dependents `needs_revision` on the blackboard is item 6 and is not included; `Affected` provides the input.

**Placeholder scan:** the only deferred content is the golden file, which Step 5 of Task 5 tells the engineer to generate and hand-review with the exact expected contents listed; the README in Task 6 Step 8 is described by required content. No TBD or "handle edge cases" steps.

**Type consistency:** `schema.Kind*`, `tree.Node`/`Tree`/`Store`, `contract.Contracts.Slice/Closure/Diff/Affected`, `vault.Options/Open/Set/Get/Names/Env/Passphrase/ReadSecret`, `brief.Parse/InvalidError/UndeclaredSecrets`, `rundir.Create/ErrExists`, `runBrief`, and `vaultOptions` are used with the same names and signatures across tasks.

**Review Focus coverage:** CRLF/BOM and fenced headings (Task 2 tests), wrong passphrase and concurrent sets (Task 3), wrong directory and path-like IDs (Task 4), uses cycle, unknown ID, self removal (Task 5).
