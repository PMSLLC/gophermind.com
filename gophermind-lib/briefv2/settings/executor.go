package settings

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
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
	Sandbox                  string        `yaml:"sandbox"` // "on" | "off"
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
		MaxRunMinutes: 720, Sandbox: "on",
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
		{&e.MaxRunMinutes, d.MaxRunMinutes},
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
	} {
		if f.v < 1 || f.v > f.max {
			return fmt.Errorf("executor.%s must be between 1 and %d", f.key, f.max)
		}
	}
	if e.GoModCache == "" {
		return fmt.Errorf("executor.go_mod_cache is required")
	}
	if e.Sandbox != "on" && e.Sandbox != "off" {
		return fmt.Errorf("executor.sandbox must be on or off")
	}
	if !loopbackHostPort(e.Proxy.Listen) {
		return fmt.Errorf("executor.proxy.listen must be a loopback host:port")
	}
	if e.Proxy.Log == "" || e.Proxy.Log == "." || e.Proxy.Log == ".." || filepath.Base(e.Proxy.Log) != e.Proxy.Log {
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
