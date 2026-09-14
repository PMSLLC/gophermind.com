package freellm

import (
	"testing"
	"time"
)

func TestParseRateLimit(t *testing.T) {
	cases := []struct {
		in   string
		want []Quota
	}{
		{"30 RPM, 1,000 RPD", []Quota{
			{UnitRequests, 30, time.Minute},
			{UnitRequests, 1000, 24 * time.Hour},
		}},
		{"15 RPM, 20K TPD", []Quota{
			{UnitRequests, 15, time.Minute},
			{UnitTokens, 20000, 24 * time.Hour},
		}},
		{"1,000 RPM, 50,000 TPM", []Quota{
			{UnitRequests, 1000, time.Minute},
			{UnitTokens, 50000, time.Minute},
		}},
		{"200 req/hr", []Quota{{UnitRequests, 200, time.Hour}}},
		{"10 RPM, 60 req/hr (anonymous)", []Quota{
			{UnitRequests, 10, time.Minute},
			{UnitRequests, 60, time.Hour},
		}},
		{"2 RPM (anonymous)", []Quota{{UnitRequests, 2, time.Minute}}},
		{"~1 RPS, 500K TPM", []Quota{{UnitTokens, 500000, time.Minute}}},
		// Unparseable: no guessing.
		{"10K neurons/day (shared)", nil},
		{"Credit-metered", nil},
		{"Session/weekly limits (unpublished)", nil},
		{"Dynamic quotas + dynamic concurrency", nil},
		{"1 concurrent request", nil},
		{"—", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := ParseRateLimit(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseRateLimit(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseRateLimit(%q)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestEveryUpstreamRateLimitIsAccountedFor fails when a sync introduces a
// rate-limit format nobody has looked at, rather than silently dropping it.
// The unparseable map is a claim checked in both directions: a string in the
// map must parse to nothing, and a string not in the map must parse to
// something. An accidental parse of a string we decided was unparseable is
// exactly as wrong as a string nobody decided about at all.
func TestEveryUpstreamRateLimitIsAccountedFor(t *testing.T) {
	// Formats we have deliberately decided not to parse.
	unparseable := map[string]bool{
		"": true, "—": true,
		"10K neurons/day (shared)":                   true,
		"Credit-metered":                             true,
		"Session/weekly limits (unpublished)":        true,
		"Dynamic quotas + dynamic concurrency":       true,
		"1 concurrent request":                       true,
		"2,000 RPD total; <=500 RPD/model (dynamic)": true,
	}
	seen := map[string]bool{}
	for _, p := range Load().All() {
		for _, m := range p.Models {
			if seen[m.RateLimit] {
				continue
			}
			seen[m.RateLimit] = true
			got := ParseRateLimit(m.RateLimit)
			switch {
			case unparseable[m.RateLimit] && len(got) > 0:
				t.Errorf("provider %q model %q rateLimit %q is marked unparseable but ParseRateLimit returned %v: the regex is guessing where we decided it should not",
					p.Name, m.ID, m.RateLimit, got)
			case !unparseable[m.RateLimit] && len(got) == 0:
				t.Errorf("provider %q model %q has unrecognized rateLimit %q: parse it in quota.go or add it to the unparseable list",
					p.Name, m.ID, m.RateLimit)
			}
		}
	}
}

func TestQuotaString(t *testing.T) {
	if got := (Quota{UnitRequests, 1000, 24 * time.Hour}).String(); got != "1,000 RPD" {
		t.Errorf("got %q, want %q", got, "1,000 RPD")
	}
	if got := (Quota{UnitTokens, 20000, 24 * time.Hour}).String(); got != "20,000 TPD" {
		t.Errorf("got %q, want %q", got, "20,000 TPD")
	}
}

func TestQuotasForUsesDefaultModel(t *testing.T) {
	c, ok := CompatFor("free-groq")
	if !ok {
		t.Fatal("free-groq missing")
	}
	if len(QuotasFor(c)) == 0 {
		t.Error("expected Groq's default model to yield at least one quota")
	}
}

func TestCommas(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234567, "-1,234,567"},
	}
	for _, tc := range cases {
		if got := Commas(tc.in); got != tc.want {
			t.Errorf("Commas(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A model's allowance is the one its own provider publishes for it, not the
// one published for whichever model happens to be the profile's default.
//
// Google Gemini is the case that proves it: gemini-2.5-pro publishes
// "5 RPM, 50 RPD" while the profile default publishes "15 RPM, 1,500 RPD".
// Metering the pro model against the default's numbers overstates its
// allowance threefold on requests per minute and thirtyfold per day, so the
// picker keeps choosing a model that is already being throttled and only
// finds out when the provider starts refusing.
func TestQuotasForModelUsesThatModelsOwnLimit(t *testing.T) {
	c, ok := CompatFor("free-gemini")
	if !ok {
		t.Skip("free-gemini missing from the registry")
	}
	got := QuotasForModel(c, "gemini-2.5-pro")
	if len(got) == 0 {
		t.Fatal("gemini-2.5-pro publishes a rate limit; got no quotas")
	}
	for _, q := range got {
		if q.Unit == UnitRequests && q.Window == time.Minute && q.Amount != 5 {
			t.Errorf("gemini-2.5-pro RPM: got %d, want 5 (its own published limit, "+
				"not the profile default's)", q.Amount)
		}
		if q.Unit == UnitRequests && q.Window == WindowDay && q.Amount != 50 {
			t.Errorf("gemini-2.5-pro RPD: got %d, want 50", q.Amount)
		}
	}
}

// A model that publishes no limit gets no denominator. Borrowing the
// default model's numbers would invent an allowance the provider never
// promised, which is the one thing the usage display must never do: a
// fabricated ceiling reads exactly like a real one.
func TestQuotasForModelInventsNothingWhenNoLimitIsPublished(t *testing.T) {
	c, ok := CompatFor("free-gemini")
	if !ok {
		t.Skip("free-gemini missing from the registry")
	}
	// gemma-4-31b-it's rate_limit in the vendored registry is "-", i.e. none.
	if got := QuotasForModel(c, "gemma-4-31b-it"); len(got) != 0 {
		t.Errorf("gemma-4-31b-it publishes no rate limit; got %v, want none", got)
	}
}

// An empty model keeps the old profile-level meaning, so TripMeters (which
// asks about a whole profile rather than one model) is unchanged.
func TestQuotasForModelEmptyModelFallsBackToDefault(t *testing.T) {
	c, ok := CompatFor("free-groq")
	if !ok {
		t.Fatal("free-groq missing")
	}
	if len(QuotasForModel(c, "")) == 0 {
		t.Error("empty model should fall back to the profile's default model")
	}
}
