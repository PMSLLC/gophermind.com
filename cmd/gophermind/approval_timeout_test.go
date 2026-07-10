package main

import (
	"testing"
	"time"
)

func TestApprovalTimeout(t *testing.T) {
	t.Setenv("GOPHERMIND_APPROVAL_TIMEOUT", "45s")
	if got := approvalTimeout(); got != 45*time.Second {
		t.Errorf("approvalTimeout = %v, want 45s", got)
	}
	t.Setenv("GOPHERMIND_APPROVAL_TIMEOUT", "")
	if got := approvalTimeout(); got != 0 {
		t.Errorf("unset should be 0, got %v", got)
	}
	t.Setenv("GOPHERMIND_APPROVAL_TIMEOUT", "garbage")
	if got := approvalTimeout(); got != 0 {
		t.Errorf("invalid should be 0, got %v", got)
	}
}
