package freellm

import (
	"strings"
	"testing"
	"time"
)

func newOdo(events ...Event) *Odometer {
	o := &Odometer{Per: map[string]ProviderTotal{}}
	for _, e := range events {
		o.Events = append(o.Events, e)
	}
	return o
}

func TestTripMetersCountOnlyInsideWindow(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	c, ok := CompatFor("free-groq") // 30 RPM, 1,000 RPD
	if !ok {
		t.Fatal("free-groq missing")
	}
	o := newOdo(
		Event{TS: now.Add(-30 * time.Second), Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now.Add(-2 * time.Minute), Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now.Add(-48 * time.Hour), Profile: "free-groq", Tokens: 10, Requests: 1},
	)
	ms := TripMeters(o, c, now)
	if len(ms) == 0 {
		t.Fatal("expected trip meters for free-groq")
	}
	var perMinute, perDay *TripMeter
	for i := range ms {
		switch ms[i].Quota.Window {
		case time.Minute:
			perMinute = &ms[i]
		case 24 * time.Hour:
			perDay = &ms[i]
		}
	}
	if perMinute == nil || perMinute.Used != 1 {
		t.Errorf("per-minute meter used = %v, want 1", perMinute)
	}
	if perDay == nil || perDay.Used != 2 {
		t.Errorf("per-day meter used = %v, want 2 (the 48h-old event is outside)", perDay)
	}
}

func TestTripMetersIgnoreOtherProfiles(t *testing.T) {
	now := time.Now()
	c, _ := CompatFor("free-groq")
	o := newOdo(
		Event{TS: now, Profile: "free-groq", Tokens: 10, Requests: 1},
		Event{TS: now, Profile: "free-openrouter", Tokens: 10, Requests: 5},
	)
	for _, m := range TripMeters(o, c, now) {
		if m.Quota.Unit == UnitRequests && m.Used != 1 {
			t.Errorf("meter counted another profile's usage: used = %d, want 1", m.Used)
		}
	}
}

func TestTripMeterWarnsAtEightyPercent(t *testing.T) {
	m := TripMeter{Quota: Quota{UnitRequests, 100, 24 * time.Hour}, Used: 79, HasQuota: true}
	if m.Warn() {
		t.Error("warned at 79%")
	}
	m.Used = 80
	if !m.Warn() {
		t.Error("did not warn at 80%")
	}
}

func TestTripMeterStringWithAndWithoutQuota(t *testing.T) {
	with := TripMeter{Quota: Quota{UnitRequests, 1000, 24 * time.Hour}, Used: 312, HasQuota: true}
	if got := with.String(); got != "312/1,000 RPD" {
		t.Errorf("got %q, want %q", got, "312/1,000 RPD")
	}
	without := TripMeter{Used: 42}
	if got := without.String(); !strings.Contains(got, "42") || strings.Contains(got, "/") {
		t.Errorf("quota-less meter rendered %q; want a bare count with no denominator", got)
	}
}

// A provider whose limit does not parse must still report a count, never a
// guessed denominator.
func TestTripMetersForUnparseableQuota(t *testing.T) {
	now := time.Now()
	c, ok := CompatFor("free-ollama-cloud") // "Session/weekly limits (unpublished)"
	if !ok {
		t.Fatal("free-ollama-cloud missing")
	}
	o := newOdo(Event{TS: now, Profile: "free-ollama-cloud", Tokens: 500, Requests: 3})
	ms := TripMeters(o, c, now)
	if len(ms) != 1 {
		t.Fatalf("got %d meters, want 1 fallback meter", len(ms))
	}
	if ms[0].HasQuota {
		t.Error("fallback meter claims to have a quota")
	}
	if ms[0].Used != 3 {
		t.Errorf("fallback meter used = %d, want 3 requests", ms[0].Used)
	}
}
