package tips

import (
	"strings"
	"testing"
)

func TestRandomReturnsATip(t *testing.T) {
	got := Random()
	if strings.TrimSpace(got) == "" {
		t.Fatal("Random() returned empty")
	}
	found := false
	for _, tip := range all {
		if tip == got {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Random() returned a tip not in the set: %q", got)
	}
}

func TestAllTipsNonEmpty(t *testing.T) {
	if len(all) == 0 {
		t.Fatal("no tips defined")
	}
	for i, tip := range all {
		if strings.TrimSpace(tip) == "" {
			t.Errorf("tip %d is empty", i)
		}
	}
}
