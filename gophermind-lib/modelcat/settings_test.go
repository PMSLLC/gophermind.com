package modelcat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAreTheDocumentedOnes(t *testing.T) {
	d := DefaultSettings()
	if d.CapacityPercent != 90 {
		t.Errorf("CapacityPercent = %d, want 90", d.CapacityPercent)
	}
	if d.WhenAllFull != "stay" {
		t.Errorf("WhenAllFull = %q, want \"stay\"", d.WhenAllFull)
	}
	if !d.FilterReachable {
		t.Error("FilterReachable should default on, so the dropdown opens short")
	}
	if d.CycleOnCapacity {
		t.Error("CycleOnCapacity must default off")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	in := DefaultSettings()
	in.Order = []string{"free-groq/openai/gpt-oss-120b", "free-ovhcloud/gpt-oss-120b"}
	in.ExcludedTerms = []string{"non-commercial"}
	in.CustomLinks = map[string]string{"free-groq": "https://groq.com/docs"}
	if err := SaveSettings(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Order) != 2 || out.Order[0] != in.Order[0] {
		t.Errorf("order did not round-trip: %v", out.Order)
	}
	if out.CustomLinks["free-groq"] != "https://groq.com/docs" {
		t.Errorf("custom link did not round-trip: %v", out.CustomLinks)
	}
}

// A damaged settings file must never block a turn.
// A settings file that exists but cannot be parsed is a failure and must be
// reported as one. Returning a nil error told every caller "these are the
// user's settings" when they were a stand-in, and a stand-in with empty
// ExcludedTerms re-enables providers the user excluded for legal reasons.
// This test previously asserted the opposite; it was pinning the fail-open.
//
// An EMPTY file is not corruption. A half-finished create or an interrupted
// write leaves one behind and there is nothing in it to have lost, so that
// stays the fresh-start case.
func TestCorruptSettingsAreReportedNotSwallowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	for _, junk := range []string{"{", "not json"} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadSettings(path)
		if err == nil {
			t.Errorf("LoadSettings(%q) returned no error; a caller cannot tell "+
				"a stand-in from the user's real configuration", junk)
		}
		// Whatever a caller that ignores the error does next, it must not be
		// allowed to select a provider the user excluded.
		if len(s.ExcludedTerms) == 0 {
			t.Errorf("LoadSettings(%q) failed open: ExcludedTerms is empty, so "+
				"an excluded provider becomes selectable again", junk)
		}
	}
}

func TestEmptySettingsFileIsAFreshStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("empty file should read as a fresh start: %v", err)
	}
	if s.CapacityPercent != 90 {
		t.Errorf("empty file did not yield defaults: %+v", s)
	}
}

func TestMissingSettingsYieldDefaults(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.CapacityPercent != 90 {
		t.Errorf("missing file did not yield defaults: %+v", s)
	}
}

// This is a security test, not a formatting one: these URLs are rendered as
// clickable links inside a WebView, where javascript: would execute in the
// app's own origin and file: would reach local disk.
func TestValidateLinkRejectsDangerousSchemes(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"vbscript:msgbox",
		"not a url at all",
		"",
	} {
		if err := ValidateLink(bad); err == nil {
			t.Errorf("ValidateLink(%q) accepted a dangerous or malformed URL", bad)
		}
	}
	for _, good := range []string{"http://example.test", "https://groq.com/docs"} {
		if err := ValidateLink(good); err != nil {
			t.Errorf("ValidateLink(%q) rejected a valid URL: %v", good, err)
		}
	}
}

func TestCapacityThresholdDefaults(t *testing.T) {
	var s Settings // zero value, as an old file would load
	if got := s.CapacityThreshold(); got != 0.9 {
		t.Errorf("zero CapacityPercent gave %v, want 0.9", got)
	}
	s.CapacityPercent = 50
	if got := s.CapacityThreshold(); got != 0.5 {
		t.Errorf("CapacityPercent 50 gave %v, want 0.5", got)
	}
}

// TestLoadSettingsReportsARealReadFailure is the fail-closed test for
// ExcludedTerms. That list exists for legal reasons, so a caller must never
// be able to mistake "your exclusions could not be read" for "you have no
// exclusions". A file that is present but unreadable is a real failure, not
// the first-run case, and must be reported as one.
func TestLoadSettingsReportsARealReadFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not deny reads")
	}
	path := filepath.Join(t.TempDir(), "s.json")
	in := DefaultSettings()
	in.ExcludedTerms = []string{"non-commercial"}
	if err := SaveSettings(path, in); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	s, err := LoadSettings(path)
	if err == nil {
		t.Fatal("LoadSettings on an unreadable file returned no error, so a caller cannot tell the exclusions were lost")
	}
	if len(s.ExcludedTerms) == 0 {
		t.Error("a failed read yielded empty ExcludedTerms, which silently re-enables what the user excluded")
	}
}

// TestCorruptSettingsExcludeEverythingRatherThanNothing covers the other
// half: a damaged file must still never block a turn, so it yields defaults
// with a nil error, but its ExcludedTerms must be maximally restrictive
// rather than empty.
func TestCorruptSettingsExcludeEverythingRatherThanNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The error is now reported (see TestCorruptSettingsAreReportedNotSwallowed);
	// what this test pins is the settings returned ALONGSIDE it, because two
	// call sites cannot fail a turn and use them anyway.
	s, _ := LoadSettings(path)
	for _, want := range []string{"non-commercial", "trains on prompts", "identity check"} {
		found := false
		for _, got := range s.ExcludedTerms {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("corrupt settings did not exclude %q: %v", want, s.ExcludedTerms)
		}
	}
}

// TestMissingSettingsAreNotTreatedAsAFailure pins the one case that must
// keep yielding plain defaults: no file yet is a normal first run.
func TestMissingSettingsAreNotTreatedAsAFailure(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a first run must not error: %v", err)
	}
	if len(s.ExcludedTerms) != 0 {
		t.Errorf("a first run must start with no exclusions, got %v", s.ExcludedTerms)
	}
}
