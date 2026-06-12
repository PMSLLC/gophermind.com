// Package config loads runtime configuration from the environment, with
// command-line flags layered on top by the caller.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultBaseURL points at the local llama.cpp server. Override with
// GOPHERMIND_BASE_URL or -base.
const defaultBaseURL = "http://10.0.0.5:8081"

// Config holds everything the harness needs to run. Every field has a sensible
// default; an empty Model is auto-discovered from the endpoint at startup.
type Config struct {
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
