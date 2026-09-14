package fortune

import (
	"slices"
	"testing"
)

func TestParseSplitsOnPercentLines(t *testing.T) {
	got := parse("alpha\n%\nbeta\nbeta2\n%\ngamma\n")
	want := []string{"alpha", "beta\nbeta2", "gamma"}
	if !slices.Equal(got, want) {
		t.Errorf("parse = %q, want %q", got, want)
	}
}

func TestParseDropsEmptyEntries(t *testing.T) {
	got := parse("%\n\n%\nonly\n%\n")
	want := []string{"only"}
	if !slices.Equal(got, want) {
		t.Errorf("parse = %q, want %q", got, want)
	}
}

func TestEmbeddedDatabaseHasManyFortunes(t *testing.T) {
	if Count() < 2000 {
		t.Errorf("Count() = %d, want >= 2000 (embedded db not parsed?)", Count())
	}
}

func TestRandomReturnsAKnownNonEmptyFortune(t *testing.T) {
	f := Random()
	if f == "" {
		t.Fatal("Random() returned empty")
	}
	if !slices.Contains(fortunes, f) {
		t.Errorf("Random() returned %q which is not in the database", f)
	}
}
