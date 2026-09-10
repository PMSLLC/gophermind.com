package freellm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// ringMaxAge bounds the event ring at the longest window any provider
	// publishes (Cohere's monthly quota), plus a day of slack.
	ringMaxAge = 31 * 24 * time.Hour
	// ringMaxEvents hard-caps the ring so a heavy user cannot grow the state
	// file without bound.
	ringMaxEvents = 20000
)

// Event is one turn's free usage, recorded for the trip meters.
type Event struct {
	TS       time.Time `json:"ts"`
	Profile  string    `json:"profile"`
	Tokens   int64     `json:"tokens"`
	Requests int64     `json:"requests"`
}

// ProviderTotal is one provider's lifetime contribution to the odometer.
type ProviderTotal struct {
	Tokens    int64     `json:"tokens"`
	Requests  int64     `json:"requests"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Odometer is the lifetime record of free capacity used. Readings never
// decrease through normal operation: negative counts are ignored, and every
// load merges upward rather than overwriting. That guarantee does not survive
// the state file being destroyed - by a torn write across a crash, or a
// hostile edit - because nothing else records this data, and a destroyed
// file legitimately starts over from zero. This is a cosmetic usage counter,
// not billing data, so that gap is accepted rather than paid for with a
// backup file or a separate high-water mark.
//
// Events is a bounded ring of recent turns, kept so the trip meters can be
// derived without depending on GOPHERMIND_USAGE_LOG, which is off by default.
type Odometer struct {
	Tokens   int64                    `json:"tokens"`
	Requests int64                    `json:"requests"`
	Per      map[string]ProviderTotal `json:"per"`
	Since    time.Time                `json:"since"`
	Events   []Event                  `json:"events"`
}

// DefaultOdometerPath is where the odometer lives: alongside the completion
// cache, under the OS user cache directory. If the cache directory cannot be
// determined, it falls back to the user's home directory rather than a
// relative path - a relative fallback would follow the current working
// directory, so running gophermind from a different folder would silently
// read as a fresh install (reading 0) instead of finding the real odometer.
func DefaultOdometerPath() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "gophermind", "free-odometer.json")
	}
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		return filepath.Join(dir, ".gophermind", "free-odometer.json")
	}
	return filepath.Join(".gophermind", "free-odometer.json")
}

// LoadOdometer reads the odometer at path. A missing file yields a fresh
// odometer. A corrupt or truncated file also yields a usable odometer rather
// than an error: refusing to start because a cache file is damaged would be
// worse than starting from what is left. The error return is always nil
// today - even a real read failure below yields a fresh odometer rather than
// propagating - and is kept in the signature for callers that need it if
// that tradeoff ever changes.
func LoadOdometer(path string) (*Odometer, error) {
	o := &Odometer{Per: map[string]ProviderTotal{}, Since: time.Now()}
	b, err := os.ReadFile(path)
	if err != nil {
		// A missing file (os.IsNotExist) is the normal "no odometer yet"
		// case. Anything else here - permission denied, path is a directory,
		// and so on - is a real problem, not a fresh install, and currently
		// reads exactly like one: both return a usable fresh odometer with a
		// nil error, silently zeroing the reading rather than surfacing the
		// failure. That is a deliberate tradeoff (starting from zero beats
		// refusing to run) rather than an oversight.
		return o, nil
	}
	var stored Odometer
	if err := json.Unmarshal(b, &stored); err != nil {
		return o, nil
	}
	if stored.Per == nil {
		stored.Per = map[string]ProviderTotal{}
	}
	if stored.Since.IsZero() {
		stored.Since = time.Now()
	}
	return &stored, nil
}

// Add records one turn and persists the result. It is monotonic by
// construction: non-positive counts are ignored, and no field is ever written
// lower than its stored value.
//
// The read-modify-write is guarded by a lock file so concurrent gophermind
// sessions sharing a cache directory cannot lose an increment.
func (o *Odometer) Add(path string, e Event) error {
	if e.Tokens <= 0 && e.Requests <= 0 {
		return nil
	}
	if e.Tokens < 0 {
		e.Tokens = 0
	}
	if e.Requests < 0 {
		e.Requests = 0
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("freellm: create odometer dir: %w", err)
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	// Re-read under the lock so a concurrent writer's increments are not lost,
	// then merge our in-memory state up (never down).
	onDisk, _ := LoadOdometer(path)
	o.mergeUp(onDisk)
	o.addLocked(e)
	return o.save(path)
}

// mergeUp raises every field of o to at least the corresponding field of other.
// It never lowers anything, which is what keeps the reading monotonic when a
// stale in-memory copy meets a newer file, or the reverse.
func (o *Odometer) mergeUp(other *Odometer) {
	if other == nil {
		return
	}
	if other.Tokens > o.Tokens {
		o.Tokens = other.Tokens
	}
	if other.Requests > o.Requests {
		o.Requests = other.Requests
	}
	if o.Per == nil {
		o.Per = map[string]ProviderTotal{}
	}
	for k, v := range other.Per {
		cur := o.Per[k]
		if v.Tokens > cur.Tokens {
			cur.Tokens = v.Tokens
		}
		if v.Requests > cur.Requests {
			cur.Requests = v.Requests
		}
		if cur.FirstSeen.IsZero() || (!v.FirstSeen.IsZero() && v.FirstSeen.Before(cur.FirstSeen)) {
			cur.FirstSeen = v.FirstSeen
		}
		if v.LastSeen.After(cur.LastSeen) {
			cur.LastSeen = v.LastSeen
		}
		o.Per[k] = cur
	}
	if !other.Since.IsZero() && (o.Since.IsZero() || other.Since.Before(o.Since)) {
		o.Since = other.Since
	}
	// Keep whichever ring has more events. This is deliberately NOT a union:
	// the lifetime totals above are the monotonic guarantee, and the ring only
	// feeds trip meters, which may under-count slightly when two sessions write
	// concurrently. Under-counting a trip meter is safe; over-counting a
	// lifetime odometer would not be.
	if len(other.Events) > len(o.Events) {
		o.Events = other.Events
	}
}

// addLocked applies one event to the in-memory state, including ring
// maintenance. Callers must hold the lock. Exposed to tests for the ring cap.
func (o *Odometer) addLocked(e Event) {
	o.Tokens += e.Tokens
	o.Requests += e.Requests
	if o.Per == nil {
		o.Per = map[string]ProviderTotal{}
	}
	t := o.Per[e.Profile]
	t.Tokens += e.Tokens
	t.Requests += e.Requests
	if t.FirstSeen.IsZero() {
		t.FirstSeen = e.TS
	}
	if e.TS.After(t.LastSeen) {
		t.LastSeen = e.TS
	}
	o.Per[e.Profile] = t

	o.Events = append(o.Events, e)
	o.pruneRing(time.Now())
}

// pruneRing drops events past ringMaxAge and enforces ringMaxEvents. It only
// ever touches Events; the lifetime totals are unaffected, which is why an
// odometer reading survives ring eviction.
func (o *Odometer) pruneRing(now time.Time) {
	cutoff := now.Add(-ringMaxAge)
	kept := o.Events[:0]
	for _, e := range o.Events {
		if e.TS.After(cutoff) {
			kept = append(kept, e)
		}
	}
	o.Events = kept
	if len(o.Events) > ringMaxEvents {
		o.Events = o.Events[len(o.Events)-ringMaxEvents:]
	}
}

// Reading returns the lifetime totals.
func (o *Odometer) Reading() (tokens, requests int64) { return o.Tokens, o.Requests }

// save writes the odometer atomically: a temp file in the same directory, then
// a rename, so a crash mid-write cannot leave a half-written state file.
func (o *Odometer) save(path string) error {
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("freellm: marshal odometer: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".odo-*")
	if err != nil {
		return fmt.Errorf("freellm: create temp odometer: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
