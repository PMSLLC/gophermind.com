// Package config loads runtime configuration from the environment, with
// command-line flags layered on top by the caller.
package config

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sampling parameter bounds. Temperature is the OpenAI-conventional [0,2];
// top_p is a probability mass in (0,1]. These are the single source of truth
// for both config validation and the TUI /temp and /topp commands.
const (
	TemperatureMin = 0.0
	TemperatureMax = 2.0
	TopPMin        = 0.0 // exclusive lower bound (0 would mask all tokens)
	TopPMax        = 1.0
)

// ValidateTemperature rejects NaN, Inf, and out-of-range temperatures. It never
// clamps silently: a bad value is an explicit error so garbage never reaches
// the API. 0 is valid and meaningful (deterministic).
func ValidateTemperature(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("temperature must be a finite number, got %v", v)
	}
	if v < TemperatureMin || v > TemperatureMax {
		return fmt.Errorf("temperature must be in [%g,%g], got %v", TemperatureMin, TemperatureMax, v)
	}
	return nil
}

// ValidateTopP rejects NaN, Inf, and out-of-range top_p values. The lower bound
// is exclusive (top_p of 0 masks every token); the upper bound 1.0 is inclusive.
func ValidateTopP(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("top_p must be a finite number, got %v", v)
	}
	if v <= TopPMin || v > TopPMax {
		return fmt.Errorf("top_p must be in (%g,%g], got %v", TopPMin, TopPMax, v)
	}
	return nil
}

// defaultBaseURL is intentionally empty: no endpoint is baked in. A user supplies
// their own OpenAI-compatible endpoint via the first-run setup wizard, a provider
// profile, GOPHERMIND_BASE_URL, or -base. Validate requires one to be set.
const defaultBaseURL = ""

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
	Profile        string   // GOPHERMIND_PROFILE: selected provider profile ("" => legacy/default endpoint)
	BaseURL        string   // GOPHERMIND_BASE_URL (required), e.g. http://10.0.0.5:8000
	APIKey         string   // GOPHERMIND_API_KEY (optional; empty when reached over VPN)
	Model          string   // GOPHERMIND_MODEL
	FallbackModels []string // GOPHERMIND_FALLBACK_MODELS: comma-separated, tried in order after Model on a fallback-eligible failure
	SpeedModel     string   // GOPHERMIND_SPEED_MODEL: faster model selected by --speed (falls back to the first FallbackModels entry)
	RootDir        string   // GOPHERMIND_ROOT (default: cwd)
	ApprovalMode   string   // GOPHERMIND_APPROVAL: auto|ask (default: ask)
	InsecureTLS    bool     // GOPHERMIND_INSECURE_TLS: skip TLS verify (self-signed internal endpoints)

	// Optional mutual-TLS / custom-CA for reaching internal endpoints SECURELY
	// (the safe alternative to InsecureTLS). ClientCertPath + ClientKeyPath
	// enable client-certificate auth and are required together; CACertPath adds
	// a private CA to trust for the server while keeping verification ON. The
	// key path's CONTENTS are never logged; see internal/llm.TLSOptions.
	ClientCertPath string // GOPHERMIND_CLIENT_CERT: PEM client certificate (with GOPHERMIND_CLIENT_KEY)
	ClientKeyPath  string // GOPHERMIND_CLIENT_KEY: PEM client private key (with GOPHERMIND_CLIENT_CERT)
	CACertPath     string // GOPHERMIND_CA_CERT: PEM CA bundle to trust for the server (appended to system roots)

	MaxIter     int           // GOPHERMIND_MAX_ITER (default: 25)
	HTTPTimeout time.Duration // GOPHERMIND_HTTP_TIMEOUT_S (default: 300s)
	CmdTimeout  time.Duration // GOPHERMIND_CMD_TIMEOUT_S (default: 120s)

	FetchAllowHosts []string // GOPHERMIND_FETCH_ALLOW_HOSTS: comma-separated host allowlist for fetch_url (empty => any host)

	// Optional resource ceilings for run_shell, applied via ulimit. 0 = no limit.
	ShellCPUSeconds int // GOPHERMIND_SHELL_CPU_SECONDS
	ShellMaxMemMB   int // GOPHERMIND_SHELL_MAX_MEM_MB
	ShellMaxProcs   int // GOPHERMIND_SHELL_MAX_PROCS

	// Bounded retry with exponential backoff for the LLM client. MaxAttempts is
	// the total number of tries (1 disables retries; a single attempt still
	// works). RetryBaseDelay is the first backoff interval; later attempts grow
	// it exponentially (with jitter) up to an internal cap.
	MaxAttempts    int           // GOPHERMIND_MAX_ATTEMPTS (default: 3; min 1)
	RetryBaseDelay time.Duration // GOPHERMIND_RETRY_BASE_DELAY_MS (default: 250ms)

	// Per-1,000-token prices (USD) for the running cost meter. Both default to
	// 0, so the meter reports $0.00 until configured.
	InputPricePer1K  float64 // GOPHERMIND_PRICE_INPUT_PER_1K
	OutputPricePer1K float64 // GOPHERMIND_PRICE_OUTPUT_PER_1K

	// Sampling parameters sent with each completion. Temperature defaults to 0
	// (deterministic) and is always sent, preserving prior behavior. TopP is a
	// pointer so "unset" (nil) is distinguishable from an explicit 0.0: when nil
	// it is omitted from the request entirely, again matching prior behavior.
	// Both are runtime-adjustable via the TUI /temp and /topp commands.
	Temperature float64  // GOPHERMIND_TEMPERATURE (default: 0; range [0,2])
	TopP        *float64 // GOPHERMIND_TOP_P (default: unset/nil; range (0,1] when set)

	// Response cache for non-streaming completions, keyed by a hash of the
	// request inputs. Off by default: caching writes prompt/response content to
	// disk (a privacy consideration), and stale entries can surprise normal use,
	// so it is opt-in for iterative dev and tests.
	CacheEnabled bool          // GOPHERMIND_CACHE_ENABLED (default: false)
	CacheDir     string        // GOPHERMIND_CACHE_DIR (default: <os user cache>/gophermind/cache, else .gophermind/cache under root)
	CacheTTL     time.Duration // GOPHERMIND_CACHE_TTL (default: 24h)

	// TranscriptPath, when non-empty, is a user-chosen destination for a
	// JSONL dump of the full wire-level message history at session end. The
	// transcript contains full prompt and response content (potentially
	// sensitive); the writer creates the file with 0600 perms and any parent
	// dir with 0700. Empty (the default) means no transcript is written and
	// there is zero overhead. It is an explicit, user-provided output path
	// (like `-o outfile`), so it is NOT contained to the repo root.
	TranscriptPath string // GOPHERMIND_TRANSCRIPT (default: unset; also --transcript)
}

