package safety

import (
	"errors"
	"testing"
)

func TestJudgeApprovalUsesVerdict(t *testing.T) {
	approve := JudgeApproval(func(tool, args string) (bool, string, error) {
		return tool == "read_file", "reason", nil
	}, func(tool, args string) bool { return false })

	if !approve("read_file", "{}") {
		t.Error("judge approved read_file; wrapper should allow")
	}
	if approve("run_shell", "{}") {
		t.Error("judge denied run_shell; wrapper should deny")
	}
}

func TestJudgeApprovalFallsBackOnError(t *testing.T) {
	fallbackCalled := false
	approve := JudgeApproval(
		func(tool, args string) (bool, string, error) { return false, "", errors.New("model down") },
		func(tool, args string) bool { fallbackCalled = true; return true },
	)
	if !approve("write_file", "{}") {
		t.Error("on judge error, wrapper should defer to the fallback (which approved)")
	}
	if !fallbackCalled {
		t.Error("fallback was not consulted on judge error")
	}
}

func TestJudgeApprovalNilJudgeIsFallback(t *testing.T) {
	approve := JudgeApproval(nil, func(tool, args string) bool { return true })
	if !approve("run_shell", "{}") {
		t.Error("nil judge should pass through to fallback")
	}
}
