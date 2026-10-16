package planner

import "testing"

func TestValidRunID(t *testing.T) {
	for id, want := range map[string]bool{
		"gm-2026-10-08-001": true, "": false, "../x": false, "gm-2026-10-08-1": false, "gm-2026-10-08-001/x": false,
	} {
		if got := ValidRunID(id); got != want {
			t.Errorf("ValidRunID(%q) = %v", id, got)
		}
	}
}
