package agent

import "testing"

func TestAllocateBudget(t *testing.T) {
	// Even split with remainder distributed to the first children.
	got := AllocateBudget(100, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 shares, got %d", len(got))
	}
	sum := 0
	for _, v := range got {
		sum += v
	}
	if sum != 100 {
		t.Errorf("shares should sum to the total: got %d, want 100 (%v)", sum, got)
	}
	if got[0] < got[2] {
		t.Errorf("remainder should go to earlier children: %v", got)
	}
	// Zero children -> nil; zero budget -> zeros.
	if AllocateBudget(100, 0) != nil {
		t.Error("zero children should return nil")
	}
	z := AllocateBudget(0, 2)
	if len(z) != 2 || z[0] != 0 || z[1] != 0 {
		t.Errorf("zero budget should split into zeros, got %v", z)
	}
}
