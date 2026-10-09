package planner

import "testing"

func TestExpandRepoEmptyStaysEmpty(t *testing.T) {
	got, err := ExpandRepo("")
	if err != nil || got != "" {
		t.Fatalf("ExpandRepo(\"\") = %q, %v; want empty, nil", got, err)
	}
}
