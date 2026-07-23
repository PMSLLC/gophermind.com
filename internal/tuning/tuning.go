// Package tuning computes gophermind's performance-related environment
// settings for a named profile and merges them into a .env file.
//
// The profile split is deliberate: asking for speed must never silently remove
// a safety control. safe/balanced/aggressive differ only in throughput knobs and
// all keep the approval gate and TLS verification on; removing the human gate
// requires explicitly naming the "unattended" profile.
package tuning

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Profile is a named tuning preset.
type Profile struct {
	Name        string
	Description string

	MaxIter        int
	MaxAttempts    int
	RetryBaseMS    int
	HTTPTimeoutS   int
	CmdTimeoutS    int
	IdleTimeoutS   int
	CacheEnabled   bool
	CacheTTL       string
	AutoApprove    bool
	CompressCtx    bool
	ShellMaxProcs  int
	NetMaxRequests int
}

var profiles = []Profile{
	{
		Name:        "safe",
		Description: "conservative: short timeouts, few retries, tight ceilings; approval gate on",
		MaxIter:     10, MaxAttempts: 2, RetryBaseMS: 500,
		HTTPTimeoutS: 120, CmdTimeoutS: 60, IdleTimeoutS: 120,
		CacheEnabled: true, CacheTTL: "6h",
		CompressCtx: false, ShellMaxProcs: 32, NetMaxRequests: 50,
	},
	{
		Name:        "balanced",
		Description: "the shipped defaults",
		MaxIter:     25, MaxAttempts: 3, RetryBaseMS: 250,
		HTTPTimeoutS: 300, CmdTimeoutS: 120, IdleTimeoutS: 300,
		CacheEnabled: true, CacheTTL: "24h",
		CompressCtx: true, ShellMaxProcs: 64, NetMaxRequests: 100,
	},
	{
		Name:        "aggressive",
		Description: "maximum throughput: long timeouts, high ceilings, caching and compression on; approval gate still on",
		MaxIter:     60, MaxAttempts: 5, RetryBaseMS: 150,
		HTTPTimeoutS: 900, CmdTimeoutS: 600, IdleTimeoutS: 600,
		CacheEnabled: true, CacheTTL: "72h",
		CompressCtx: true, ShellMaxProcs: 256, NetMaxRequests: 500,
	},
	{
		Name:        "unattended",
		Description: "aggressive plus auto-approval, for runs with nobody watching — the agent writes files and runs shell commands with no gate",
		MaxIter:     60, MaxAttempts: 5, RetryBaseMS: 150,
		HTTPTimeoutS: 900, CmdTimeoutS: 600, IdleTimeoutS: 600,
		CacheEnabled: true, CacheTTL: "72h",
		AutoApprove: true,
		CompressCtx: true, ShellMaxProcs: 256, NetMaxRequests: 500,
	},
}

// Lookup resolves a profile by name.
func Lookup(name string) (Profile, bool) {
	for _, p := range profiles {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Profile{}, false
}

// Names lists the profiles in preset order, for help text and errors.
func Names() []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}
	return out
}

// Describe renders "name — description" lines for help output.
func Describe() string {
	var b strings.Builder
	for _, p := range profiles {
		fmt.Fprintf(&b, "  %-11s %s\n", p.Name, p.Description)
	}
	return b.String()
}

// Probe carries what was measured about the live endpoint and machine. A zero
// Probe means "nothing measured": profile values are used as-is.
type Probe struct {
	TokensPerSecond float64 // measured generation throughput
	ContextWindow   int     // model's reported context window
	MaxOutputTokens int
	NumCPU          int
}

