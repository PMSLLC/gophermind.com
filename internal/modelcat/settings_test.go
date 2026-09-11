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
func TestCorruptSettingsYieldDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	for _, junk := range []string{"", "{", "not json"} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadSettings(path)
		if err != nil {
			t.Errorf("LoadSettings(%q) errored: %v", junk, err)
		}
		if s.CapacityPercent != 90 {
			t.Errorf("corrupt file did not yield defaults: %+v", s)
		}
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
