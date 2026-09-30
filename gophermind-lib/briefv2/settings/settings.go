// Package settings reads ~/.gophermind/gophermind.yaml: which providers exist,
// which of them may see what (visibility), and which models each tier tries in
// which order. A provider's key is never stored here, only the name of the
// vault entry that holds it.
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/config"
)

// Visibility says whether a provider runs on John's hardware or somewhere else.
type Visibility string

const (
	Private Visibility = "private"
	Public  Visibility = "public"
)

// The three model tiers, in the order a config lists them.
var Tiers = []string{"strong", "standard", "any"}

type ModelEntry struct {
	ID            string `yaml:"id"`
	ContextTokens int    `yaml:"context_tokens"`
}

type ProviderConfig struct {
	Name          string       `yaml:"name"`
	BaseURL       string       `yaml:"base_url"`
	Visibility    Visibility   `yaml:"visibility"`
	MaxConcurrent int          `yaml:"max_concurrent"`
	Models        []ModelEntry `yaml:"models"`
	APIKeySecret  string       `yaml:"api_key_secret,omitempty"`
	// ReasoningEffort: none, low, medium or high. Empty means the field is not sent.
	ReasoningEffort string `yaml:"reasoning_effort,omitempty"`
}

type Privacy struct {
	Mode string `yaml:"mode"` // need_to_know | private_only
}

type Defaults struct {
	MaxContextTokens  int           `yaml:"max_context_tokens"`
	MaxRevisions      int           `yaml:"max_revisions"`
	MaxCoverageRounds int           `yaml:"max_coverage_rounds"`
	CallTimeout       time.Duration `yaml:"call_timeout"`
	MaxWaitMinutes    int           `yaml:"max_wait_minutes"`
}

type RateLimits struct {
	CooldownAfter429Seconds int `yaml:"cooldown_after_429_seconds"`
	BackoffInitialSeconds   int `yaml:"backoff_initial_seconds"`
	BackoffMaxSeconds       int `yaml:"backoff_max_seconds"`
	BackoffMultiplier       int `yaml:"backoff_multiplier"`
}

type Human struct {
	Mode string `yaml:"mode"` // terminal | file
}

type Vault struct {
	Path string `yaml:"path"`
}

// Config is the whole gophermind.yaml.
type Config struct {
	Providers  []ProviderConfig    `yaml:"providers"`
	Models     map[string][]string `yaml:"models"`
	Privacy    Privacy             `yaml:"privacy"`
	Defaults   Defaults            `yaml:"defaults"`
	RateLimits RateLimits          `yaml:"rate_limits"`
	Human      Human               `yaml:"human"`
	Vault      Vault               `yaml:"vault"`
}

// Default is what a first run writes: the Mac mini plus two providers that
// need no key.
func Default() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{Name: "mini", BaseURL: "http://192.168.1.35:11434/v1", Visibility: Private, MaxConcurrent: 1,
				Models: []ModelEntry{{ID: "qwen3.6:35b-a3b", ContextTokens: 32768}}, ReasoningEffort: "none"},
			{Name: "kilo", BaseURL: "https://api.kilo.ai/api/gateway", Visibility: Public, MaxConcurrent: 2,
				Models: []ModelEntry{{ID: "kilo-auto/free", ContextTokens: 131072}}},
			{Name: "ovh", BaseURL: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1", Visibility: Public, MaxConcurrent: 1,
				Models: []ModelEntry{{ID: "Qwen3.6-27B", ContextTokens: 131072}}},
		},
		Models: map[string][]string{
			"strong":   {"mini/qwen3.6:35b-a3b"},
			"standard": {"mini/qwen3.6:35b-a3b", "kilo/kilo-auto/free"},
			"any":      {"kilo/kilo-auto/free", "ovh/Qwen3.6-27B", "mini/qwen3.6:35b-a3b"},
		},
		Privacy: Privacy{Mode: "need_to_know"},
		Defaults: Defaults{MaxContextTokens: 8000, MaxRevisions: 2, MaxCoverageRounds: 2,
			CallTimeout: 10 * time.Minute, MaxWaitMinutes: 30},
		RateLimits: RateLimits{CooldownAfter429Seconds: 60, BackoffInitialSeconds: 5, BackoffMaxSeconds: 300, BackoffMultiplier: 2},
		Human:      Human{Mode: "terminal"},
		Vault:      Vault{Path: "~/.gophermind/vault.age"},
	}
}

// Path is ~/.gophermind/gophermind.yaml, or under GOPHERMIND_CONFIG_DIR.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind.yaml"), nil
}

const header = "# GopherMind v2 settings. A provider's key is never written here: api_key_secret names\n" +
	"# an entry in the vault (gophermind brief vault set <name>).\n"

// Load reads path. When the file does not exist it writes the defaults there
// (file mode 0600, folder 0700) and returns them. Unknown fields are an error,
// so a pasted "api_key: ..." line is refused rather than silently kept.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeDefaults(path)
	}
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	return &c, nil
}

func writeDefaults(path string) (*Config, error) {
	c := Default()
	body, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return Load(path) // another process created it first
	}
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	_, werr := f.WriteString(header + string(body))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("settings: %w", werr)
	}
	return c, nil
}

// SplitEntry splits a chain entry "provider/model" at the first slash, so
// "kilo/kilo-auto/free" is provider kilo, model kilo-auto/free.
func SplitEntry(entry string) (providerName, model string, ok bool) {
	i := strings.Index(entry, "/")
	if i <= 0 || i == len(entry)-1 {
		return "", "", false
	}
	return entry[:i], entry[i+1:], true
}

