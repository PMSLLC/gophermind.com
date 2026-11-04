package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func TestLocalChecksRefusesAPackageNamedLikeTheStandardLibrary(t *testing.T) {
	c := &contract.Contracts{
		BriefID:    "b",
		Components: []contract.Component{{ID: "io", Package: "io"}},
		Types: []contract.Type{{ID: "io-writer", Package: "io", File: "io/io.go",
			Decl: "type Writer interface {\n\tWrite(p []byte) (n int, err error)\n}"}},
	}
	err := localChecks(c, "b")
	if err == nil || !strings.Contains(err.Error(), "package io is named like the standard library package io") || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("localChecks = %v, want the shadow rule naming io", err)
	}
}

func TestStdShadowLooksAtTheLastPathElement(t *testing.T) {
	refused := &contract.Contracts{Components: []contract.Component{{ID: "web", Package: "internal/http"}}}
	if err := stdShadow(refused); err == nil || !strings.Contains(err.Error(), "net/http") {
		t.Errorf("internal/http: err = %v, want a refusal naming net/http", err)
	}
	ok := &contract.Contracts{
		Components: []contract.Component{{ID: "types", Package: "internal/types"}, {ID: "main", Package: "cmd/linkcheck"}, {ID: "root", Package: "."}},
		Functions:  []contract.Function{{ID: "fn-all", Package: "internal/check"}},
	}
	if err := stdShadow(ok); err != nil {
		t.Errorf("allowed packages refused: %v", err)
	}
}

func TestStdShadowAllowsADomainNameThatIsAlsoADeepStdlibPackage(t *testing.T) {
	ok := &contract.Contracts{Components: []contract.Component{{ID: "u", Package: "internal/user"}, {ID: "l", Package: "internal/list"}}}
	if err := stdShadow(ok); err != nil {
		t.Errorf("domain names refused: %v", err)
	}
	for _, pkg := range []string{"internal/json", "internal/http"} {
		c := &contract.Contracts{Components: []contract.Component{{ID: "x", Package: pkg}}}
		if err := stdShadow(c); err == nil {
			t.Errorf("%s: want a refusal", pkg)
		}
	}
}

// A package under cmd/ is package main, and siblingImports keys packages by
// their clause, so it can never take over a qualifier. The harness itself
// writes cmd/<program>.
func TestStdShadowSparesMainPackages(t *testing.T) {
	for _, pkg := range []string{"cmd/json", "cmd/sort", "cmd/csv"} {
		c := &contract.Contracts{Components: []contract.Component{{ID: "main", Package: pkg}}}
		if err := stdShadow(c); err != nil {
			t.Errorf("%s: refused: %v", pkg, err)
		}
	}
}

// StdQualifiers has no rand key because ResolveQualifier special-cases it, yet
// a contract rand package still wins over math/rand.
func TestStdShadowRefusesRand(t *testing.T) {
	c := &contract.Contracts{Components: []contract.Component{{ID: "r", Package: "internal/rand"}}}
	if err := stdShadow(c); err == nil || !strings.Contains(err.Error(), "math/rand") {
		t.Errorf("internal/rand: err = %v, want a refusal naming math/rand", err)
	}
}

// A model lists a standard library package in a function's uses (run
// linkcheck-v1-20261104-r003: fn-ctx used "context"). The repair pass must
// not ask for that id: the model declares a package named context and the
// shadow rule refuses it, and the loop ends the run after two passes.
func TestUnresolvedSkipsNamesTheShadowRuleWouldRefuse(t *testing.T) {
	doc := map[string]any{
		"types":     []any{map[string]any{"id": "result", "uses": []any{}}},
		"functions": []any{map[string]any{"id": "fn-ctx", "uses": []any{"context", "http", "rand", "result", "missing-type", "user"}}},
	}
	got := unresolvedFinal(doc)
	want := []string{"missing-type", "user"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unresolved = %v, want %v", got, want)
	}
}
