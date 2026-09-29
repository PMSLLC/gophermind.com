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
