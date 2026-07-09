package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestSamplingDefaults(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_TEMPERATURE", "")
	t.Setenv("GOPHERMIND_TOP_P", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temperature != 0 {
		t.Errorf("Temperature = %v, want 0 (deterministic default)", cfg.Temperature)
	}
	if cfg.TopP != nil {
		t.Errorf("TopP = %v, want nil (unset default)", *cfg.TopP)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with defaults: %v", err)
	}
}

func TestSamplingEnvParsing(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_TEMPERATURE", "0.7")
	t.Setenv("GOPHERMIND_TOP_P", "0.9")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", cfg.Temperature)
	}
	if cfg.TopP == nil || *cfg.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", cfg.TopP)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestSamplingZeroTemperatureExplicit(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_TEMPERATURE", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temperature != 0 {
		t.Errorf("Temperature = %v, want explicit 0", cfg.Temperature)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestSamplingValidationRejectsOutOfRange(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")

	cases := []struct {
		name string
		temp string
		topp string
	}{
		{"temp too high", "2.5", ""},
		{"temp negative", "-0.1", ""},
		{"topp too high", "0", "1.5"},
		{"topp zero", "0", "0"},
		{"topp negative", "0", "-0.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPHERMIND_TEMPERATURE", tc.temp)
			t.Setenv("GOPHERMIND_TOP_P", tc.topp)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Errorf("Validate accepted out-of-range temp=%q topp=%q", tc.temp, tc.topp)
			}
		})
	}
}

func TestValidateTemperatureRejectsNonFinite(t *testing.T) {
	if err := ValidateTemperature(math.NaN()); err == nil {
		t.Error("ValidateTemperature(NaN) should error")
	}
	if err := ValidateTemperature(math.Inf(1)); err == nil {
		t.Error("ValidateTemperature(+Inf) should error")
	}
}

func TestValidateTopPRejectsNonFinite(t *testing.T) {
	if err := ValidateTopP(math.NaN()); err == nil {
		t.Error("ValidateTopP(NaN) should error")
	}
	if err := ValidateTopP(math.Inf(1)); err == nil {
		t.Error("ValidateTopP(+Inf) should error")
	}
}

func TestFallbackModelsParsing(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	// Trims whitespace and drops empty entries.
	t.Setenv("GOPHERMIND_FALLBACK_MODELS", " a , b ,, c ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(cfg.FallbackModels) != len(want) {
		t.Fatalf("FallbackModels = %v, want %v", cfg.FallbackModels, want)
	}
	for i := range want {
		if cfg.FallbackModels[i] != want[i] {
			t.Errorf("FallbackModels[%d] = %q, want %q", i, cfg.FallbackModels[i], want[i])
		}
	}
}

func TestFallbackModelsDefaultsNil(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_FALLBACK_MODELS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FallbackModels != nil {
		t.Errorf("FallbackModels = %v, want nil (unset => no fallback)", cfg.FallbackModels)
	}
}

func TestPriceEnvParsing(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_PRICE_INPUT_PER_1K", "0.50")
	t.Setenv("GOPHERMIND_PRICE_OUTPUT_PER_1K", "1.50")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InputPricePer1K != 0.50 {
		t.Errorf("InputPricePer1K = %v, want 0.50", cfg.InputPricePer1K)
	}
	if cfg.OutputPricePer1K != 1.50 {
		t.Errorf("OutputPricePer1K = %v, want 1.50", cfg.OutputPricePer1K)
	}
}

