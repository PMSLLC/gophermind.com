package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://example:8000")
	t.Setenv("GOPHERMIND_MODEL", "test-model")
	// Clear the rest so we observe defaults.
	for _, k := range []string{"GOPHERMIND_API_KEY", "GOPHERMIND_APPROVAL", "GOPHERMIND_MAX_ITER", "GOPHERMIND_HTTP_TIMEOUT_S", "GOPHERMIND_CMD_TIMEOUT_S", "GOPHERMIND_ROOT"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ApprovalMode != "ask" {
		t.Errorf("ApprovalMode = %q, want ask", cfg.ApprovalMode)
	}
	if cfg.MaxIter != 25 {
		t.Errorf("MaxIter = %d, want 25", cfg.MaxIter)
	}
	if cfg.HTTPTimeout != 300*time.Second {
		t.Errorf("HTTPTimeout = %v, want 5m", cfg.HTTPTimeout)
	}
	if cfg.RootDir == "" {
		t.Error("RootDir should default to cwd, got empty")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_APPROVAL", "auto")
	t.Setenv("GOPHERMIND_MAX_ITER", "7")
	t.Setenv("GOPHERMIND_CMD_TIMEOUT_S", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ApprovalMode != "auto" {
		t.Errorf("ApprovalMode = %q, want auto", cfg.ApprovalMode)
	}
	if cfg.MaxIter != 7 {
		t.Errorf("MaxIter = %d, want 7", cfg.MaxIter)
	}
	if cfg.CmdTimeout != 30*time.Second {
		t.Errorf("CmdTimeout = %v, want 30s", cfg.CmdTimeout)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"ok", Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 1}, false},
		{"empty model ok (discovered)", Config{BaseURL: "http://x", ApprovalMode: "ask", MaxIter: 1}, false},
		{"no base url", Config{Model: "m", ApprovalMode: "ask", MaxIter: 1}, true},
		{"bad mode", Config{BaseURL: "http://x", Model: "m", ApprovalMode: "yolo", MaxIter: 1}, true},
		{"zero iter", Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
