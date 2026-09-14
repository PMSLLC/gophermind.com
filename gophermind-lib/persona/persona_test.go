package persona

import (
	"strings"
	"testing"
)

func TestPresetKnown(t *testing.T) {
	for _, name := range []string{"reviewer", "architect", "tester"} {
		text, ok := Preset(name)
		if !ok {
			t.Errorf("%q should be a known persona", name)
		}
		if strings.TrimSpace(text) == "" {
			t.Errorf("%q preset text is empty", name)
		}
	}
}

func TestPresetCaseInsensitive(t *testing.T) {
	if _, ok := Preset("Reviewer"); !ok {
		t.Error("persona lookup should be case-insensitive")
	}
}

func TestPresetUnknown(t *testing.T) {
	if _, ok := Preset("wizard"); ok {
		t.Error("unknown persona should not resolve")
	}
	if _, ok := Preset(""); ok {
		t.Error("empty persona should not resolve")
	}
}

func TestNamesSortedAndComplete(t *testing.T) {
	names := Names()
	if len(names) != 3 {
		t.Fatalf("want 3 personas, got %v", names)
	}
	if names[0] != "architect" || names[1] != "reviewer" || names[2] != "tester" {
		t.Errorf("Names() not sorted: %v", names)
	}
}
