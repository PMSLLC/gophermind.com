package freellm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/lockfile"
)

// OdometerEnv, when set, overrides where the odometer lives, so tests and
// alternate installs can redirect it.
const OdometerEnv = "GOPHERMIND_ODOMETER"

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
	TS      time.Time `json:"ts"`
	Profile string    `json:"profile"`
	// Model is the model that served this turn. Empty when the caller does not
	// know it, including every event written before per-model metering existed;
	// such an event still counts toward its provider, just not toward a model.
	Model    string `json:"model,omitempty"`
	Tokens   int64  `json:"tokens"`
	Requests int64  `json:"requests"`
}

// ProviderTotal is one provider's lifetime contribution to the odometer.
type ProviderTotal struct {
	Tokens    int64     `json:"tokens"`
	Requests  int64     `json:"requests"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Odometer is the lifetime record of free capacity used. Readings never
// decrease through normal operation: negative counts are ignored, every load
// merges upward rather than overwriting, and a load that could not read the
// file does not write one back. That guarantee does not survive the state
// file being destroyed - by a torn write across a crash, or a hostile edit -
// because nothing else records this data, and a destroyed file legitimately
// starts over from zero; its bytes are set aside as <path>.corrupt first, so
// a human still has them. This is a cosmetic usage counter, not billing
// data, so that gap is accepted rather than paid for with a backup file or a
// separate high-water mark.
//
// Events is a bounded ring of recent turns, kept so the trip meters can be
// derived without depending on GOPHERMIND_USAGE_LOG, which is off by default.
type Odometer struct {
	Tokens   int64                    `json:"tokens"`
	Requests int64                    `json:"requests"`
	Per      map[string]ProviderTotal `json:"per"`
	// PerModel is the lifetime total per model, keyed by ModelKey. It is
	// additive alongside Per rather than replacing it: a state file written
	// before this field existed loads with PerModel nil and its lifetime
	// reading untouched.
	PerModel map[string]ProviderTotal `json:"per_model,omitempty"`
	Since    time.Time                `json:"since"`
	Events   []Event                  `json:"events"`
}

// ModelKey is the PerModel key for one model at one provider. Profile and
// model are joined rather than nested so mergeUp stays a flat loop over one
// map shape.
func ModelKey(profile, model string) string { return profile + "/" + model }

// ModelReading returns the lifetime total for one model. A model that has
// never been used reads as a zero total rather than a missing entry, so a
// caller can render it without a lookup dance.
func (o *Odometer) ModelReading(profile, model string) ProviderTotal {
	if o == nil || o.PerModel == nil {
		return ProviderTotal{}
	}
	return o.PerModel[ModelKey(profile, model)]
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

// OdometerPath resolves where the free-usage odometer lives, honoring
// OdometerEnv (GOPHERMIND_ODOMETER) so tests and alternate installs can
// redirect it, falling back to DefaultOdometerPath otherwise. Both
// cmd/gophermind and internal/tui need this exact resolution, so it lives
// here instead of being duplicated in each caller.
func OdometerPath() string {
	if p := strings.TrimSpace(os.Getenv(OdometerEnv)); p != "" {
		return p
	}
	return DefaultOdometerPath()
}

// corruptSuffix is appended to the odometer path to set damaged bytes aside.
const corruptSuffix = ".corrupt"

// odometerState says what a load found on disk. Starting from zero is only
// correct when there is nothing to start from; the other two states mean a
// reading may still exist, and a save would destroy it.
type odometerState int

const (
	// odoFresh: no file yet, or a zero-length one. Nothing to lose.
	odoFresh odometerState = iota
	// odoIntact: the file was read and parsed.
	odoIntact
	// odoCorrupt: the file was read but will not parse, and is not empty, so
	// its bytes may still be salvageable by a human.
	odoCorrupt
	// odoUnreadable: the file could not be read at all. Whatever reading it
	// holds is still there, untouched, and must stay that way.
	odoUnreadable
)

// loadOdometer is LoadOdometer plus the state its caller needs to decide
// whether saving over the file would destroy a reading.
func loadOdometer(path string) (*Odometer, odometerState, error) {
	fresh := &Odometer{Per: map[string]ProviderTotal{}, Since: time.Now()}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fresh, odoFresh, nil
		}
		return fresh, odoUnreadable, fmt.Errorf("freellm: read odometer %s: %w", path, err)
	}
	var stored Odometer
	if err := json.Unmarshal(b, &stored); err != nil {
		if len(strings.TrimSpace(string(b))) == 0 {
			return fresh, odoFresh, nil
		}
		return fresh, odoCorrupt, nil
	}
	if stored.Per == nil {
		stored.Per = map[string]ProviderTotal{}
	}
	if stored.Since.IsZero() {
		stored.Since = time.Now()
	}
	return &stored, odoIntact, nil
}

// LoadOdometer reads the odometer at path.
//
// A missing or zero-length file yields a fresh odometer with a nil error:
// that is the normal "no odometer yet" case and there is no reading to lose.
//
// A file that is present but will not parse also yields a fresh odometer
// with a nil error. Refusing to start because a cache file is damaged would
// be worse than starting from what is left, and a damaged file's reading is
// not recoverable by this package anyway. Add sets those bytes aside rather
// than dropping them, so a human still can.
//
// A file that cannot be read at all returns an error. That case is not a
// fresh install: the reading is still on disk and intact, and the returned
// odometer reads zero only because nothing could be loaded. Reporting it as
// success is what let Add merge up against those zeros and save, replacing a
// multi-million-token lifetime reading with a single event over a transient
// EACCES or EMFILE. The odometer returned alongside the error is still
// usable, so a display caller that ignores the error shows zero rather than
// crashing, but it must never be written back.
func LoadOdometer(path string) (*Odometer, error) {
	o, state, err := loadOdometer(path)
	if state == odoUnreadable {
		return o, err
	}
	return o, nil
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
	unlock, err := lockfile.Acquire(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	// Re-read under the lock so a concurrent writer's increments are not lost,
	// then merge our in-memory state up (never down).
	onDisk, state, loadErr := loadOdometer(path)
	switch state {
	case odoUnreadable:
		// The file is there and holds a reading; we just cannot see it.
		// Saving now would merge up against zeros and overwrite it, so this
		// turn's usage goes unrecorded instead. The error is returned for a
		// caller that wants to report it, and recording is best-effort, so
		// the turn itself is unaffected.
		return fmt.Errorf("freellm: odometer not updated, its file could not be read: %w", loadErr)
	case odoCorrupt:
		// Nothing here can parse those bytes, but they are the only copy of
		// whatever reading they held, so set them aside before starting over
		// rather than saving on top of them. If even that fails, leave the
		// file alone and record nothing.
		if err := os.Rename(path, path+corruptSuffix); err != nil {
			return fmt.Errorf("freellm: odometer not updated, its damaged file could not be set aside: %w", err)
		}
	}
	o.mergeUp(onDisk)
	o.addLocked(e)
	return o.save(path)
}

// mergeProviderTotals raises every field of dst to at least the corresponding
// field of src, keyed the same way for Per and PerModel. It never lowers
// anything, which is what keeps a merge monotonic. dst is allocated if nil.
func mergeProviderTotals(dst, src map[string]ProviderTotal) map[string]ProviderTotal {
	if dst == nil {
		dst = map[string]ProviderTotal{}
	}
	for k, v := range src {
		cur := dst[k]
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
		dst[k] = cur
	}
	return dst
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
	o.Per = mergeProviderTotals(o.Per, other.Per)
	o.PerModel = mergeProviderTotals(o.PerModel, other.PerModel)
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

	if e.Model != "" {
		if o.PerModel == nil {
			o.PerModel = map[string]ProviderTotal{}
		}
		key := ModelKey(e.Profile, e.Model)
		mt := o.PerModel[key]
		mt.Tokens += e.Tokens
		mt.Requests += e.Requests
		if mt.FirstSeen.IsZero() {
			mt.FirstSeen = e.TS
		}
		if e.TS.After(mt.LastSeen) {
			mt.LastSeen = e.TS
		}
		o.PerModel[key] = mt
	}

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
	return lockfile.WriteAtomic(path, b, 0o600)
}

// Record adds one turn's usage to the odometer at the default path.
//
// It exists so every surface that runs a turn meters it the same way. The
// CLI had the only call site in the tree, inside the one-shot run/ask
// command, which meant a turn served over HTTP or through the desktop app
// counted for nothing: the model picker read the odometer for its remaining
// allowances, saw zero for every model forever, and never cycled a model
// away at capacity because nothing ever approached capacity.
//
// profile empty means the turn did not run on a free provider and nothing
// is recorded. model must be the model that actually served the turn, not
// the configured one: speed routing, startup discovery and the runtime
// /model command all reassign it, so the configured value can name a
// different model entirely and attributing usage to it meters the wrong
// allowance.
//
// Recording is best effort and never fails a turn: a user's work does not
// stop because a usage counter could not be written. The error is returned
// for callers that want to log it.
func Record(profile, model string, promptTokens, completionTokens int) error {
	if profile == "" {
		return nil
	}
	path := OdometerPath()
	o, err := LoadOdometer(path)
	if err != nil {
		// An unreadable odometer is not a reason to write a zeroed one over
		// it; see LoadOdometer for why that matters.
		return err
	}
	return o.Add(path, Event{
		TS:       time.Now(),
		Profile:  profile,
		Model:    model,
		Tokens:   int64(promptTokens + completionTokens),
		Requests: 1,
	})
}
