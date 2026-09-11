package freellm

import (
	"fmt"
	"time"
)

// warnFraction is the point at which a trip meter starts warning. gophermind
// never blocks a request on this: the local count can drift from the
// provider's when another client shares the key, and refusing a request that
// would have succeeded is worse than a 429.
const warnFraction = 0.8

// TripMeter is consumption inside one quota window. HasQuota is false when
// upstream's limit did not parse, in which case Used is still meaningful but
// there is no denominator to show.
type TripMeter struct {
	Quota    Quota
	Used     int64
	HasQuota bool
}

// Fraction is consumption as a share of the quota, or 0 when there is none.
func (t TripMeter) Fraction() float64 {
	if !t.HasQuota || t.Quota.Amount <= 0 {
		return 0
	}
	return float64(t.Used) / float64(t.Quota.Amount)
}

// Warn reports whether this meter has reached the warning threshold.
func (t TripMeter) Warn() bool { return t.HasQuota && t.Fraction() >= warnFraction }

// String renders "312/1,000 RPD" with a quota, or a bare "42 requests in 24h
// (no published limit)" without one. It never invents a denominator.
func (t TripMeter) String() string {
	if t.HasQuota {
		return fmt.Sprintf("%s/%s", Commas(t.Used), t.Quota.String())
	}
	return fmt.Sprintf("%s requests in 24h (no published limit)", Commas(t.Used))
}

// TripMeters returns one meter per published quota for the profile, counting
// only that profile's events inside each window.
//
// When no quota parses, it returns a single quota-less meter carrying the
// day's request count, so the user still sees activity.
func TripMeters(o *Odometer, c Compat, now time.Time) []TripMeter {
	return tripMeters(o, c, "", now)
}

// ModelTripMeters returns one meter per published quota for the profile,
// counting only that profile's events for the given model inside each
// window. It mirrors TripMeters exactly, so the quota-less fallback and the
// warn threshold cannot drift between the two.
func ModelTripMeters(o *Odometer, c Compat, model string, now time.Time) []TripMeter {
	return tripMeters(o, c, model, now)
}

// tripMeters is the shared body for TripMeters and ModelTripMeters. model
// empty means "any model", which is what TripMeters wants.
func tripMeters(o *Odometer, c Compat, model string, now time.Time) []TripMeter {
	quotas := QuotasFor(c)
	if len(quotas) == 0 {
		return []TripMeter{{Used: sumWindow(o, c.Profile, model, UnitRequests, now, 24*time.Hour)}}
	}
	out := make([]TripMeter, 0, len(quotas))
	for _, q := range quotas {
		out = append(out, TripMeter{
			Quota:    q,
			Used:     sumWindow(o, c.Profile, model, q.Unit, now, q.Window),
			HasQuota: true,
		})
	}
	return out
}

// sumWindow totals one profile's usage of one unit within the window ending
// at now. model, when non-empty, further restricts the total to events for
// that model; empty means any model.
func sumWindow(o *Odometer, profile, model string, unit Unit, now time.Time, window time.Duration) int64 {
	if o == nil {
		return 0
	}
	cutoff := now.Add(-window)
	var total int64
	for _, e := range o.Events {
		if e.Profile != profile || !e.TS.After(cutoff) || e.TS.After(now) {
			continue
		}
		if model != "" && e.Model != model {
			continue
		}
		if unit == UnitTokens {
			total += e.Tokens
		} else {
			total += e.Requests
		}
	}
	return total
}