// Settings renders the environment variables for a profile, adjusted by
// whatever the probe measured.
func Settings(p Profile, pr Probe) map[string]string {
	idle := p.IdleTimeoutS
	httpT := p.HTTPTimeoutS

	// A slow endpoint needs a wider idle window, or a long generation is cut
	// off mid-stream. Scale relative to a 30 tok/s reference, capped so a
	// pathological measurement cannot produce an effectively infinite timeout.
	if pr.TokensPerSecond > 0 && pr.TokensPerSecond < 30 {
		factor := 30 / pr.TokensPerSecond
		if factor > 4 {
			factor = 4
		}
		idle = int(float64(idle) * factor)
		httpT = int(float64(httpT) * factor)
	}

	procs := p.ShellMaxProcs
	if cpu := pr.NumCPU; cpu > 0 {
		// Keep the process ceiling proportional to the machine.
		if want := cpu * 32; want > procs {
			procs = want
		}
	}

	approval := "ask"
	if p.AutoApprove {
		approval = "auto"
	}

	return map[string]string{
		"GOPHERMIND_MAX_ITER":              strconv.Itoa(p.MaxIter),
		"GOPHERMIND_MAX_ATTEMPTS":          strconv.Itoa(p.MaxAttempts),
		"GOPHERMIND_RETRY_BASE_DELAY_MS":   strconv.Itoa(p.RetryBaseMS),
		"GOPHERMIND_HTTP_TIMEOUT_S":        strconv.Itoa(httpT),
		"GOPHERMIND_CMD_TIMEOUT_S":         strconv.Itoa(p.CmdTimeoutS),
		"GOPHERMIND_STREAM_IDLE_TIMEOUT_S": strconv.Itoa(idle),
		"GOPHERMIND_CACHE_ENABLED":         boolEnv(p.CacheEnabled),
		"GOPHERMIND_CACHE_TTL":             p.CacheTTL,
		"GOPHERMIND_COMPRESS_CONTEXT":      boolEnv(p.CompressCtx),
		"GOPHERMIND_SHELL_MAX_PROCS":       strconv.Itoa(procs),
		"GOPHERMIND_NET_MAX_REQUESTS":      strconv.Itoa(p.NetMaxRequests),
		"GOPHERMIND_APPROVAL":              approval,
		// Never relaxed by any profile: skipping TLS verification is not a
		// performance setting, and the guards defend against untrusted content.
		"GOPHERMIND_INSECURE_TLS":    "0",
		"GOPHERMIND_EGRESS_GUARD":    "1",
		"GOPHERMIND_INJECTION_GUARD": "1",
		"GOPHERMIND_TEMPERATURE":     "0",
		"GOPHERMIND_HISTORY":         "on",
		"GOPHERMIND_AUTO_CHECKPOINT": "1",
	}
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// DefaultProbe fills in what can be measured without touching the network.
func DefaultProbe() Probe { return Probe{NumCPU: runtime.NumCPU()} }

// MergeIntoEnvFile writes values into the .env file at path, updating keys in
// place and appending the rest. It returns how many lines it changed or added.
//
// Rules that protect a live .env:
//   - an empty replacement value never overwrites a non-empty existing one, so
//     credentials and endpoints survive optimization
//   - comments, blank lines, ordering and unrelated keys are preserved
//   - the file's existing permissions are kept (a 0600 .env stays 0600)
func MergeIntoEnvFile(path string, values map[string]string) (int, error) {
	perm := os.FileMode(0o600)
	var lines []string

	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if fi, serr := os.Stat(path); serr == nil {
			perm = fi.Mode().Perm()
		}
		lines = strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	case os.IsNotExist(err):
		lines = nil
	default:
		return 0, err
	}

	seen := map[string]bool{}
	changed := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, oldVal, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		newVal, want := values[key]
		if !want {
			continue
		}
		seen[key] = true
		// Never blank out a value the user already set.
		if strings.TrimSpace(newVal) == "" {
			continue
		}
		if strings.TrimSpace(oldVal) == newVal {
			continue
		}
		lines[i] = key + "=" + newVal
		changed++
	}

	// Append anything not already present, in deterministic order.
	var missing []string
	for k, v := range values {
		if seen[k] || strings.TrimSpace(v) == "" {
			continue
		}
		missing = append(missing, k)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "# --- tuned by `gophermind optimize` ---")
		for _, k := range missing {
			lines = append(lines, k+"="+values[k])
			changed++
		}
	}

	out := strings.Join(lines, "\n") + "\n"
	return changed, writeFilePreservingPerm(path, out, perm)
}

// writeFilePreservingPerm writes via temp file + rename so an interrupted write
// cannot truncate a working .env, and restores the original permissions.
func writeFilePreservingPerm(path, content string, perm os.FileMode) error {
	dir := "."
	if i := strings.LastIndex(path, string(os.PathSeparator)); i >= 0 {
		dir = path[:i]
	}
	tmp, err := os.CreateTemp(dir, ".env-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
