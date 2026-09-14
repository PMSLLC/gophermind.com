package tools

import (
	"strings"
	"testing"
)

func TestClassifyEgress(t *testing.T) {
	cases := []struct {
		payload string
		want    string // a category that must be present, or "" for none
	}{
		{`{"key":"AKIA0123456789ABCDEF"}`, "secret"},
		{`contact me at alice@example.com`, "email"},
		{`nothing sensitive here`, ""},
	}
	for _, c := range cases {
		got := classifyEgress(c.payload)
		if c.want == "" {
			if len(got) != 0 {
				t.Errorf("payload %q: expected no categories, got %v", c.payload, got)
			}
			continue
		}
		if !containsStr(got, c.want) {
			t.Errorf("payload %q: expected category %q, got %v", c.payload, c.want, got)
		}
	}
}

func TestGuardEgressWarn(t *testing.T) {
	warn, err := guardEgress("warn", `token=AKIA0123456789ABCDEF`)
	if err != nil {
		t.Fatalf("warn mode should not error: %v", err)
	}
	if warn == "" || !strings.Contains(strings.ToLower(warn), "secret") {
		t.Errorf("expected a secret warning, got %q", warn)
	}
}

func TestGuardEgressDeny(t *testing.T) {
	_, err := guardEgress("deny", `token=AKIA0123456789ABCDEF`)
	if err == nil {
		t.Error("deny mode should block a payload containing a secret")
	}
}

func TestGuardEgressClean(t *testing.T) {
	warn, err := guardEgress("deny", `just a normal message`)
	if err != nil {
		t.Errorf("clean payload should pass even in deny mode: %v", err)
	}
	if warn != "" {
		t.Errorf("clean payload should have no warning, got %q", warn)
	}
}

func TestGuardEgressDisabled(t *testing.T) {
	// Empty/unknown mode disables the guard entirely.
	warn, err := guardEgress("", `token=AKIA0123456789ABCDEF`)
	if err != nil || warn != "" {
		t.Errorf("disabled guard should be a no-op, got warn=%q err=%v", warn, err)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
