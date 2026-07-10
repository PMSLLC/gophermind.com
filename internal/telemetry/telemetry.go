// Package telemetry records anonymous, LOCAL-ONLY feature/latency counters to a
// file — opt-in (disabled unless explicitly enabled) and never sent anywhere, so
// usage can be understood without any network egress or PII.
package telemetry

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

// Recorder accumulates event counts and writes them to a local JSON file.
// A nil Recorder is a no-op, so callers never need to nil-check.
type Recorder struct {
	mu     sync.Mutex
	path   string
	counts map[string]int
}

// New returns a Recorder writing to path, or nil when enabled is false (the
// default) — telemetry is strictly opt-in.
func New(path string, enabled bool) *Recorder {
	if !enabled || path == "" {
		return nil
	}
	r := &Recorder{path: path, counts: map[string]int{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &r.counts)
	}
	return r
}

// Incr records one occurrence of a named event and persists the counts.
func (r *Recorder) Incr(event string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[event]++
	data, err := json.MarshalIndent(r.counts, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(r.path, data, 0o644)
}

// Report renders the recorded counts as a sorted summary.
func Report(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "(no telemetry recorded)\n", nil
		}
		return "", err
	}
	var counts map[string]int
	if err := json.Unmarshal(data, &counts); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += k + ": " + itoa(counts[k]) + "\n"
	}
	if out == "" {
		out = "(no events)\n"
	}
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
