package freellm

import (
	"testing"
	"time"
)

func TestModelTripMetersCountOnlyThatModel(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	c, ok := CompatFor("free-groq") // 30 RPM, 1,000 RPD
	if !ok {
		t.Fatal("free-groq missing")
	}
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "a", Tokens: 10, Requests: 1},
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "b", Tokens: 10, Requests: 1},
		{TS: now.Add(-time.Minute), Profile: "free-groq", Model: "a", Tokens: 10, Requests: 1},
	}}

	ms := ModelTripMeters(o, c, "a", now)
	if len(ms) == 0 {
		t.Fatal("expected trip meters for model a")
	}
	for _, m := range ms {
		if m.Quota.Unit == UnitRequests && m.Quota.Window == 24*time.Hour && m.Used != 2 {
			t.Errorf("model a per-day requests = %d, want 2 (model b must not count)", m.Used)
		}
	}
}

// An event with no model must not be attributed to a named model.
func TestModelTripMetersIgnoreModellessEvents(t *testing.T) {
	now := time.Now()
	c, _ := CompatFor("free-groq")
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now, Profile: "free-groq", Tokens: 99, Requests: 1},
	}}
	for _, m := range ModelTripMeters(o, c, "a", now) {
		if m.Used != 0 {
			t.Errorf("a model-less event counted toward model a: used = %d", m.Used)
		}
	}
}

// The denominator rule from the free-provider spec still holds per model.
func TestModelTripMetersNeverInventADenominator(t *testing.T) {
	now := time.Now()
	c, ok := CompatFor("free-ollama-cloud") // unpublished limits
	if !ok {
		t.Fatal("free-ollama-cloud missing")
	}
	o := &Odometer{Per: map[string]ProviderTotal{}, Events: []Event{
		{TS: now, Profile: "free-ollama-cloud", Model: "gpt-oss:120b", Tokens: 5, Requests: 1},
	}}
	ms := ModelTripMeters(o, c, "gpt-oss:120b", now)
	if len(ms) != 1 {
		t.Fatalf("got %d meters, want 1 fallback", len(ms))
	}
	if ms[0].HasQuota {
		t.Error("invented a quota for a provider that publishes none")
	}
}
