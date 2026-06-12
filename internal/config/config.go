// Package config loads runtime configuration from the environment, with
// command-line flags layered on top by the caller.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// defaultBaseURL points at the local llama.cpp server. Override with
// GOPHERMIND_BASE_URL or -base.
const defaultBaseURL = "http://10.0.0.5:8081"

// builtinProfile is a named set of per-endpoint defaults. A profile only
// carries the fields that distinguish one backend from another; everything
// else (approval mode, iteration cap, prices, root) stays global.
type builtinProfile struct {
	BaseURL string
	Model   string        // "" => auto-discover from the endpoint
	Timeout time.Duration // 0 => fall back to the global HTTP timeout default
}

// builtinProfiles are the three example backends a user can select with
// --profile without configuring anything. Each is still overridable via
// per-profile env vars (see profileEnvOr). API keys are intentionally never
// baked in here; supply them with GOPHERMIND_PROFILE_<NAME>_API_KEY.
var builtinProfiles = map[string]builtinProfile{
	"local-llama": {
		BaseURL: "http://127.0.0.1:8080",
		Model:   "", // auto-discover from the local server
		Timeout: 300 * time.Second,
	},
	"openai": {
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
		Timeout: 120 * time.Second,
	},
	"anthropic-proxy": {
		// Placeholder for a local Anthropic-compatible OpenAI shim; override
		// with GOPHERMIND_PROFILE_ANTHROPIC_PROXY_BASE_URL.
		BaseURL: "http://127.0.0.1:8082/v1",
		Model:   "claude-3-5-sonnet",
		Timeout: 120 * time.Second,
	},
}

// Config holds everything the harness needs to run. Every field has a sensible
// default; an empty Model is auto-discovered from the endpoint at startup.
type Config struct {
	Profile      string        // GOPHERMIND_PROFILE: selected provider profile ("" => legacy/default endpoint)
	BaseURL      string        // GOPHERMIND_BASE_URL (required), e.g. http://10.0.0.5:8000
	APIKey       string        // GOPHERMIND_API_KEY (optional; empty when reached over VPN)
	Model        string        // GOPHERMIND_MODEL
	RootDir      string        // GOPHERMIND_ROOT (default: cwd)
	ApprovalMode string        // GOPHERMIND_APPROVAL: auto|ask (default: ask)
	InsecureTLS  bool          // GOPHERMIND_INSECURE_TLS: skip TLS verify (self-signed internal endpoints)
	MaxIter      int           // GOPHERMIND_MAX_ITER (default: 25)
	HTTPTimeout  time.Duration // GOPHERMIND_HTTP_TIMEOUT_S (default: 300s)
	CmdTimeout   time.Duration // GOPHERMIND_CMD_TIMEOUT_S (default: 120s)

	// Per-1,000-token prices (USD) for the running cost meter. Both default to
	// 0, so the meter reports $0.00 until configured.
	InputPricePer1K  float64 // GOPHERMIND_PRICE_INPUT_PER_1K
	OutputPricePer1K float64 // GOPHERMIND_PRICE_OUTPUT_PER_1K
}

// Load reads configuration from the environment and applies defaults. The
// returned Config is not yet validated; call Validate after flags are applied.
func Load() (Config, error) {
	root := envOr("GOPHERMIND_ROOT", "")
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("getwd: %w", err)
		}
		root = wd
	}

	return Config{
		Profile:      envOr("GOPHERMIND_PROFILE", ""),
		BaseURL:      envOr("GOPHERMIND_BASE_URL", defaultBaseURL),
		APIKey:       envOr("GOPHERMIND_API_KEY", ""),
		Model:        envOr("GOPHERMIND_MODEL", ""), // empty => auto-discover from /v1/models
		RootDir:      root,
		ApprovalMode: envOr("GOPHERMIND_APPROVAL", "ask"),
		InsecureTLS:  envBool("GOPHERMIND_INSECURE_TLS"),
		MaxIter:      envIntOr("GOPHERMIND_MAX_ITER", 25),
		HTTPTimeout:  time.Duration(envIntOr("GOPHERMIND_HTTP_TIMEOUT_S", 300)) * time.Second,
		CmdTimeout:   time.Duration(envIntOr("GOPHERMIND_CMD_TIMEOUT_S", 120)) * time.Second,

		InputPricePer1K:  envFloatOr("GOPHERMIND_PRICE_INPUT_PER_1K", 0),
		OutputPricePer1K: envFloatOr("GOPHERMIND_PRICE_OUTPUT_PER_1K", 0),
	}, nil
}

