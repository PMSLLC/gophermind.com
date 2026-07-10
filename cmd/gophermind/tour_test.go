package main

import (
	"strings"
	"testing"
)

func TestWelcomeTour(t *testing.T) {
	tour := welcomeTour()
	if strings.TrimSpace(tour) == "" {
		t.Fatal("welcome tour is empty")
	}
	// It should point at the core commands so a new user knows where to start.
	for _, want := range []string{"run", "ask", "chat", "doctor"} {
		if !strings.Contains(tour, want) {
			t.Errorf("tour missing mention of %q", want)
		}
	}
}