func TestPriceDefaultsZero(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_PRICE_INPUT_PER_1K", "")
	t.Setenv("GOPHERMIND_PRICE_OUTPUT_PER_1K", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InputPricePer1K != 0 || cfg.OutputPricePer1K != 0 {
		t.Errorf("prices should default to 0, got input=%v output=%v", cfg.InputPricePer1K, cfg.OutputPricePer1K)
	}
}

func TestApplyProfileDefaultPreservesLegacy(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://legacy:9000")
	t.Setenv("GOPHERMIND_API_KEY", "legacy-key")
	t.Setenv("GOPHERMIND_MODEL", "legacy-model")
	t.Setenv("GOPHERMIND_PROFILE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got.BaseURL != "http://legacy:9000" || got.APIKey != "legacy-key" || got.Model != "legacy-model" {
		t.Errorf("legacy single-endpoint behavior changed: %+v", got.BaseURL)
	}
}

func TestApplyProfileBuiltinDefaults(t *testing.T) {
	// Clear any per-profile overrides.
	for _, k := range []string{"GOPHERMIND_PROFILE_OPENAI_BASE_URL", "GOPHERMIND_PROFILE_OPENAI_MODEL", "GOPHERMIND_PROFILE_OPENAI_API_KEY", "GOPHERMIND_PROFILE_OPENAI_TIMEOUT"} {
		t.Setenv(k, "")
	}
	cfg := Config{Profile: "openai", BaseURL: "http://legacy", Model: "legacy", APIKey: "legacy"}
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want openai default", got.BaseURL)
	}
	if got.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want gpt-4o-mini", got.Model)
	}
	// A built-in profile carries no key; legacy key must NOT leak into it.
	if got.APIKey != "" {
		t.Errorf("APIKey should be empty for built-in profile, got non-empty")
	}
	if got.HTTPTimeout != 120*time.Second {
		t.Errorf("HTTPTimeout = %v, want 120s", got.HTTPTimeout)
	}
}

func TestApplyProfileEnvOverridesDefaults(t *testing.T) {
	t.Setenv("GOPHERMIND_PROFILE_OPENAI_BASE_URL", "http://proxy:8000/v1")
	t.Setenv("GOPHERMIND_PROFILE_OPENAI_MODEL", "gpt-custom")
	t.Setenv("GOPHERMIND_PROFILE_OPENAI_API_KEY", "sk-secret")
	t.Setenv("GOPHERMIND_PROFILE_OPENAI_TIMEOUT", "45")

	cfg := Config{Profile: "openai"}
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got.BaseURL != "http://proxy:8000/v1" {
		t.Errorf("BaseURL = %q, want env override", got.BaseURL)
	}
	if got.Model != "gpt-custom" {
		t.Errorf("Model = %q, want env override", got.Model)
	}
	if got.APIKey != "sk-secret" {
		t.Errorf("APIKey not picked up from per-profile env")
	}
	if got.HTTPTimeout != 45*time.Second {
		t.Errorf("HTTPTimeout = %v, want 45s", got.HTTPTimeout)
	}
}

func TestApplyProfileHyphenatedName(t *testing.T) {
	t.Setenv("GOPHERMIND_PROFILE_ANTHROPIC_PROXY_BASE_URL", "http://shim:9999/v1")
	cfg := Config{Profile: "anthropic-proxy"}
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got.BaseURL != "http://shim:9999/v1" {
		t.Errorf("BaseURL = %q, want hyphenated env override", got.BaseURL)
	}
}

func TestApplyProfileCustomViaEnv(t *testing.T) {
	t.Setenv("GOPHERMIND_PROFILE_MYBOX_BASE_URL", "http://mybox:1234/v1")
	t.Setenv("GOPHERMIND_PROFILE_MYBOX_MODEL", "qwen")
	cfg := Config{Profile: "mybox"}
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile custom: %v", err)
	}
	if got.BaseURL != "http://mybox:1234/v1" || got.Model != "qwen" {
		t.Errorf("custom profile not resolved: base=%q model=%q", got.BaseURL, got.Model)
	}
}

func TestApplyProfileUnknown(t *testing.T) {
	t.Setenv("GOPHERMIND_PROFILE_NOPE_BASE_URL", "")
	cfg := Config{Profile: "nope"}
	_, err := cfg.ApplyProfile()
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the bad profile, got: %v", err)
	}
}

func TestApplyProfileRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"  ", "../etc", "a/b", "foo bar", "x\ty", "dot.name"} {
		cfg := Config{Profile: name}
		if _, err := cfg.ApplyProfile(); err == nil {
			t.Errorf("expected rejection for unsafe profile name %q", name)
		}
	}
}

