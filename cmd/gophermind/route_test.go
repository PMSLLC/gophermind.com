package main

import "testing"

func TestRouteModel(t *testing.T) {
	speed, strong := "fast-model", "strong-model"

	// Short/simple tasks route to the cheap speed model.
	if got := routeModel("what is 2+2?", speed, strong); got != speed {
		t.Errorf("simple task routed to %q, want speed", got)
	}
	// Complexity keywords route to the strong model.
	for _, task := range []string{
		"refactor the authentication module",
		"debug this failing race condition",
		"design a scalable architecture for X",
	} {
		if got := routeModel(task, speed, strong); got != strong {
			t.Errorf("complex task %q routed to %q, want strong", task, got)
		}
	}
	// A very long task routes to strong even without keywords.
	long := make([]byte, 0, 700)
	for i := 0; i < 700; i++ {
		long = append(long, 'a')
	}
	if got := routeModel(string(long), speed, strong); got != strong {
		t.Errorf("long task routed to %q, want strong", got)
	}
	// Empty speed model disables routing (always strong).
	if got := routeModel("hi", "", strong); got != strong {
		t.Errorf("no speed model should return strong, got %q", got)
	}
}
