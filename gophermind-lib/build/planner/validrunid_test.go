package planner

import (
	"strings"
	"testing"
)

func TestValidRunID(t *testing.T) {
	for id, want := range map[string]bool{
		"gm-2026-10-08-001": true, "": false, "../x": false, "gm-2026-10-08-1": false, "gm-2026-10-08-001/x": false,
	} {
		if got := ValidRunID(id); got != want {
			t.Errorf("ValidRunID(%q) = %v", id, got)
		}
	}
}

func TestValidRunID_NewShape(t *testing.T) {
	for id, want := range runIDTable() {
		if got := ValidRunID(id); got != want {
			t.Errorf("ValidRunID(%q) = %v, want %v", id, got, want)
		}
	}
}

func runIDTable() map[string]bool {
	return map[string]bool{
		"csvstat-v1-20261029-r001":                     true,
		"csvstat-v1.1.2-20261029-r004":                 true,
		"csvstat-v1.1b.2-20261029-r1000":               true,
		"ledger-sync-v1-20261029-r001":                 true,
		"gm-2026-10-29-004":                            true,
		"csvstat-v1-20261029":                          false,
		"Csvstat-v1-20261029-r001":                     false,
		"a/../b-v1-20261029-r001":                      false,
		"csvstat-v1..1-20261029-r001":                  false,
		"csvstat-v1-20261029-r001/":                    false,
		strings.Repeat("a", 130) + "-v1-20261029-r001": false,
		"": false,
	}
}