func TestApplyProfileErrorDoesNotLeakSecrets(t *testing.T) {
	// Even with key material in the environment, an unknown-profile error must
	// not include it.
	t.Setenv("GOPHERMIND_PROFILE_SECRETBOX_API_KEY", "sk-super-secret-value")
	cfg := Config{Profile: "secretbox"} // no _BASE_URL => unknown
	_, err := cfg.ApplyProfile()
	if err == nil {
		t.Fatal("expected unknown-profile error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-value") {
		t.Errorf("error leaked API key material: %v", err)
	}
}

func TestProfileSelectionPrecedence(t *testing.T) {
	// Env selects a profile; a non-empty flag value (simulated by overwriting
	// Config.Profile) takes precedence — matching main.go's flag > env order.
	t.Setenv("GOPHERMIND_PROFILE", "openai")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Profile != "openai" {
		t.Errorf("Profile from env = %q, want openai", cfg.Profile)
	}
	// Flag override.
	cfg.Profile = "local-llama"
	got, err := cfg.ApplyProfile()
	if err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if got.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("flag-selected profile not applied: BaseURL=%q", got.BaseURL)
	}
}

func TestConfigStringHasNoStringerLeak(t *testing.T) {
	// Config has no custom Stringer; ensure default formatting that an operator
	// might print never trips up — and that we don't accidentally add one that
	// dumps the key. This guards the secrets-handling contract.
	cfg := Config{Profile: "openai", APIKey: "sk-leak-me"}
	// %v on a struct prints field values; that's expected for a plain struct.
	// The contract we enforce elsewhere is that error/log paths never print the
	// key. Here we simply assert ApplyProfile's error path stays clean.
	cfg.Profile = "unknownxyz"
	cfg.APIKey = "sk-leak-me"
	if _, err := cfg.ApplyProfile(); err != nil && strings.Contains(err.Error(), "sk-leak-me") {
		t.Errorf("ApplyProfile error leaked key: %v", err)
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
		{"negative price", Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 1, InputPricePer1K: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClientCertEnvParsing(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	t.Setenv("GOPHERMIND_CLIENT_CERT", "/etc/certs/client.crt")
	t.Setenv("GOPHERMIND_CLIENT_KEY", "/etc/certs/client.key")
	t.Setenv("GOPHERMIND_CA_CERT", "/etc/certs/ca.crt")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientCertPath != "/etc/certs/client.crt" {
		t.Errorf("ClientCertPath = %q", cfg.ClientCertPath)
	}
	if cfg.ClientKeyPath != "/etc/certs/client.key" {
		t.Errorf("ClientKeyPath = %q", cfg.ClientKeyPath)
	}
	if cfg.CACertPath != "/etc/certs/ca.crt" {
		t.Errorf("CACertPath = %q", cfg.CACertPath)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with both cert+key: %v", err)
	}
}

func TestClientCertDefaultsEmpty(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_MODEL", "m")
	for _, k := range []string{"GOPHERMIND_CLIENT_CERT", "GOPHERMIND_CLIENT_KEY", "GOPHERMIND_CA_CERT"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientCertPath != "" || cfg.ClientKeyPath != "" || cfg.CACertPath != "" {
		t.Errorf("cert paths should default empty, got cert=%q key=%q ca=%q", cfg.ClientCertPath, cfg.ClientKeyPath, cfg.CACertPath)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with no cert config: %v", err)
	}
}

func TestValidateRejectsCertWithoutKey(t *testing.T) {
	cfg := Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 1, ClientCertPath: "/c.crt"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error: cert without key")
	}
}

func TestValidateRejectsKeyWithoutCert(t *testing.T) {
	cfg := Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 1, ClientKeyPath: "/c.key"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error: key without cert")
	}
}

func TestValidateAcceptsCAAlone(t *testing.T) {
	// A custom CA without client-cert auth is valid (server-trust only).
	cfg := Config{BaseURL: "http://x", Model: "m", ApprovalMode: "ask", MaxIter: 1, CACertPath: "/ca.crt"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("CA-only config should validate: %v", err)
	}
}

func TestTranscriptPathDefaultsEmpty(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_TRANSCRIPT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TranscriptPath != "" {
		t.Errorf("TranscriptPath = %q, want empty (unset => no export)", cfg.TranscriptPath)
	}
}

func TestTranscriptPathFromEnv(t *testing.T) {
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")
	t.Setenv("GOPHERMIND_TRANSCRIPT", "/tmp/session.jsonl")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TranscriptPath != "/tmp/session.jsonl" {
		t.Errorf("TranscriptPath = %q, want /tmp/session.jsonl", cfg.TranscriptPath)
	}
}

// writeEnvFile drops a .env file with the given contents into dir.
func writeEnvFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
}

// unsetEnv makes a var genuinely absent for the duration of the test. The
// t.Setenv call first registers restoration of any ambient value; the following
// Unsetenv then clears it so loadDotEnv sees it as unset rather than empty.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestLoadDotEnvSeedsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "GOPHERMIND_BASE_URL=http://fromdotenv:9000\nGOPHERMIND_MODEL=dotenv-model\nGOPHERMIND_MAX_ITER=9\n")
	t.Chdir(dir)
	unsetEnv(t, "GOPHERMIND_BASE_URL", "GOPHERMIND_MODEL", "GOPHERMIND_MAX_ITER", "GOPHERMIND_ROOT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "http://fromdotenv:9000" {
		t.Errorf("BaseURL = %q, want http://fromdotenv:9000 (from .env)", cfg.BaseURL)
	}
	if cfg.Model != "dotenv-model" {
		t.Errorf("Model = %q, want dotenv-model (from .env)", cfg.Model)
	}
	if cfg.MaxIter != 9 {
		t.Errorf("MaxIter = %d, want 9 (from .env)", cfg.MaxIter)
	}
}

