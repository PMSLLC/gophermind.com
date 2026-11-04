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
