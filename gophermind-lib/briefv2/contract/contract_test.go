package contract_test

import (
	"encoding/json"
	"os"
	"reflect"
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

func synth(types, functions string) string {
	return `{"spec_version":"2.0","brief_id":"gm-2026-09-29-001","revision":0,"module":"m",
"conventions":{"layout":["l"],"naming":["n"],"errors":"e"},
"types":[` + types + `],"functions":[` + functions + `],
"components":[{"id":"c","package":"p","exports":["fn-x"]}]}`
}

func loadRaw(t *testing.T, raw string) *contract.Contracts {
	t.Helper()
	c, err := contract.Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const fnX = `{"id":"fn-x","package":"p","file":"p/x.go","signature":"func X(a A)","doc":"X does x.","uses":["ta"]}`

func TestSliceTerminatesOnUsesCycle(t *testing.T) {
	c := loadRaw(t, synth(
		`{"id":"ta","package":"p","file":"p/a.go","decl":"type A struct{ B *B }","uses":["tb"]},
         {"id":"tb","package":"p","file":"p/a.go","decl":"type B struct{ A *A }","uses":["ta"]}`, fnX))
	got, err := c.Slice([]string{"fn-x"}, "fn-y")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"type A struct{ B *B }", "type B struct{ A *A }", "// X does x.\nfunc X(a A)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSliceSelfLoopAndThreeCycle(t *testing.T) {
	c := loadRaw(t, synth(
		`{"id":"ts","package":"p","file":"p/a.go","decl":"type S struct{ N *S }","uses":["ts"]},
         {"id":"t1","package":"p","file":"p/a.go","decl":"type One struct{}","uses":["t2"]},
         {"id":"t2","package":"p","file":"p/a.go","decl":"type Two struct{}","uses":["t3"]},
         {"id":"t3","package":"p","file":"p/a.go","decl":"type Three struct{}","uses":["t1"]}`,
		`{"id":"fn-x","package":"p","file":"p/x.go","signature":"func X()","doc":"X does x.","uses":["ts","t2"]}`))
	got, err := c.Slice([]string{"fn-x"}, "fn-y")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"type S struct{ N *S }", "type One struct{}", "type Three struct{}", "type Two struct{}", "// X does x.\nfunc X()"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSliceOrdersByUsesWhenFileOrderIsReversed(t *testing.T) {
	c := loadRaw(t, synth(
		`{"id":"tb","package":"p","file":"p/a.go","decl":"type B struct{ A A }","uses":["ta"]},
         {"id":"ta","package":"p","file":"p/a.go","decl":"type A struct{}","uses":[]}`,
		`{"id":"fn-b","package":"p","file":"p/x.go","signature":"func B()","doc":"B.","uses":["fn-a"]},
         {"id":"fn-a","package":"p","file":"p/x.go","signature":"func A(b B)","doc":"A.","uses":["tb"]},
         {"id":"fn-c","package":"p","file":"p/x.go","signature":"func C()","doc":"C.","uses":["fn-b"]},
         {"id":"fn-x","package":"p","file":"p/x.go","signature":"func X()","doc":"X.","uses":[]}`))
	got, err := c.Slice([]string{"fn-c"}, "fn-z")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"type A struct{}", "type B struct{ A A }", "// A.\nfunc A(b B)", "// B.\nfunc B()", "// C.\nfunc C()"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSliceEmptyClosureIsNonNilEmptyArray(t *testing.T) {
	c := load(t)
	cases := map[string]struct {
		deps []string
		self string
	}{
		"empty":     {nil, "x"},
		"component": {[]string{"types"}, "x"},
		"self only": {[]string{"fn-crm-new"}, "fn-crm-new"},
	}
	for name, tc := range cases {
		got, err := c.Slice(tc.deps, tc.self)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("%s: got %#v, want non-nil empty", name, got)
		}
		b, _ := json.Marshal(got)
		if string(b) != "[]" {
			t.Errorf("%s: json = %s, want []", name, b)
		}
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

func mutate(t *testing.T, from, to string) *contract.Contracts {
	t.Helper()
	raw, _ := os.ReadFile(contractsPath)
	if !strings.Contains(string(raw), from) {
		t.Fatalf("fixture lacks %q", from)
	}
	return loadRaw(t, strings.Replace(string(raw), from, to, 1))
}

func TestDiffReportsUsesOnlyChange(t *testing.T) {
	nw := mutate(t, `"uses": ["validation-error", "validation-codes"]`, `"uses": ["validation-error"]`)
	diff := contract.Diff(load(t), nw)
	if !reflect.DeepEqual(diff, []string{"fn-validate-email"}) {
		t.Fatalf("Diff = %v", diff)
	}
	deps := map[string][]string{"fn-register-handler": registerDeps, "fn-crm-new": {}}
	aff, err := nw.Affected(diff, deps)
	if err != nil || !reflect.DeepEqual(aff, []string{"fn-register-handler"}) {
		t.Fatalf("Affected = %v, %v", aff, err)
	}
}

func TestDiffReportsUsesReorder(t *testing.T) {
	nw := mutate(t, `"uses": ["validation-error", "validation-codes"]`, `"uses": ["validation-codes", "validation-error"]`)
	if diff := contract.Diff(load(t), nw); !reflect.DeepEqual(diff, []string{"fn-validate-email"}) {
		t.Fatalf("Diff = %v", diff)
	}
}

func TestDiffReportsPackageOnlyChange(t *testing.T) {
	nw := mutate(t, "\"id\": \"user\",\n      \"package\": \"store\"", "\"id\": \"user\",\n      \"package\": \"store2\"")
	if diff := contract.Diff(load(t), nw); !reflect.DeepEqual(diff, []string{"user"}) {
		t.Fatalf("Diff = %v", diff)
	}
}

func TestAffectedReportsNodeDependingOnRemovedID(t *testing.T) {
	nw := load(t)
	deps := map[string][]string{
		"fn-old":  {"fn-removed"},
		"fn-live": {"fn-crm-new"},
	}
	aff, err := nw.Affected([]string{"fn-removed"}, deps)
	if err != nil {
		t.Fatalf("Affected must not fail on an id absent from the receiver: %v", err)
	}
	if !reflect.DeepEqual(aff, []string{"fn-old"}) {
		t.Errorf("Affected = %v, want [fn-old]", aff)
	}
}
