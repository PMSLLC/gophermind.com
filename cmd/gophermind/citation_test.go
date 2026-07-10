package main

import (
	"strings"
	"testing"
)

func TestCitationDirective(t *testing.T) {
	d := citationDirective()
	if !strings.Contains(strings.ToLower(d), "cite") || !strings.Contains(d, "URL") {
		t.Errorf("citation directive should mention citing URLs: %q", d)
	}
}
