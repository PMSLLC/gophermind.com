package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func followContract() *contract.Contracts {
	return &contract.Contracts{
		Module: "example.com/linkcheck",
		Functions: []contract.Function{
			{ID: "fn_follow", Package: "internal/check", File: "internal/check/follow.go", Signature: "func follow(ctx context.Context, client *http.Client, url string, depth int) (string, string, string)"},
		},
	}
}

func TestEscapeGlitchFromLinkcheckIsAbsorbedAndOnlyNextIsRefused(t *testing.T) {
	if _, err := lookGo("go"); err != nil {
		t.Skip("no go tool")
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "escape-glitch", "fn_follow_test.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	fixed := nudgeSyntax(unescapeQuotedSource(src))
	if !parsesAsGo(fixed) {
		t.Fatal("the glitched file does not parse after the existing repairs")
	}
	err = compileCheckTestFile(followContract(), "internal/check/fn_follow_test.go", fixed, nil)
	if err == nil || !strings.Contains(err.Error(), "declared and not used: next") || strings.Contains(err.Error(), "line 108") {
		t.Fatalf("refusal = %v, want exactly the unused next", err)
	}
}
