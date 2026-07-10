package tools

import "testing"

func TestNetBudgetLimitsRequests(t *testing.T) {
	b := NewNetBudget(2, 0) // 2 requests, unlimited bytes
	if err := b.begin(); err != nil {
		t.Fatal(err)
	}
	if err := b.begin(); err != nil {
		t.Fatal(err)
	}
	if err := b.begin(); err == nil {
		t.Error("third request should exceed the request budget")
	}
}

func TestNetBudgetLimitsBytes(t *testing.T) {
	b := NewNetBudget(0, 100) // unlimited requests, 100 bytes
	if err := b.add(60); err != nil {
		t.Fatal(err)
	}
	if err := b.add(60); err == nil {
		t.Error("exceeding the byte budget should error")
	}
}

func TestNetBudgetNilIsUnlimited(t *testing.T) {
	var b *NetBudget // nil
	for i := 0; i < 1000; i++ {
		if err := b.begin(); err != nil {
			t.Fatal(err)
		}
		if err := b.add(1 << 20); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNetBudgetZeroMeansNoLimit(t *testing.T) {
	b := NewNetBudget(0, 0)
	for i := 0; i < 100; i++ {
		if err := b.begin(); err != nil {
			t.Fatalf("unlimited budget errored: %v", err)
		}
	}
}
