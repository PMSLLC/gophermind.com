package modelcat

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gophermind/internal/freellm"
)

// Entry is one model gophermind can address: its provider, whether it can be
// reached right now, remaining allowance and links.
type Entry struct {
	// ID is the model identifier as the provider names it.
	ID string
	// Provider is the upstream provider's display name.
	Provider string
	// Profile is the gophermind profile name, e.g. "free-groq". Empty for a
	// model served by the user's own configured endpoint.
	Profile string
	// Reachable reports whether this entry can be used right now.
	Reachable bool
	// Reason explains why Reachable is false. Empty when Reachable is true
	// or nothing more specific is known.
	Reason string
	// Used is consumption in the current window for the entry's most
	// constraining quota, or a bare request count when no quota is
	// published.
	Used int64
	// Quota is the amount of the most constraining published quota. 0 means
	// no quota is published; such an entry never has NearCapacity set.
	Quota int64
	// Unit is what Quota counts ("requests" or "tokens"), empty when Quota
	// is 0.
	Unit string
	// Window is the human label for Quota's period ("minute", "hour",
	// "day", "month"), empty when Quota is 0.
	Window string
	// Context is the model's context window, as the provider describes it.
	Context string
	// Modality is the model's supported modality, as the provider
	// describes it.
	Modality string
	// Terms lists the free-tier obligations worth warning about, from
	// freellm.TermsFlags.
	Terms []string
	// ProviderURL is a human-facing link to the provider, or empty.
	ProviderURL string
	// ModelURL is a human-facing link to the model, or empty.
	ModelURL string
	// NearCapacity reports whether Used has crossed the settings' capacity
	// threshold of Quota. Always false when Quota is 0: there is no line to
	// cross.
	NearCapacity bool
}

// Build assembles the catalogue: one entry per model in the vendored
// registry, joined to gophermind's compatibility table for reachability and
// links, and to the odometer for remaining allowance. endpointModels are the
// ids the active configured endpoint serves (nil when it could not be
// reached); they appear with an empty Profile and no quota, since they have
// no free-tier provider behind them.
func Build(o *freellm.Odometer, s Settings, endpointModels []string, now time.Time) []Entry {
	reg := freellm.Load()
	var out []Entry
	for _, c := range freellm.Compats() {
		provider, ok := reg.Lookup(c.Upstream)
		if !ok {
			continue
		}
		reachable, reason := Reachability(c)
		terms := freellm.TermsFlags(c.Terms)
		for _, m := range provider.Models {
			out = append(out, buildEntry(o, s, c, provider.Name, m, reachable, reason, terms, now))
		}
	}
	for _, id := range endpointModels {
		out = append(out, Entry{
			ID:        id,
			Reachable: true,
			ModelURL:  DeriveModelURL(id),
		})
	}
	return out
}

// buildEntry constructs one registry model's catalogue entry.
func buildEntry(o *freellm.Odometer, s Settings, c freellm.Compat, providerName string, m freellm.Model, reachable bool, reason string, terms []string, now time.Time) Entry {
	e := Entry{
		ID:        m.ID,
		Provider:  providerName,
		Profile:   c.Profile,
		Reachable: reachable,
		Reason:    reason,
		Context:   m.Context,
		Modality:  m.Modality,
		Terms:     terms,
	}
	meter := pickMeter(freellm.ModelTripMeters(o, c, m.ID, now))
	e.Used = meter.Used
	if meter.HasQuota {
		e.Quota = meter.Quota.Amount
		e.Unit = unitLabel(meter.Quota.Unit)
		e.Window = windowLabel(meter.Quota.Window)
		e.NearCapacity = meter.Fraction() >= s.CapacityThreshold()
	}
	e.ProviderURL, e.ModelURL = resolveLinks(s, c, m.ID)
	return e
}

// pickMeter returns the meter that best represents an entry's remaining
// allowance: the published quota closest to being exhausted, or the sole
// fallback meter when no quota is published. freellm.ModelTripMeters never
// returns an empty slice.
func pickMeter(meters []freellm.TripMeter) freellm.TripMeter {
	best := meters[0]
	bestFraction := -1.0
	for _, m := range meters {
		if !m.HasQuota {
			continue
		}
		if f := m.Fraction(); f > bestFraction {
			bestFraction = f
			best = m
		}
	}
	return best
}

// unitLabel renders a freellm.Unit as the Entry.Unit string.
func unitLabel(u freellm.Unit) string {
	if u == freellm.UnitTokens {
		return "tokens"
	}
	return "requests"
}

// windowLabel renders a quota window as the Entry.Window string.
func windowLabel(d time.Duration) string {
	switch d {
	case time.Minute:
		return "minute"
	case time.Hour:
		return "hour"
	case freellm.WindowDay:
		return "day"
	case 30 * 24 * time.Hour:
		return "month"
	default:
		return d.String()
	}
}

// resolveLinks applies the link resolution order: a custom link for the
// exact "profile/model" key, then a custom link for the "profile" key, then
// a derived link, then empty. ProviderURL prefers the profile-level custom
// link, falling back to the compat entry's Website.
func resolveLinks(s Settings, c freellm.Compat, modelID string) (providerURL, modelURL string) {
	modelKey := c.Profile + "/" + modelID
	switch {
	case s.CustomLinks[modelKey] != "":
		modelURL = s.CustomLinks[modelKey]
	case s.CustomLinks[c.Profile] != "":
		modelURL = s.CustomLinks[c.Profile]
	default:
		modelURL = DeriveModelURL(modelID)
	}
	if s.CustomLinks[c.Profile] != "" {
		providerURL = s.CustomLinks[c.Profile]
	} else {
		providerURL = c.Website
	}
	return providerURL, modelURL
}

// Reachability reports whether a compat entry can be used right now, and
// why not when it cannot. This is computed, never guessed: an unsupported
// entry is never reachable and carries its Note as the reason; a no-key
// entry is always reachable; a keyed entry is reachable only when its
// GOPHERMIND_PROFILE_<NAME>_API_KEY environment variable is actually set,
// and the reason names that exact variable so the caller knows what to set.
func Reachability(c freellm.Compat) (bool, string) {
	if !c.Supported {
		return false, c.Note
	}
	if c.NoKey {
		return true, ""
	}
	env := apiKeyEnv(c.Profile)
	if strings.TrimSpace(os.Getenv(env)) != "" {
		return true, ""
	}
	return false, fmt.Sprintf("set %s to use this provider", env)
}

// apiKeyEnv derives the API key environment variable for a profile, e.g.
// "free-groq" => "GOPHERMIND_PROFILE_FREE_GROQ_API_KEY". It mirrors
// internal/config's own profile-to-env mapping, which is unexported there.
func apiKeyEnv(profile string) string {
	up := strings.ToUpper(profile)
	up = strings.ReplaceAll(up, "-", "_")
	return "GOPHERMIND_PROFILE_" + up + "_API_KEY"
}

// DeriveModelURL returns a Hugging Face URL for an "owner/name" model id:
// exactly one slash, both parts non-empty, and no spaces. Anything else
// returns "" rather than a guess that would 404.
func DeriveModelURL(id string) string {
	if id == "" || strings.Contains(id, " ") {
		return ""
	}
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return "https://huggingface.co/" + id
}
