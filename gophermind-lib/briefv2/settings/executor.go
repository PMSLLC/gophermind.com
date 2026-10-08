package settings

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExecutorConfig is the executor: section of gophermind.yaml. Every integer
// is positive; Validate also refuses absurd values.
type ExecutorConfig struct {
	Workers                  int           `yaml:"workers"`
	FixAttempts              int           `yaml:"fix_attempts"`
	RepairRounds             int           `yaml:"repair_rounds"`
	AcceptanceRepairRounds   int           `yaml:"acceptance_repair_rounds"`
	TestTimeoutSeconds       int           `yaml:"test_timeout_seconds"`
	AcceptanceTimeoutSeconds int           `yaml:"acceptance_timeout_seconds"`
	StaleClaimSeconds        int           `yaml:"stale_claim_seconds"`
	OutputCapBytes           int           `yaml:"output_cap_bytes"`
	HeartbeatSeconds         int           `yaml:"heartbeat_seconds"`
	GoModCache               string        `yaml:"go_mod_cache"` // starts with ~/; the executor expands it
	MaxRunMinutes            int           `yaml:"max_run_minutes"`
	Sandbox                  string        `yaml:"sandbox"`            // "on" | "off"
	Artifacts                string        `yaml:"artifacts"`          // "on" | "off": save the reply and check output of each attempt under attempts/
	ArtifactMaxBytes         int           `yaml:"artifact_max_bytes"` // cap on each saved file
	Proxy                    ExecutorProxy `yaml:"proxy"`
}

// ExecutorProxy configures the harness module proxy.
type ExecutorProxy struct {
	Listen string `yaml:"listen"` // loopback host:port
	Log    string `yaml:"log"`    // file name inside the run folder
}

// DefaultToolchainPATH is the only toolchain variable a settings file holds;
// the executor adds the per-run ones.
const DefaultToolchainPATH = "/usr/local/go/bin:/usr/bin:/bin"

// DefaultExecutor returns the defaults of the executor design, section 11.
func DefaultExecutor() ExecutorConfig {
	return ExecutorConfig{
		Workers: 1, FixAttempts: 2, RepairRounds: 2, AcceptanceRepairRounds: 2,
		TestTimeoutSeconds: 120, AcceptanceTimeoutSeconds: 300, StaleClaimSeconds: 120,
		OutputCapBytes: 65536, HeartbeatSeconds: 30, GoModCache: "~/.gophermind/gomodcache",
		MaxRunMinutes: 720, Sandbox: "on", Artifacts: "on", ArtifactMaxBytes: 65536,
		Proxy: ExecutorProxy{Listen: "127.0.0.1:0", Log: "proxy.log"},
	}
}

// applyExecutorDefaults fills every zero value of the executor section and a
// nil toolchain. A negative value is left alone so Validate can refuse it.
func (c *Config) applyExecutorDefaults() {
	d := DefaultExecutor()
	e := &c.Executor
	for _, f := range []struct {
		v   *int
		def int
	}{
		{&e.Workers, d.Workers}, {&e.FixAttempts, d.FixAttempts}, {&e.RepairRounds, d.RepairRounds},
		{&e.AcceptanceRepairRounds, d.AcceptanceRepairRounds}, {&e.TestTimeoutSeconds, d.TestTimeoutSeconds},
		{&e.AcceptanceTimeoutSeconds, d.AcceptanceTimeoutSeconds}, {&e.StaleClaimSeconds, d.StaleClaimSeconds},
		{&e.OutputCapBytes, d.OutputCapBytes}, {&e.HeartbeatSeconds, d.HeartbeatSeconds},
		{&e.MaxRunMinutes, d.MaxRunMinutes}, {&e.ArtifactMaxBytes, d.ArtifactMaxBytes},
	} {
		if *f.v == 0 {
			*f.v = f.def
		}
	}
	if e.GoModCache == "" {
		e.GoModCache = d.GoModCache
	}
	if e.Sandbox == "" {
		e.Sandbox = d.Sandbox
	}
	if e.Artifacts == "" {
		e.Artifacts = d.Artifacts
	}
	if e.Proxy.Listen == "" {
		e.Proxy.Listen = d.Proxy.Listen
	}
	if e.Proxy.Log == "" {
		e.Proxy.Log = d.Proxy.Log
	}
	if c.Toolchain == nil {
		c.Toolchain = map[string]string{"PATH": DefaultToolchainPATH}
	}
}

