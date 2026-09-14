package freellm

import (
	"testing"
)

func TestRecordAccumulatesAcrossTurns(t *testing.T) {
	t.Setenv("GOPHERMIND_ODOMETER", t.TempDir()+"/odo.json")

	if err := Record("free-groq", "llama-3.3-70b", 100, 50); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := Record("free-groq", "llama-3.3-70b", 10, 5); err != nil {
		t.Fatalf("second record: %v", err)
	}

	o, err := LoadOdometer(OdometerPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Tokens; got != 165 {
		t.Errorf("Tokens = %d, want 165", got)
	}
	if got := o.Requests; got != 2 {
		t.Errorf("Requests = %d, want 2", got)
	}
	if got := o.ModelReading("free-groq", "llama-3.3-70b").Tokens; got != 165 {
		t.Errorf("per-model Tokens = %d, want 165", got)
	}
}

// A turn on the user's own endpoint spends no free-tier allowance, so there
// is nothing to meter and nothing to write.
func TestRecordIgnoresANonFreeTurn(t *testing.T) {
	path := t.TempDir() + "/odo.json"
	t.Setenv("GOPHERMIND_ODOMETER", path)

	if err := Record("", "qwen3.6-35b", 100, 50); err != nil {
		t.Fatalf("record: %v", err)
	}
	o, err := LoadOdometer(path)
	if err != nil {
		t.Fatal(err)
	}
	if o.Requests != 0 {
		t.Errorf("Requests = %d, want 0 for a non-free turn", o.Requests)
	}
}