// profileNameRe constrains profile names to a safe, predictable charset. This
// keeps a name usable as an env-var suffix and guarantees it can never be
// turned into a filesystem path component (no slashes, dots, or whitespace).
var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validProfileName rejects empty, whitespace-only, and otherwise unsafe profile
// names. It does NOT echo any secrets; only the (already-untrusted) name.
func validProfileName(name string) error {
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("profile name must not have leading/trailing whitespace")
	}
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if !profileNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: use only letters, digits, '-' and '_'", name)
	}
	return nil
}

// profileEnvKey maps a profile name to the prefix of its env vars, e.g.
// "anthropic-proxy" => "GOPHERMIND_PROFILE_ANTHROPIC_PROXY". The name is
// already validated, so the result is always a safe env identifier.
func profileEnvKey(name string) string {
	up := strings.ToUpper(name)
	up = strings.ReplaceAll(up, "-", "_")
	return "GOPHERMIND_PROFILE_" + up
}

// ApplyProfile resolves the named profile into the endpoint fields (BaseURL,
// APIKey, Model, HTTPTimeout). Resolution order per field:
//
//	per-profile env var  >  built-in profile default
//
// When c.Profile is empty the receiver is returned unchanged, preserving the
// legacy single-endpoint behavior exactly. An unknown profile name (one that
// is neither built in nor backed by per-profile env vars) returns an error
// that names the bad profile but never any key material.
func (c Config) ApplyProfile() (Config, error) {
	if c.Profile == "" {
		return c, nil
	}
	if err := validProfileName(c.Profile); err != nil {
		return Config{}, err
	}

	prefix := profileEnvKey(c.Profile)
	builtin, isBuiltin := builtinProfiles[c.Profile]

	// A custom profile is recognized only if it defines at least a base URL
	// via env. Otherwise the name is unknown and we fail loudly.
	envBase := os.Getenv(prefix + "_BASE_URL")
	if !isBuiltin && envBase == "" {
		return Config{}, fmt.Errorf("unknown profile %q: no built-in profile and %s_BASE_URL is not set", c.Profile, prefix)
	}

	c.BaseURL = firstNonEmpty(envBase, builtin.BaseURL)
	c.Model = firstNonEmpty(os.Getenv(prefix+"_MODEL"), builtin.Model)
	c.APIKey = os.Getenv(prefix + "_API_KEY") // never defaulted; secrets only from env

	if v := os.Getenv(prefix + "_TIMEOUT"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			c.HTTPTimeout = time.Duration(n) * time.Second
		}
	} else if builtin.Timeout > 0 {
		c.HTTPTimeout = builtin.Timeout
	}

	return c, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Validate checks that required fields are set and enumerated fields are valid.
// Call it after command-line flags have been merged in.
func (c Config) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base URL is required (the OpenAI-compatible endpoint, e.g. http://10.0.0.5:8000)")
	}
	// Model may be empty here; it is auto-discovered from /v1/models at startup.
	if c.ApprovalMode != "auto" && c.ApprovalMode != "ask" {
		return fmt.Errorf("approval mode must be auto or ask, got %q", c.ApprovalMode)
	}
	if c.MaxIter < 1 {
		return fmt.Errorf("max iterations must be >= 1, got %d", c.MaxIter)
	}
	if c.InputPricePer1K < 0 || c.OutputPricePer1K < 0 {
		return fmt.Errorf("token prices must be >= 0, got input=%v output=%v", c.InputPricePer1K, c.OutputPricePer1K)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return fallback
	}
	return n
}

func envFloatOr(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}
