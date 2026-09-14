package safety

import (
	"testing"
	"time"
)

func TestSecretScannerDetectsKeysNotProse(t *testing.T) {
	ss := NewSecretScanner()
	if !ss.Scan("token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Error("should detect a GitHub token")
	}
	if !ss.Scan("key: sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Error("should detect an sk- key")
	}
	if ss.Scan("the quick brown fox jumps over the lazy dog") {
		t.Error("plain prose should not match")
	}
}

func TestCheckPolicyAllowList(t *testing.T) {
	p := &Policy{AllowList: []string{"git status", "go test"}}
	if err := CheckPolicy(p, "git status"); err != nil {
		t.Errorf("allow-listed command rejected: %v", err)
	}
	if err := CheckPolicy(p, "rm -rf /"); err == nil {
		t.Error("non-allow-listed command should be rejected")
	}
}

func TestReadModeDeniesGatedTools(t *testing.T) {
	approve := ReadMode()
	if approve("write_file", "") || approve("run_shell", "") {
		t.Error("read mode must deny gated tools")
	}
	if !approve("read_file", "") || !approve("search", "") {
		t.Error("read mode must allow non-gated tools")
	}
}

func TestApprovalWithTimeout(t *testing.T) {
	yes := ApprovalWithTimeout(func(_, _ string) bool { return true }, time.Second)
	if !yes("t", "") {
		t.Error("fast approval should pass through")
	}
	slow := ApprovalWithTimeout(func(_, _ string) bool { time.Sleep(50 * time.Millisecond); return true }, 5*time.Millisecond)
	if slow("t", "") {
		t.Error("should deny on timeout")
	}
}
