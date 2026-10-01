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
	"net/netip"
	"net/url"
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
	// BaseURLFallbacks are tried in order when BaseURL does not answer within
	// 3 s at preflight and at the start of a run (the mini's VPN address, for
	// one). Optional; the first one that answers is used for the process only.
	BaseURLFallbacks []string `yaml:"base_url_fallbacks,omitempty"`
	// CallTimeoutSeconds overrides defaults.call_timeout for this provider. Nil means
	// the global value; a value below 1 is refused.
	CallTimeoutSeconds *int `yaml:"call_timeout_seconds,omitempty"`
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
	// CooldownAfterTimeoutSeconds is how long a chain entry is skipped after a call to it
	// timed out. Omitted or 0 in a file means 60.
	CooldownAfterTimeoutSeconds int `yaml:"cooldown_after_timeout_seconds"`
	BackoffInitialSeconds       int `yaml:"backoff_initial_seconds"`
	BackoffMaxSeconds           int `yaml:"backoff_max_seconds"`
	BackoffMultiplier           int `yaml:"backoff_multiplier"`
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
	Executor   ExecutorConfig      `yaml:"executor"`
	Toolchain  map[string]string   `yaml:"toolchain"`
}

// Default is what a first run writes: the Mac mini plus two providers that
// need no key.
func Default() *Config {
	c := &Config{
		Providers: []ProviderConfig{
			{Name: "mini", BaseURL: "http://192.168.1.35:11434/v1", Visibility: Private, MaxConcurrent: 1,
				Models: []ModelEntry{{ID: "qwen3.6:35b-a3b", ContextTokens: 32768}}, ReasoningEffort: "none",
				BaseURLFallbacks: []string{"http://10.8.0.6:11434/v1"}, CallTimeoutSeconds: intPtr(1500)},
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
		RateLimits: RateLimits{CooldownAfter429Seconds: 60, CooldownAfterTimeoutSeconds: 60, BackoffInitialSeconds: 5, BackoffMaxSeconds: 300, BackoffMultiplier: 2},
		Human:      Human{Mode: "terminal"},
		Vault:      Vault{Path: "~/.gophermind/vault.age"},
	}
	c.applyExecutorDefaults()
	return c
}

func intPtr(n int) *int { return &n }

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
	if err := checkExplicitZeroCounts(data); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	if c.RateLimits.CooldownAfterTimeoutSeconds == 0 {
		c.RateLimits.CooldownAfterTimeoutSeconds = 60 // an existing file without the key
	}
	c.applyExecutorDefaults()
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
		if !validBaseURL(p.BaseURL) {
			return fmt.Errorf("%s: base_url must be an http or https URL with a host and no credentials", where)
		}
		// A private provider's prompts go to its base_url, so that host must be on a
		// private network, whatever privacy.mode says. A public provider is not
		// checked: under private_only it is never called.
		if p.Visibility == Private && !c.FallbackAllowed(p, p.BaseURL) {
			return fmt.Errorf("%s: base_url must be a private-network host (loopback, RFC 1918, 100.64.0.0/10, link-local, IPv6 ULA, or a .local, .internal or .lan name) because the provider's visibility is private", where)
		}
		if p.CallTimeoutSeconds != nil && *p.CallTimeoutSeconds < 1 {
			return fmt.Errorf("%s: call_timeout_seconds must be at least 1, got %d", where, *p.CallTimeoutSeconds)
		}
		for _, u := range p.BaseURLFallbacks {
			if !validBaseURL(u) {
				return fmt.Errorf("%s: base_url_fallbacks entries must be http or https URLs with a host and no credentials", where)
			}
			if !c.FallbackAllowed(p, u) {
				return fmt.Errorf("%s: base_url_fallbacks entries must keep the provider's visibility: a private provider, or any provider under privacy.mode private_only, takes only private-network hosts, and a public provider only public ones", where)
			}
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
	case r.CooldownAfterTimeoutSeconds < 1:
		return fmt.Errorf("rate_limits.cooldown_after_timeout_seconds must be at least 1, got %d", r.CooldownAfterTimeoutSeconds)
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
	if err := c.Executor.validate(); err != nil {
		return err
	}
	return validateToolchain(c.Toolchain)
}

// validBaseURL accepts an absolute http or https URL with a host and no
// user information.
func validBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

// FallbackAllowed says whether raw may stand in for p's base_url without
// changing who sees the prompts and the provider's key. The router's privacy
// rule keys on visibility, so a private provider (and every provider under
// privacy.mode private_only) may only fall back to a private-network host and a
// public provider only to a public one. The host is judged by its literal text:
// no name is resolved.
func (c *Config) FallbackAllowed(p ProviderConfig, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	private := privateHost(u.Hostname())
	if p.Visibility == Private || c.Privacy.Mode == "private_only" {
		return private
	}
	return !private
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// privateHost: loopback, RFC 1918, the CGNAT/VPN range 100.64/10, link-local,
// IPv6 ULA, or a name ending in .local, .internal, .lan or .invalid (or localhost).
// RFC 2606 guarantees a .invalid name never resolves, so no prompt can leave the
// machine through one (test fixtures use them).
func privateHost(host string) bool {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		a = a.Unmap()
		return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || cgnat.Contains(a)
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" {
		return true
	}
	for _, suf := range []string{".local", ".internal", ".lan", ".invalid"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
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

// CallTimeoutFor is the call timeout for a provider: its own override or
// defaults.call_timeout.
func (c *Config) CallTimeoutFor(providerName string) time.Duration {
	for _, p := range c.Providers {
		if p.Name == providerName && p.CallTimeoutSeconds != nil && *p.CallTimeoutSeconds >= 1 {
			return time.Duration(*p.CallTimeoutSeconds) * time.Second
		}
	}
	return c.Defaults.CallTimeout
}

// MaxCallTimeout is the longest call timeout any provider can have.
func (c *Config) MaxCallTimeout() time.Duration {
	max := c.Defaults.CallTimeout
	for _, p := range c.Providers {
		if d := c.CallTimeoutFor(p.Name); d > max {
			max = d
		}
	}
	return max
}
