package settings_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/settings"
)

// clineYAML is a whole file with a mini provider and a cline provider; extra
// lines go under the cline provider, and mode is privacy.mode.
func clineYAML(extra, mode string) string {
	return `providers:
  - name: mini
    base_url: http://192.168.1.35:11434/v1
    visibility: private
    max_concurrent: 1
    models:
      - {id: "qwen3.6:35b-a3b", context_tokens: 32768}
  - name: cline
    kind: cline
    cline_provider: cline-pass
    visibility: public
    max_concurrent: 1
    models:
      - {id: "cline-pass/qwen3.7-max", context_tokens: 131072}
` + extra + `models:
  strong: ["mini/qwen3.6:35b-a3b"]
  standard: ["mini/qwen3.6:35b-a3b"]
  any: ["cline/cline-pass/qwen3.7-max", "mini/qwen3.6:35b-a3b"]
privacy:
  mode: ` + mode + `
defaults:
  max_context_tokens: 8000
  call_timeout: 10m
rate_limits:
  cooldown_after_429_seconds: 60
  cooldown_after_timeout_seconds: 60
  backoff_initial_seconds: 5
  backoff_max_seconds: 300
  backoff_multiplier: 2
human: {mode: terminal}
vault: {path: "~/.gophermind/vault.age"}
`
}

func loadYAML(t *testing.T, body string) (*settings.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return settings.Load(path)
}

func TestClineProviderParsesAndValidates(t *testing.T) {
	extra := "    cline_binary: /usr/local/bin/cline\n    cline_settings_dir: /tmp/cline-settings\n    cline_timeout_seconds: 900\n"
	body := strings.Replace(clineYAML("", "need_to_know"), "    max_concurrent: 1\n    models:\n      - {id: \"cline-pass", extra+"    max_concurrent: 1\n    models:\n      - {id: \"cline-pass", 1)
	c, err := loadYAML(t, body)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Providers[1]
	if p.Kind != "cline" || p.ClineProvider != "cline-pass" || p.ClineBinary != "/usr/local/bin/cline" ||
		p.ClineSettingsDir != "/tmp/cline-settings" || p.ClineTimeoutSeconds == nil || *p.ClineTimeoutSeconds != 900 {
		t.Errorf("provider = %+v", p)
	}
	if !p.IsCline() || c.Providers[0].IsCline() {
		t.Error("IsCline is wrong")
	}
	// The ladder entry's model id contains a slash.
	info, ok := c.ModelInfo("cline/cline-pass/qwen3.7-max")
	if !ok || info.ID != "cline-pass/qwen3.7-max" || info.ContextTokens != 131072 {
		t.Errorf("ModelInfo = %+v, %v", info, ok)
	}
	name, model, ok := settings.SplitEntry("cline/cline-pass/qwen3.7-max")
	if !ok || name != "cline" || model != "cline-pass/qwen3.7-max" {
		t.Errorf("SplitEntry = %q %q %v", name, model, ok)
	}
}

func TestClineProviderRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*settings.Config)
		want   string
	}{
		{"bad kind", func(c *settings.Config) { c.Providers[0].Kind = "anthropic" }, `provider mini: kind must be openai or cline, got "anthropic"`},
		{"private cline", func(c *settings.Config) { c.Providers[1].Visibility = settings.Private }, "provider cline: kind cline needs visibility public"},
		{"no cline_provider", func(c *settings.Config) { c.Providers[1].ClineProvider = "" }, "provider cline: cline_provider is required"},
		{"no models", func(c *settings.Config) { c.Providers[1].Models = nil; c.Models["any"] = c.Models["strong"] }, "at least one model"},
		{"base_url set", func(c *settings.Config) { c.Providers[1].BaseURL = "https://api.example.com/v1" }, "base_url is ignored by kind cline"},
		{"api_key_secret set", func(c *settings.Config) { c.Providers[1].APIKeySecret = "k" }, "api_key_secret is ignored by kind cline"},
		{"base_url_fallbacks set", func(c *settings.Config) { c.Providers[1].BaseURLFallbacks = []string{"https://a.example.com"} }, "base_url_fallbacks is ignored by kind cline"},
		{"private_only", func(c *settings.Config) { c.Privacy.Mode = "private_only" }, "kind cline is not allowed under privacy.mode private_only"},
		{"zero timeout", func(c *settings.Config) { c.Providers[1].ClineTimeoutSeconds = intp(0) }, "cline_timeout_seconds must be at least 1"},
		{"cline_provider on openai", func(c *settings.Config) { c.Providers[0].ClineProvider = "x" }, "provider mini: cline_provider needs kind cline"},
		{"cline_binary on openai", func(c *settings.Config) { c.Providers[0].Kind = "openai"; c.Providers[0].ClineBinary = "x" }, "cline_binary needs kind cline"},
		{"cline_settings_dir on openai", func(c *settings.Config) { c.Providers[0].ClineSettingsDir = "x" }, "cline_settings_dir needs kind cline"},
		{"cline_timeout on openai", func(c *settings.Config) { c.Providers[0].ClineTimeoutSeconds = intp(5) }, "cline_timeout_seconds needs kind cline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadYAML(t, clineYAML("", "need_to_know"))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(c)
			err = c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func intp(n int) *int { return &n }

func TestClinePrivateOnlyIsRefusedOnLoad(t *testing.T) {
	if _, err := loadYAML(t, clineYAML("", "private_only")); err == nil || !strings.Contains(err.Error(), "private_only") {
		t.Errorf("err = %v", err)
	}
}

func TestKindOpenAIAndEmptyBehaveAlike(t *testing.T) {
	a := settings.Default()
	b := settings.Default()
	b.Providers[0].Kind = "openai"
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	pa, err := a.BuildProviders(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.BuildProviders(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pa["mini"].(*provider.OpenAI); !ok {
		t.Errorf("mini is %T", pa["mini"])
	}
	if _, ok := pb["mini"].(*provider.OpenAI); !ok {
		t.Errorf("mini (kind openai) is %T", pb["mini"])
	}
}

func TestBuildProvidersBuildsAClineWithoutAskingForSecrets(t *testing.T) {
	c, err := loadYAML(t, clineYAML("", "need_to_know"))
	if err != nil {
		t.Fatal(err)
	}
	ps, err := c.BuildProviders(nil, func(name string) (string, error) {
		t.Errorf("secret source called for %q", name)
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cl, ok := ps["cline"].(*provider.Cline)
	if !ok {
		t.Fatalf("cline is %T", ps["cline"])
	}
	ms := cl.Models()
	if cl.Name() != "cline" || len(ms) != 1 || ms[0].ID != "cline-pass/qwen3.7-max" || ms[0].ContextTokens != 131072 {
		t.Errorf("name %q models %+v", cl.Name(), ms)
	}
	if _, ok := ps["mini"].(*provider.OpenAI); !ok {
		t.Errorf("mini is %T", ps["mini"])
	}
}