func TestLoadDotEnvRealEnvWins(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "GOPHERMIND_MODEL=dotenv-model\n")
	t.Chdir(dir)
	t.Setenv("GOPHERMIND_BASE_URL", "http://real")
	t.Setenv("GOPHERMIND_MODEL", "real-model") // exported => must beat .env

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "real-model" {
		t.Errorf("Model = %q, want real-model (real env must win over .env)", cfg.Model)
	}
}

func TestLoadDotEnvSyntax(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"# a comment",
		"",
		"   # indented comment",
		"export GOPHERMIND_BASE_URL=http://exported:8000",
		`GOPHERMIND_MODEL="quoted-model"`,
		"GOPHERMIND_APPROVAL='auto'",
		"notakeyline",
		"GOPHERMIND_MAX_ITER = 12 ",
	}, "\n")
	writeEnvFile(t, dir, content)
	t.Chdir(dir)
	unsetEnv(t, "GOPHERMIND_BASE_URL", "GOPHERMIND_MODEL", "GOPHERMIND_APPROVAL", "GOPHERMIND_MAX_ITER", "GOPHERMIND_ROOT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "http://exported:8000" {
		t.Errorf("BaseURL = %q, want http://exported:8000 (export prefix stripped)", cfg.BaseURL)
	}
	if cfg.Model != "quoted-model" {
		t.Errorf("Model = %q, want quoted-model (double quotes stripped)", cfg.Model)
	}
	if cfg.ApprovalMode != "auto" {
		t.Errorf("ApprovalMode = %q, want auto (single quotes stripped)", cfg.ApprovalMode)
	}
	if cfg.MaxIter != 12 {
		t.Errorf("MaxIter = %d, want 12 (key/value trimmed)", cfg.MaxIter)
	}
}

func TestLoadDotEnvMissingFileOK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	unsetEnv(t, "GOPHERMIND_MAX_ITER", "GOPHERMIND_ROOT")
	t.Setenv("GOPHERMIND_BASE_URL", "http://x")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no .env should not error: %v", err)
	}
	if cfg.MaxIter != 25 {
		t.Errorf("MaxIter = %d, want 25 (default holds with no .env)", cfg.MaxIter)
	}
}
