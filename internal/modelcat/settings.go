// Package modelcat assembles the model picker's catalogue and settings: one
// list of every model gophermind can reach, with reachability, remaining
// allowance and links, plus the user's preference order, thresholds,
// exclusions and custom links for it.
package modelcat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/freellm"
)

// SettingsEnv, when set, overrides where the model settings file lives, so
// tests and alternate installs can redirect it.
const SettingsEnv = "GOPHERMIND_MODEL_SETTINGS"

// settingsFileName is the file the settings live in, beside the odometer.
const settingsFileName = "model-settings.json"

// defaultCapacityPercent is what CapacityPercent means when it is 0 (the
// zero value, as an old or freshly-defaulted file would carry). It is also
// DefaultSettings' own value, so a fresh install and an old file agree.
const defaultCapacityPercent = 90

// Settings is the user's model picker preferences: order, capacity
// threshold, term exclusions, filter defaults and custom links. It persists
// as one JSON object beside the odometer.
type Settings struct {
	// Order lists "profile/model" keys, most preferred first.
	Order []string `json:"order,omitempty"`
	// CycleOnCapacity switches to the next model in Order when the current
	// one nears its quota.
	CycleOnCapacity bool `json:"cycle_on_capacity"`
	// CapacityPercent is the percent of a quota that counts as near
	// capacity. 0 means the default, 90.
	CapacityPercent int `json:"capacity_percent"`
	// WhenAllFull is what to do when every model in Order is near capacity:
	// "stay" (default) or "ask".
	WhenAllFull string `json:"when_all_full,omitempty"`
	// FilterReachable hides unreachable entries from the picker.
	FilterReachable bool `json:"filter_reachable"`
	// FilterHasCapacity hides entries with no remaining capacity.
	FilterHasCapacity bool `json:"filter_has_capacity"`
	// ExcludedTerms lists free-tier terms flags (see freellm.TermsFlags)
	// whose models should be hidden, e.g. "non-commercial", "trains on
	// prompts", "identity check".
	ExcludedTerms []string `json:"excluded_terms,omitempty"`
	// CustomLinks maps a "profile" or "profile/model" key to a URL that
	// overrides the derived one.
	CustomLinks map[string]string `json:"custom_links,omitempty"`
}

// DefaultSettings returns the settings a fresh install starts with: capacity
// warnings at 90%, staying put when every model is full, and the picker
// filtered to reachable entries so it opens short.
func DefaultSettings() Settings {
	return Settings{
		CapacityPercent: defaultCapacityPercent,
		WhenAllFull:     "stay",
		FilterReachable: true,
	}
}

// CapacityThreshold returns CapacityPercent as a fraction, defaulting to 0.9
// when CapacityPercent is 0 (the zero value, as an old file would load).
func (s Settings) CapacityThreshold() float64 {
	p := s.CapacityPercent
	if p == 0 {
		p = defaultCapacityPercent
	}
	return float64(p) / 100
}

// SettingsPath resolves where the model settings file lives, honoring
// SettingsEnv (GOPHERMIND_MODEL_SETTINGS), else beside the odometer.
func SettingsPath() string {
	if p := strings.TrimSpace(os.Getenv(SettingsEnv)); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(freellm.OdometerPath()), settingsFileName)
}

// restrictiveSettings returns DefaultSettings with every free-tier terms
// flag excluded. It is the fail-closed value for the cases where the user's
// real settings could not be read.
//
// ExcludedTerms is a legal constraint, not a preference: a provider whose
// terms the user excluded must never be auto-selected. So when the list is
// unknown, the safe direction is to exclude every term it could have named
// rather than none of them. The wrong guess then costs the user a shorter
// picker until the file is fixed, instead of silently re-enabling exactly
// what they excluded.
//
// A new flag added to freellm.Terms must be set here too.
func restrictiveSettings() Settings {
	s := DefaultSettings()
	s.ExcludedTerms = freellm.TermsFlags(freellm.Terms{
		NonCommercial:   true,
		TrainsOnPrompts: true,
		IdentityCheck:   true,
	})
	return s
}

// LoadSettings reads the settings at path.
//
// A file that does not exist yet is the normal first-run case: it yields
// DefaultSettings with a nil error, so a fresh install starts with no
// exclusions because the user has genuinely set none.
//
// Any other read failure - permission denied, an IO error, the path being a
// directory - is a real failure and is returned as one. It must not read
// like a first run: doing so discards the user's ExcludedTerms, which exist
// for legal reasons, along with every other preference, on nothing worse
// than a transient error.
//
// A file that is present but unparseable cannot yield the user's real
// preferences either, but reporting it would break the standing promise
// that a damaged preference file never blocks a turn. It therefore yields
// restrictiveSettings with a nil error: defaults, with every terms flag
// excluded rather than none. The same value accompanies the error on a read
// failure, so a caller that ignores the error still fails closed.
func LoadSettings(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSettings(), nil
		}
		return restrictiveSettings(), fmt.Errorf("modelcat: read settings %s: %w", path, err)
	}
	// An empty file is a fresh start, not corruption: that is what a
	// half-finished create or an interrupted write leaves behind, and there
	// is nothing in it to have lost.
	if len(bytes.TrimSpace(b)) == 0 {
		return DefaultSettings(), nil
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		// Unparseable content IS a failure and is reported as one. Returning
		// a nil error here would tell the caller these are the user's
		// settings when they are a stand-in, and the stand-in's empty
		// ExcludedTerms would re-enable providers the user excluded for
		// legal reasons. The settings returned alongside the error fail
		// closed for any caller that cannot propagate it.
		return restrictiveSettings(), fmt.Errorf("modelcat: parse settings %s: %w", path, err)
	}
	return s, nil
}

// SaveSettings writes s to path atomically: a temp file in the same
// directory, permissioned 0600, then a rename, so a crash mid-write cannot
// leave a half-written settings file.
func SaveSettings(path string, s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("modelcat: marshal settings: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("modelcat: create settings dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".model-settings-*")
	if err != nil {
		return fmt.Errorf("modelcat: create temp settings file: %w", err)
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

// ValidateLink checks a URL before it is stored as a custom link. This is a
// security boundary, not formatting: these URLs are rendered as clickable
// links inside a WebView, where a javascript: URL executes in the app's own
// origin and a file: URL reaches local disk. Only http and https are
// accepted, and the URL must have a non-empty host: a bare string like
// "not a url at all" parses without error in Go's net/url, so checking the
// scheme alone is not enough.
func ValidateLink(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("modelcat: invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("modelcat: URL scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("modelcat: URL has no host")
	}
	return nil
}