// Validate checks everything Load and the router rely on.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return errors.New("providers: at least one provider is required")
	}
	byName := map[string]ProviderConfig{}
	for i, p := range c.Providers {
		where := fmt.Sprintf("providers[%d]", i)
		if p.Name == "" {
			return fmt.Errorf("%s: name is required", where)
		}
		if strings.Contains(p.Name, "/") {
			return fmt.Errorf("%s: name %q must not contain a slash", where, p.Name)
		}
		if _, dup := byName[p.Name]; dup {
			return fmt.Errorf("providers: duplicate provider name %q", p.Name)
		}
		byName[p.Name] = p
		where = "provider " + p.Name
		if p.BaseURL == "" {
			return fmt.Errorf("%s: base_url is required", where)
		}
		if p.Visibility != Private && p.Visibility != Public {
			return fmt.Errorf("%s: visibility must be private or public, got %q", where, p.Visibility)
		}
		if p.MaxConcurrent < 1 {
			return fmt.Errorf("%s: max_concurrent must be at least 1, got %d", where, p.MaxConcurrent)
		}
		switch p.ReasoningEffort {
		case "", "none", "low", "medium", "high":
		default:
			return fmt.Errorf("%s: reasoning_effort must be none, low, medium or high, got %q", where, p.ReasoningEffort)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("%s: at least one model is required", where)
		}
		seen := map[string]bool{}
		for _, m := range p.Models {
			if m.ID == "" {
				return fmt.Errorf("%s: a model has no id", where)
			}
			if seen[m.ID] {
				return fmt.Errorf("%s: duplicate model %q", where, m.ID)
			}
			seen[m.ID] = true
			if m.ContextTokens < 1 {
				return fmt.Errorf("%s: model %s: context_tokens must be at least 1, got %d", where, m.ID, m.ContextTokens)
			}
		}
	}
	for tier := range c.Models {
		if tier != "strong" && tier != "standard" && tier != "any" {
			return fmt.Errorf("models: unknown tier %q (want strong, standard, or any)", tier)
		}
	}
	for _, tier := range Tiers {
		chain := c.Models[tier]
		if len(chain) == 0 {
			return fmt.Errorf("models: tier %s has no entries", tier)
		}
		for _, entry := range chain {
			if _, ok := c.ModelInfo(entry); !ok {
				return fmt.Errorf("models: tier %s: %q is not a configured provider/model", tier, entry)
			}
		}
	}
	if c.Privacy.Mode != "need_to_know" && c.Privacy.Mode != "private_only" {
		return fmt.Errorf("privacy.mode must be need_to_know or private_only, got %q", c.Privacy.Mode)
	}
	d := c.Defaults
	switch {
	case d.MaxContextTokens < 1:
		return fmt.Errorf("defaults.max_context_tokens must be at least 1, got %d", d.MaxContextTokens)
	case d.MaxRevisions < 0:
		return fmt.Errorf("defaults.max_revisions must not be negative, got %d", d.MaxRevisions)
	case d.MaxCoverageRounds < 0:
		return fmt.Errorf("defaults.max_coverage_rounds must not be negative, got %d", d.MaxCoverageRounds)
	case d.CallTimeout <= 0:
		return fmt.Errorf("defaults.call_timeout must be positive, got %v", d.CallTimeout)
	case d.MaxWaitMinutes < 0:
		return fmt.Errorf("defaults.max_wait_minutes must not be negative, got %d", d.MaxWaitMinutes)
	}
	r := c.RateLimits
	switch {
	case r.CooldownAfter429Seconds < 1:
		return fmt.Errorf("rate_limits.cooldown_after_429_seconds must be at least 1, got %d", r.CooldownAfter429Seconds)
	case r.BackoffInitialSeconds < 1:
		return fmt.Errorf("rate_limits.backoff_initial_seconds must be at least 1, got %d", r.BackoffInitialSeconds)
	case r.BackoffMaxSeconds < r.BackoffInitialSeconds:
		return fmt.Errorf("rate_limits.backoff_max_seconds (%d) must be at least backoff_initial_seconds (%d)", r.BackoffMaxSeconds, r.BackoffInitialSeconds)
	case r.BackoffMultiplier < 1:
		return fmt.Errorf("rate_limits.backoff_multiplier must be at least 1, got %d", r.BackoffMultiplier)
	}
	if c.Human.Mode != "terminal" && c.Human.Mode != "file" {
		return fmt.Errorf("human.mode must be terminal or file, got %q", c.Human.Mode)
	}
	if c.Vault.Path == "" {
		return errors.New("vault.path is required")
	}
	return nil
}

// Visibility reports a provider's visibility; ok is false for an unknown name.
func (c *Config) Visibility(providerName string) (Visibility, bool) {
	for _, p := range c.Providers {
		if p.Name == providerName {
			return p.Visibility, true
		}
	}
	return "", false
}

// ModelInfo resolves a chain entry "provider/model" to the model's info.
func (c *Config) ModelInfo(entry string) (provider.ModelInfo, bool) {
	name, model, ok := SplitEntry(entry)
	if !ok {
		return provider.ModelInfo{}, false
	}
	for _, p := range c.Providers {
		if p.Name != name {
			continue
		}
		for _, m := range p.Models {
			if m.ID == model {
				return provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens}, true
			}
		}
	}
	return provider.ModelInfo{}, false
}