// validate checks the section. Messages name the key only, never its value.
func (e ExecutorConfig) validate() error {
	for _, f := range []struct {
		key string
		v   int
		max int
	}{
		{"workers", e.Workers, 64},
		{"fix_attempts", e.FixAttempts, 20},
		{"repair_rounds", e.RepairRounds, 20},
		{"acceptance_repair_rounds", e.AcceptanceRepairRounds, 20},
		{"test_timeout_seconds", e.TestTimeoutSeconds, 86400},
		{"acceptance_timeout_seconds", e.AcceptanceTimeoutSeconds, 86400},
		{"stale_claim_seconds", e.StaleClaimSeconds, 86400},
		{"output_cap_bytes", e.OutputCapBytes, 64 << 20},
		{"heartbeat_seconds", e.HeartbeatSeconds, 3600},
		{"max_run_minutes", e.MaxRunMinutes, 10080},
		{"artifact_max_bytes", e.ArtifactMaxBytes, 16 << 20},
	} {
		if f.v < 1 || f.v > f.max {
			return fmt.Errorf("executor.%s must be between 1 and %d", f.key, f.max)
		}
	}
	// Leaf model loops share one working tree (stray-file check, stub swap) and
	// the mini serves one model call at a time: workers stays 1 until leaves
	// get isolated trees.
	if e.Workers != 1 {
		return fmt.Errorf("executor.workers must be 1 until leaf-isolated trees exist")
	}
	if e.GoModCache == "" {
		return fmt.Errorf("executor.go_mod_cache is required")
	}
	if e.Sandbox != "on" && e.Sandbox != "off" {
		return fmt.Errorf("executor.sandbox must be on or off")
	}
	if e.Artifacts != "on" && e.Artifacts != "off" {
		return fmt.Errorf("executor.artifacts must be on or off")
	}
	if !loopbackHostPort(e.Proxy.Listen) {
		return fmt.Errorf("executor.proxy.listen must be a loopback host:port")
	}
	if e.Proxy.Log == "" || e.Proxy.Log == "." || strings.Contains(e.Proxy.Log, "..") || strings.ContainsAny(e.Proxy.Log, `/\`) {
		return fmt.Errorf("executor.proxy.log must be a plain file name")
	}
	return nil
}

func loopbackHostPort(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateToolchain(t map[string]string) error {
	if t["PATH"] == "" {
		return fmt.Errorf("toolchain.PATH is required")
	}
	return nil
}

// checkExplicitZeroCounts refuses an explicit 0 for a retry, attempt or round
// count, so a leaf can never be abandoned by a typo. An omitted key is fine
// (it takes the default); negatives are refused by validate.
func checkExplicitZeroCounts(raw []byte) error {
	var doc struct {
		Executor map[string]any `yaml:"executor"`
		Defaults map[string]any `yaml:"defaults"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil // the strict decode already reported any syntax problem
	}
	for _, key := range []string{"fix_attempts", "repair_rounds", "acceptance_repair_rounds"} {
		if v, ok := doc.Executor[key]; ok {
			if n, isInt := v.(int); isInt && n == 0 {
				return fmt.Errorf("executor.%s must be at least 1", key)
			}
		}
	}
	for _, key := range []string{"clarify_max_calls", "clarify_max_questions", "enrich_batch_size"} {
		if v, ok := doc.Defaults[key]; ok {
			if n, isInt := v.(int); isInt && n == 0 {
				return fmt.Errorf("defaults.%s must be at least 1", key)
			}
		}
	}
	return nil
}
