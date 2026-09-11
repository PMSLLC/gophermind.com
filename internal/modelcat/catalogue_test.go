package modelcat

import (
	"strings"
	"testing"
	"time"

	"gophermind/internal/freellm"
)

func TestBuildIncludesEveryRegistryModel(t *testing.T) {
	entries := Build(nil, DefaultSettings(), nil, time.Now())
	if len(entries) < 100 {
		t.Fatalf("catalogue has %d entries, expected the full registry (100+)", len(entries))
	}
}

func TestNoKeyProviderIsReachable(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Profile == "free-ovhcloud" {
			if !e.Reachable {
				t.Errorf("a no-key provider reported unreachable: %s", e.Reason)
			}
			return
		}
	}
	t.Fatal("free-ovhcloud not in the catalogue")
}

func TestKeyedProviderReachabilityFollowsTheEnvVar(t *testing.T) {
	find := func(entries []Entry) *Entry {
		for i := range entries {
			if entries[i].Profile == "free-groq" {
				return &entries[i]
			}
		}
		return nil
	}
	e := find(Build(nil, DefaultSettings(), nil, time.Now()))
	if e == nil {
		t.Fatal("free-groq not in the catalogue")
	}
	if e.Reachable {
		t.Skip("GOPHERMIND_PROFILE_FREE_GROQ_API_KEY is set in this environment")
	}
	if !strings.Contains(e.Reason, "GOPHERMIND_PROFILE_FREE_GROQ_API_KEY") {
		t.Errorf("reason does not name the variable to set: %q", e.Reason)
	}

	t.Setenv("GOPHERMIND_PROFILE_FREE_GROQ_API_KEY", "k")
	if e2 := find(Build(nil, DefaultSettings(), nil, time.Now())); e2 == nil || !e2.Reachable {
		t.Error("setting the key did not make the provider reachable")
	}
}

func TestUnsupportedProviderCarriesItsNote(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Profile == "free-cloudflare" {
			if e.Reachable {
				t.Error("an unsupported provider reported reachable")
			}
			if e.Reason == "" {
				t.Error("unsupported provider has no reason")
			}
			return
		}
	}
}

// The denominator rule, again, at the catalogue layer.
func TestNoQuotaIsInventedOrNearCapacity(t *testing.T) {
	for _, e := range Build(nil, DefaultSettings(), nil, time.Now()) {
		if e.Quota == 0 && e.NearCapacity {
			t.Errorf("%s/%s is NearCapacity with no published quota", e.Profile, e.ID)
		}
	}
}

// A model on the user's own endpoint has no provider and no quota.
func TestEndpointModelsAppearWithNoProfile(t *testing.T) {
	entries := Build(nil, DefaultSettings(), []string{"qwen3.6-35b-a3b"}, time.Now())
	for _, e := range entries {
		if e.ID == "qwen3.6-35b-a3b" && e.Profile == "" {
			if e.Quota != 0 {
				t.Error("a local endpoint model reported a quota")
			}
			if !e.Reachable {
				t.Error("a model the endpoint serves reported unreachable")
			}
			return
		}
	}
	t.Fatal("endpoint model missing from the catalogue")
}

func TestDeriveModelURL(t *testing.T) {
	if got := DeriveModelURL("Qwen/Qwen3-8B"); got != "https://huggingface.co/Qwen/Qwen3-8B" {
		t.Errorf("got %q", got)
	}
	// A routing-variant suffix must not derive a link: these are real registry
	// ids from OpenRouter and Kilo Code, and huggingface.co/<owner>/<name>:free
	// does not exist.
	for _, id := range []string{
		"nvidia/nemotron-3-super-120b-a12b:free",
		"poolside/laguna-s-2.1:free",
		"cohere/north-mini-code:free",
	} {
		if got := DeriveModelURL(id); got != "" {
			t.Errorf("DeriveModelURL(%q) = %q, want empty: the :free suffix is a routing marker, not a repo name", id, got)
		}
	}
	for _, id := range []string{"gpt-oss-120b", "glm-4.7-flash", "a/b/c", "", "has space/x"} {
		if got := DeriveModelURL(id); got != "" {
			t.Errorf("DeriveModelURL(%q) = %q, want empty rather than a guess", id, got)
		}
	}
}

func TestCustomLinkBeatsDerived(t *testing.T) {
	s := DefaultSettings()
	s.CustomLinks = map[string]string{"free-huggingface/Qwen/Qwen2.5-7B-Instruct": "https://example.test/mine"}
	for _, e := range Build(nil, s, nil, time.Now()) {
		if e.Profile == "free-huggingface" && e.ID == "Qwen/Qwen2.5-7B-Instruct" {
			if e.ModelURL != "https://example.test/mine" {
				t.Errorf("custom link did not win: %q", e.ModelURL)
			}
			return
		}
	}
	t.Fatal("target model not in the catalogue")
}

// Reachability is exported and used standalone by the catalogue tests above
// only indirectly; exercise it directly too so its contract is pinned apart
// from Build's assembly.
func TestReachabilityDirect(t *testing.T) {
	c, ok := freellm.CompatFor("free-ovhcloud")
	if !ok {
		t.Fatal("free-ovhcloud not in the compat table")
	}
	reachable, reason := Reachability(c)
	if !reachable || reason != "" {
		t.Errorf("no-key provider: reachable=%v reason=%q", reachable, reason)
	}
}
