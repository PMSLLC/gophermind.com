package freellm

import (
	"fmt"
	"os"
	"strings"
)

// ReferralMarker is appended to every rendering of a referral link. The
// invariant it encodes: no code path emits an affiliate URL without saying so.
const ReferralMarker = "(referral link)"

// NoAffiliateEnv, when set to a non-empty value, forces the plain provider
// website even where a referral link is configured.
const NoAffiliateEnv = "GOPHERMIND_NO_AFFILIATE"

// Attribution names the provider serving the current model, and where to find
// them. The link and its referral status are unexported and set only by
// AttributionFor and AttributionFromCompat, which both apply the affiliate
// opt-out, so a value built outside this package cannot claim a plain website
// while carrying a referral URL: it can at worst construct a value with an
// empty link, which renders no URL at all.
type Attribution struct {
	// Model is the model name being served, e.g. "openai/gpt-oss-120b".
	Model string
	// Provider is the upstream provider name, e.g. "Groq".
	Provider string
	// Free reports whether this attribution is for a free tier.
	Free bool

	link       string // plain website, or the referral URL when isReferral
	isReferral bool   // set only when link is a referral URL
}

// Link is the URL to show for this provider. It is a referral link only when
// IsReferral reports true, and every rendering of a referral link carries the
// disclosure marker.
func (a Attribution) Link() string { return a.link }

// IsReferral reports whether Link is a referral URL that must be disclosed.
func (a Attribution) IsReferral() bool { return a.isReferral }

// AttributionFor returns attribution for a free profile. The second result is
// false for a paid or unknown profile, which has no free attribution to show.
func AttributionFor(profile, model string) (Attribution, bool) {
	c, ok := CompatFor(profile)
	if !ok {
		return Attribution{}, false
	}
	return AttributionFromCompat(c, model), true
}

// AttributionFromCompat builds attribution for an arbitrary Compat value,
// applying the affiliate opt-out exactly like AttributionFor. Exported so a
// caller that already has a Compat (including a synthetic one built in a
// test, since every shipped Affiliate is empty today) can build attribution
// without a registry lookup.
func AttributionFromCompat(c Compat, model string) Attribution {
	if model == "" {
		model = c.DefaultModel
	}
	a := Attribution{Model: model, Provider: c.Upstream, Free: true, link: c.Website}
	if c.Affiliate != "" && os.Getenv(NoAffiliateEnv) == "" {
		a.link = c.Affiliate
		a.isReferral = true
	}
	return a
}

// Line is the full one-line form, for the startup banner:
//
//	openai/gpt-oss-120b via Groq (free tier) https://groq.com
func (a Attribution) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s via %s", a.Model, a.Provider)
	if a.Free {
		b.WriteString(" (free tier)")
	}
	if a.link != "" {
		fmt.Fprintf(&b, " %s", a.link)
		if a.isReferral {
			fmt.Fprintf(&b, " %s", ReferralMarker)
		}
	}
	return b.String()
}

// Short is the compact form, for the status line: the provider name only, with
// the referral marker when one applies.
func (a Attribution) Short() string {
	if a.link != "" && a.isReferral {
		return fmt.Sprintf("%s %s", a.Provider, ReferralMarker)
	}
	return a.Provider
}
