package freellm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Unit is what a quota counts.
type Unit int

const (
	// UnitRequests counts API calls.
	UnitRequests Unit = iota
	// UnitTokens counts prompt plus completion tokens.
	UnitTokens
)

// Quota is one published free-tier limit: an amount of some unit per window.
type Quota struct {
	Unit   Unit
	Amount int64
	Window time.Duration
}

// String renders a quota the way upstream writes it, e.g. "1,000 RPD".
func (q Quota) String() string {
	u := "R"
	if q.Unit == UnitTokens {
		u = "T"
	}
	var w string
	switch q.Window {
	case time.Minute:
		w = "PM"
	case time.Hour:
		w = "P/hr"
	case 24 * time.Hour:
		w = "PD"
	case 30 * 24 * time.Hour:
		w = "PMo"
	default:
		w = "P?"
	}
	return fmt.Sprintf("%s %s%s", Commas(q.Amount), u, w)
}

// WindowDay is a one day quota window.
const WindowDay = 24 * time.Hour

// quotaRe matches the unambiguous forms: "30 RPM", "1,000 RPD", "20K TPD",
// "50,000 TPM", "200 req/hr". Deliberately narrow: anything it does not match
// yields no quota rather than a guess.
var quotaRe = regexp.MustCompile(`(?i)([0-9][0-9,]*)\s*(K?)\s*(RPM|RPD|TPM|TPD|req/hr)`)

// ParseRateLimit extracts every quota it can recognize from an upstream
// rate-limit string, in the order they appear. An unrecognized string yields
// nil: a trip meter with no denominator is honest, a guessed one is not.
//
// "RPS" is deliberately not parsed. Upstream writes it as "~1 RPS", an
// approximation, and a per-second window is not a useful trip meter.
//
// A string containing "dynamic" is skipped even when it also contains a
// number and a recognized unit, e.g. "2,000 RPD total; <=500 RPD/model
// (dynamic)": upstream is telling us the figure is not a fixed cap, and
// extracting a Quota from it anyway would present a moving target as a flat
// one.
func ParseRateLimit(s string) []Quota {
	if strings.Contains(strings.ToLower(s), "dynamic") {
		return nil
	}
	var out []Quota
	for _, m := range quotaRe.FindAllStringSubmatch(s, -1) {
		n, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if strings.EqualFold(m[2], "K") {
			n *= 1000
		}
		q := Quota{Amount: n}
		switch strings.ToLower(m[3]) {
		case "rpm":
			q.Unit, q.Window = UnitRequests, time.Minute
		case "rpd":
			q.Unit, q.Window = UnitRequests, WindowDay
		case "tpm":
			q.Unit, q.Window = UnitTokens, time.Minute
		case "tpd":
			q.Unit, q.Window = UnitTokens, WindowDay
		case "req/hr":
			q.Unit, q.Window = UnitRequests, time.Hour
		default:
			continue
		}
		out = append(out, q)
	}
	return out
}

// QuotasFor returns the quotas that apply to a profile's default model.
// Returns nil for an unsupported profile or an unparseable limit.
//
// Use QuotasForModel when metering a specific model: models on one provider
// routinely publish different limits, and this function answers only the
// question about the default.
func QuotasFor(c Compat) []Quota {
	return QuotasForModel(c, "")
}

// QuotasForModel returns the quotas a provider publishes for one of its
// models. An empty model means the profile's default model, which is what
// whole-profile metering asks about.
//
// The limit comes from the named model's own registry entry, never from
// another model's. Providers publish genuinely different allowances per
// model - Google Gemini alone spans "5 RPM, 50 RPD", "15 RPM, 1,500 RPD",
// "30 RPM, 1,500 RPD" and nothing at all - so borrowing the default's
// numbers both overstates the constrained models and invents a ceiling for
// the models that publish none. A fabricated denominator is worse than no
// denominator: it reads exactly like a real one, and usage measured against
// it looks safe right up to the point the provider starts refusing.
//
// Returns nil when the model publishes no parseable limit. Callers must
// render that as "no published quota", not as a quota of zero.
func QuotasForModel(c Compat, model string) []Quota {
	if !c.Supported {
		return nil
	}
	p, ok := Load().Lookup(c.Upstream)
	if !ok {
		return nil
	}
	if model == "" {
		model = c.DefaultModel
	}
	for _, m := range p.Models {
		if m.ID == model {
			return ParseRateLimit(m.RateLimit)
		}
	}
	return nil
}

// Commas formats an integer with thousands separators, e.g. 1234567 becomes
// "1,234,567". A later task in another package needs this exact formatting,
// so it is exported here rather than duplicated there.
func Commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
