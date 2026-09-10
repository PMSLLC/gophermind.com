package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"gophermind/internal/freellm"
)

func TestFreeListShowsNoKeyProvidersFirst(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"list"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{"free-ovhcloud", "free-groq", "no key"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	// A no-key provider must appear before a key-required one.
	if strings.Index(out, "free-ovhcloud") > strings.Index(out, "free-groq") {
		t.Error("key-required provider listed before a no-key one")
	}
}

func TestFreeListMarksTerms(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"list"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "non-commercial") {
		t.Errorf("list does not flag Cohere's non-commercial terms:\n%s", out)
	}
}

// TestFreeListJSONIsDataDriven covers the hidden "free list --json" mode
// that scripts/gen-free-providers-doc.sh relies on: it must emit valid JSON
// and reflect compat.go's Supported/NoKey flags exactly, so the generated
// doc never hand-types a provider fact that could drift.
func TestFreeListJSONIsDataDriven(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"list", "--json"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var entries []freeListEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	byProfile := make(map[string]freeListEntry, len(entries))
	for _, e := range entries {
		byProfile[e.Profile] = e
	}
	ovh, ok := byProfile["free-ovhcloud"]
	if !ok || !ovh.NoKey || !ovh.Supported {
		t.Errorf("free-ovhcloud entry wrong or missing: %+v (ok=%v)", ovh, ok)
	}
	llm7, ok := byProfile["free-llm7"]
	if !ok || llm7.Supported {
		t.Errorf("free-llm7 must be present and Supported=false: %+v (ok=%v)", llm7, ok)
	}
}

func TestFreeShowPrintsExportLines(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-groq"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{
		"https://groq.com",
		"openai/gpt-oss-120b",
		"GOPHERMIND_PROFILE_FREE_GROQ_API_KEY",
		"--profile free-groq",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestFreeShowNoKeyProviderOmitsKeyLine(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"show", "free-ovhcloud"}, &buf)
	if strings.Contains(buf.String(), "API_KEY") {
		t.Errorf("show told the user to set a key for a no-key provider:\n%s", buf.String())
	}
}

func TestFreeShowUnknownProfileErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-nope"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown profile")
	}
}

func TestFreeUsageRunsWithNoOdometer(t *testing.T) {
	t.Setenv("GOPHERMIND_ODOMETER", t.TempDir()+"/odo.json")
	var buf bytes.Buffer
	if code := runFree([]string{"usage"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(buf.String(), "0") {
		t.Errorf("usage output does not show a zero reading:\n%s", buf.String())
	}
}

func TestFreeUnknownSubcommandErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"wat"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown subcommand")
	}
}

// syntheticAffiliateCompat is a Compat with an affiliate link, used to drive
// the "free show" disclosure path below. No shipped Compat carries one today
// (see freellm.TestReferralAlwaysDisclosed), so this is the only way to
// exercise the referral branch at all; it is never registered with
// freellm.CompatFor and never reaches "gophermind free list/show" for a real
// profile.
var syntheticAffiliateCompat = freellm.Compat{
	Profile: "free-test-affiliate", Upstream: "TestProvider",
	Website: "https://provider.test", Affiliate: "https://provider.test/ref/1",
	DefaultModel: "test-model", Supported: true,
}

// TestFreeShowLinkLineDisclosesReferral is the CLI-layer half of the
// disclosure invariant: freellm.attribution_test.go proves Attribution.Line()
// and .Short() never emit a referral URL without freellm.ReferralMarker, but
// that test cannot see the CLI's own rendering. freeShowLinkLine is the exact
// function "free show" calls to build its "Link:" line, so this proves the
// same invariant for it: given a referral attribution, the marker is always
// present alongside the URL.
func TestFreeShowLinkLineDisclosesReferral(t *testing.T) {
	a := freellm.AttributionFromCompat(syntheticAffiliateCompat, syntheticAffiliateCompat.DefaultModel)
	line := freeShowLinkLine(a)
	if !strings.Contains(line, syntheticAffiliateCompat.Affiliate) {
		t.Errorf("Link line %q missing the affiliate URL %q", line, syntheticAffiliateCompat.Affiliate)
	}
	if !strings.Contains(line, freellm.ReferralMarker) {
		t.Errorf("Link line %q emits a referral URL without %q", line, freellm.ReferralMarker)
	}
}

// TestFreeShowLinkLineHonorsNoAffiliateOptOut proves GOPHERMIND_NO_AFFILIATE
// suppresses the referral link in the CLI's own rendering, not just inside
// freellm.attributionFrom: freeShowLinkLine sees a plain, non-referral
// Attribution (the opt-out is applied earlier, by AttributionFromCompat) and
// must print no Link line at all, carrying neither the affiliate URL nor the
// marker.
func TestFreeShowLinkLineHonorsNoAffiliateOptOut(t *testing.T) {
	t.Setenv(freellm.NoAffiliateEnv, "1")
	a := freellm.AttributionFromCompat(syntheticAffiliateCompat, syntheticAffiliateCompat.DefaultModel)
	line := freeShowLinkLine(a)
	if line != "" {
		t.Errorf("Link line = %q, want empty with %s set", line, freellm.NoAffiliateEnv)
	}
	if strings.Contains(line, syntheticAffiliateCompat.Affiliate) {
		t.Errorf("Link line %q carries the affiliate URL despite the opt-out", line)
	}
}
