package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFactsRoundTripAndTrim(t *testing.T) {
	r := newRepo(t)
	if got, err := ReadFacts(r); err != nil || got != "" {
		t.Fatalf("no facts: %q, %v", got, err)
	}
	if err := WriteFacts(r, "  Go 1.25\n  test: go test ./...\n\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFacts(r)
	if err != nil || got != "Go 1.25\n  test: go test ./..." {
		t.Errorf("ReadFacts = %q, %v", got, err)
	}
}

func TestReadFactsIsBounded(t *testing.T) {
	r := newRepo(t)
	if err := WriteFacts(r, strings.Repeat("x", 50_000)); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadFacts(r)
	if len(got) > FactsCapBytes+3 || !strings.HasSuffix(got, "...") {
		t.Errorf("len=%d, want at most %d plus the marker", len(got), FactsCapBytes)
	}
	if err := WriteFacts(r, strings.Repeat("\U0001D11E", 5000)); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFacts(r); !utf8.ValidString(got) || len(got) > FactsCapBytes+3 {
		t.Errorf("multibyte facts: valid=%v len=%d", utf8.ValidString(got), len(got))
	}
}
