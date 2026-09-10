package freellm

import (
	"strings"
	"testing"
)

func TestAttributionLine(t *testing.T) {
	a, ok := AttributionFor("free-groq", "openai/gpt-oss-120b")
	if !ok {
		t.Fatal("free-groq has no attribution")
	}
	line := a.Line()
	for _, want := range []string{"openai/gpt-oss-120b", "Groq", "https://groq.com", "free tier"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, referralMarker) {
		t.Errorf("line %q claims a referral link where none is configured", line)
	}
}

func TestAttributionMissForPaidProfile(t *testing.T) {
	if _, ok := AttributionFor("openai", "gpt-4o-mini"); ok {
		t.Error("a built-in paid profile must not produce free attribution")
	}
	if _, ok := AttributionFor("", "some-model"); ok {
		t.Error("an empty profile must not produce free attribution")
	}
}

// TestReferralAlwaysDisclosed is the invariant that makes an affiliate link
// safe to add later: there is no code path that emits one unmarked. The loop
// body does not execute today because every shipped Compat.Affiliate is
// empty; it exists to fire the day one is added.
func TestReferralAlwaysDisclosed(t *testing.T) {
	for _, c := range Compats() {
		if c.Affiliate == "" {
			continue
		}
		a, ok := AttributionFor(c.Profile, c.DefaultModel)
		if !ok {
			t.Errorf("profile %q has an affiliate link but no attribution", c.Profile)
			continue
		}
		for name, out := range map[string]string{"Line": a.Line(), "Short": a.Short()} {
			if !strings.Contains(out, referralMarker) {
				t.Errorf("profile %q %s() = %q, which emits a referral link without %q",
					c.Profile, name, out, referralMarker)
			}
		}
	}
}

// TestReferralDisclosureInvariant proves the same invariant against
// attributionFrom directly, so it holds even while every shipped Affiliate is
// empty, and pins the guarantee that the link and its referral status can
// only be set together by this package.
func TestReferralDisclosureInvariant(t *testing.T) {
	// A zero Attribution renders no URL and no marker.
	var zero Attribution
	for name, out := range map[string]string{"Line": zero.Line(), "Short": zero.Short()} {
		if strings.Contains(out, referralMarker) {
			t.Errorf("zero value %s() = %q, unexpectedly carries %q", name, out, referralMarker)
		}
		if strings.Contains(out, "http") {
			t.Errorf("zero value %s() = %q, unexpectedly carries a URL", name, out)
		}
	}

	// A synthetic Compat with an affiliate link renders both the URL and the
	// marker, from both Line() and Short().
	c := Compat{
		Profile: "free-x", Upstream: "X", Website: "https://x.test",
		Affiliate: "https://x.test/ref/1", Supported: true,
	}
	a := attributionFrom(c, "m")
	if !a.IsReferral() {
		t.Error("IsReferral() is false with an affiliate link configured")
	}
	if a.Link() != c.Affiliate {
		t.Errorf("Link() = %q, want the affiliate URL %q", a.Link(), c.Affiliate)
	}
	if !strings.Contains(a.Line(), c.Affiliate) {
		t.Errorf("Line() = %q, missing the affiliate URL %q", a.Line(), c.Affiliate)
	}
	for name, out := range map[string]string{"Line": a.Line(), "Short": a.Short()} {
		if !strings.Contains(out, referralMarker) {
			t.Errorf("%s() = %q, missing %q", name, out, referralMarker)
		}
	}
}

func TestNoAffiliateEnvForcesWebsite(t *testing.T) {
	t.Setenv(NoAffiliateEnv, "1")
	c := Compat{
		Profile: "free-x", Upstream: "X", Website: "https://x.test",
		Affiliate: "https://x.test/ref/1", Supported: true,
	}
	a := attributionFrom(c, "m")
	if a.Link() != "https://x.test" {
		t.Errorf("Link() = %q, want the plain website with %s set", a.Link(), NoAffiliateEnv)
	}
	if a.IsReferral() {
		t.Error("IsReferral() is true with the opt-out set")
	}
	for name, out := range map[string]string{"Line": a.Line(), "Short": a.Short()} {
		if strings.Contains(out, referralMarker) {
			t.Errorf("%s() = %q, carries %q despite the opt-out", name, out, referralMarker)
		}
		if strings.Contains(out, c.Affiliate) {
			t.Errorf("%s() = %q, carries the affiliate URL despite the opt-out", name, out)
		}
	}
}