// Load reads configuration from the environment and applies defaults. The
// returned Config is not yet validated; call Validate after flags are applied.
func Load() (Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("getwd: %w", err)
	}

	// Seed the environment from optional .env files before reading any variable,
	// so even GOPHERMIND_ROOT can be set there. Precedence (highest first): real
	// environment > working-directory .env > global config .env > built-in
	// defaults. Because each loader only fills gaps, the working-directory file
	// wins over the global one, and real env wins over both. Missing files are
	// not errors.
	if err := loadDotEnvFile(filepath.Join(wd, ".env")); err != nil {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	if p, perr := ConfigFilePath(); perr == nil {
		if err := loadDotEnvFile(p); err != nil {
			return Config{}, fmt.Errorf("load global config: %w", err)
		}
	}

	root := envOr("GOPHERMIND_ROOT", "")
	if root == "" {
		root = wd
	}

	return Config{
		Profile:         envOr("GOPHERMIND_PROFILE", ""),
		BaseURL:         envOr("GOPHERMIND_BASE_URL", defaultBaseURL),
		APIKey:          envOr("GOPHERMIND_API_KEY", ""),
		Model:           envOr("GOPHERMIND_MODEL", ""), // empty => auto-discover from /v1/models
		FallbackModels:  envList("GOPHERMIND_FALLBACK_MODELS"),
		SpeedModel:      envOr("GOPHERMIND_SPEED_MODEL", ""),
		RootDir:         root,
		ApprovalMode:    envOr("GOPHERMIND_APPROVAL", "ask"),
		InsecureTLS:     envBool("GOPHERMIND_INSECURE_TLS"),
		ClientCertPath:  envOr("GOPHERMIND_CLIENT_CERT", ""),
		ClientKeyPath:   envOr("GOPHERMIND_CLIENT_KEY", ""),
		CACertPath:      envOr("GOPHERMIND_CA_CERT", ""),
		MaxIter:         envIntOr("GOPHERMIND_MAX_ITER", 25),
		HTTPTimeout:     time.Duration(envIntOr("GOPHERMIND_HTTP_TIMEOUT_S", 300)) * time.Second,
		CmdTimeout:      time.Duration(envIntOr("GOPHERMIND_CMD_TIMEOUT_S", 120)) * time.Second,
		FetchAllowHosts: envList("GOPHERMIND_FETCH_ALLOW_HOSTS"),
		ShellCPUSeconds: envIntOr("GOPHERMIND_SHELL_CPU_SECONDS", 0),
		ShellMaxMemMB:   envIntOr("GOPHERMIND_SHELL_MAX_MEM_MB", 0),
		ShellMaxProcs:   envIntOr("GOPHERMIND_SHELL_MAX_PROCS", 0),

		MaxAttempts:    envIntOr("GOPHERMIND_MAX_ATTEMPTS", 3),
		RetryBaseDelay: time.Duration(envIntOr("GOPHERMIND_RETRY_BASE_DELAY_MS", 250)) * time.Millisecond,

		InputPricePer1K:  envFloatOr("GOPHERMIND_PRICE_INPUT_PER_1K", 0),
		OutputPricePer1K: envFloatOr("GOPHERMIND_PRICE_OUTPUT_PER_1K", 0),

		Temperature: envFloatOr("GOPHERMIND_TEMPERATURE", 0),
		TopP:        envFloatPtr("GOPHERMIND_TOP_P"),

		CacheEnabled: envBool("GOPHERMIND_CACHE_ENABLED"),
		CacheDir:     envOr("GOPHERMIND_CACHE_DIR", defaultCacheDir(root)),
		CacheTTL:     envDurationOr("GOPHERMIND_CACHE_TTL", 24*time.Hour),

		TranscriptPath: envOr("GOPHERMIND_TRANSCRIPT", ""),
	}, nil
}

