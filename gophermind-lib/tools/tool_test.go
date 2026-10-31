package tools

import "testing"

func TestRegistryWithCopies(t *testing.T) {
	orig := NewRegistry(Tool{Name: "a"}, Tool{Name: "b", Description: "old"})
	cp := orig.With(Tool{Name: "c"}, Tool{Name: "b", Description: "new"})

	if _, ok := orig.Get("c"); ok {
		t.Error("original gained the new tool")
	}
	if got, _ := orig.Get("b"); got.Description != "old" {
		t.Errorf("original b changed: %q", got.Description)
	}
	for _, n := range []string{"a", "b", "c"} {
		if _, ok := cp.Get(n); !ok {
			t.Errorf("copy missing %s", n)
		}
	}
	if got, _ := cp.Get("b"); got.Description != "new" {
		t.Errorf("same-named tool not replaced: %q", got.Description)
	}
}