// loadDotEnvFile reads KEY=VALUE pairs from the .env file at path and sets any
// that are not already present in the process environment. Real (already-exported)
// environment variables always take precedence — a .env only fills gaps — so a
// deployment's real config can never be silently overridden by a stray file. A
// missing file is not an error (it is optional); malformed lines are skipped
// rather than failing the whole load. This is deliberately the only implicit
// source in config loading; every other value is an explicit os.Getenv.
//
// Supported syntax (a practical subset of dotenv): blank lines and lines whose
// first non-space char is '#' are ignored; an optional leading "export " is
// stripped; the value is everything after the first '='; surrounding single or
// double quotes are removed. No variable interpolation is performed.
func loadDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue // no '=' — not a KEY=VALUE line
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, present := os.LookupEnv(key); present {
			continue // real environment wins
		}
		if err := os.Setenv(key, unquoteEnv(strings.TrimSpace(val))); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// unquoteEnv strips a single matching pair of surrounding single or double
// quotes from a .env value, leaving everything else (including inner quotes)
// untouched. Unquoted and mismatched values are returned as-is.
func unquoteEnv(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// ConfigFilePath returns the path to gophermind's global config .env, written by
// the first-run setup wizard and read by Load as a gap-filler. It lives under
// the OS user config dir (e.g. ~/.config/gophermind/.env), so a user configures
// once and it applies in every directory.
func ConfigFilePath() (string, error) {
	// GOPHERMIND_CONFIG_DIR overrides the location (the .env is placed directly
	// inside it). Useful for relocating config and for hermetic tests.
	if dir := os.Getenv("GOPHERMIND_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".env"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind", ".env"), nil
}

// GlobalConfigExists reports whether the global config .env has been written.
// It is the "already configured" signal for the first-run wizard trigger.
func GlobalConfigExists() bool {
	p, err := ConfigFilePath()
	if err != nil {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// BuiltinProfileNames returns the built-in provider profiles as {name, baseURL}
// pairs in stable (name-sorted) order, for the setup wizard's endpoint menu.
func BuiltinProfileNames() [][2]string {
	names := make([]string, 0, len(builtinProfiles))
	for name := range builtinProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([][2]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, [2]string{name, builtinProfiles[name].BaseURL})
	}
	return pairs
}

// defaultCacheDir picks a contained location for cached completions: the OS user
// cache dir under gophermind/cache when available, otherwise .gophermind/cache
// under the repo root. Both keep cache files out of the working tree's path of
// fire and within a predictable, per-user location.
func defaultCacheDir(root string) string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "gophermind", "cache")
	}
	return filepath.Join(root, ".gophermind", "cache")
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
	if c.RetryBaseDelay < 0 {
		return fmt.Errorf("retry base delay must be >= 0, got %v", c.RetryBaseDelay)
	}
	// Client-certificate auth requires BOTH a cert and a key; one without the
	// other is a configuration error. (The files' existence and PEM validity are
	// checked at client construction so startup fails fast.) The error names the
	// env vars but never any file contents.
	if (c.ClientCertPath != "") != (c.ClientKeyPath != "") {
		return fmt.Errorf("client certificate auth requires BOTH GOPHERMIND_CLIENT_CERT and GOPHERMIND_CLIENT_KEY (got only one)")
	}
	if err := ValidateTemperature(c.Temperature); err != nil {
		return err
	}
	if c.TopP != nil {
		if err := ValidateTopP(*c.TopP); err != nil {
			return err
		}
	}
	// MaxAttempts is normalized (values < 1 mean a single attempt) by the
	// client's RetryPolicy, so it is intentionally not rejected here.
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envList parses a comma-separated env var into a slice: each element is
// trimmed and empties are dropped. An unset/empty var yields a nil slice (no
// fallback). It does not dedup or cap — the llm client owns those concerns.
func envList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
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

// envFloatPtr parses an optional float env var into a *float64: an unset or
// empty var yields nil (the value is "not configured"), distinguishing it from
// an explicit 0. A malformed value also yields nil so a typo can't silently
// flip behavior; range validation happens later in Validate.
func envFloatPtr(key string) *float64 {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &n
}

// envDurationOr parses a Go duration string (e.g. "24h", "30m"). An empty,
// malformed, or negative value falls back to the default.
func envDurationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}
