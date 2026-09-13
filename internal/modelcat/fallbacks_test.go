package modelcat

import (
	"strings"
	"testing"
)

// A 429 is fallback-eligible in llm.Client, but only across the models in its
// Fallbacks list, and that list was populated solely from manual config. With
// it empty a rate-limited provider just failed the turn.
//
// The list has to stay on ONE provider: llm.Client swaps only the model
// string, keeping the same BaseURL and key, so another provider's model id
// sent to this endpoint is a 404 rather than a fallback. That is the same
// constraint DefaultCandidates hit.
func TestFallbackModelsStayOnOneProvider(t *testing.T) {
	entries := []Entry{
		{ID: "a-1", Profile: "p1", Provider: "P1", Reachable: true},
		{ID: "a-2", Profile: "p1", Provider: "P1", Reachable: true},
		{ID: "b-1", Profile: "p2", Provider: "P2", Reachable: true},
	}
	got := FallbackModels(entries, Settings{}, "p1", "a-1")
	for _, m := range got {
		if strings.HasPrefix(m, "b-") {
			t.Errorf("fallback %q belongs to another provider", m)
		}
	}
	if len(got) != 1 || got[0] != "a-2" {
		t.Fatalf("got %v, want just the sibling model a-2", got)
	}
}

// The model already in use must not appear: llm.Client tries Model first and
// then the fallbacks, so repeating it wastes an attempt on the endpoint that
// just refused.
func TestFallbackModelsExcludeTheCurrentModel(t *testing.T) {
	entries := []Entry{
		{ID: "a-1", Profile: "p1", Reachable: true},
		{ID: "a-2", Profile: "p1", Reachable: true},
	}
	for _, m := range FallbackModels(entries, Settings{}, "p1", "a-1") {
		if m == "a-1" {
			t.Error("the current model was listed as its own fallback")
		}
	}
}

// An unreachable model cannot answer, and a term the user excluded must never
// be reached by any automatic path. That exclusion exists for legal reasons
// and a fallback is exactly the kind of quiet path that would defeat it.
func TestFallbackModelsRespectReachabilityAndExclusions(t *testing.T) {
	entries := []Entry{
		{ID: "ok", Profile: "p1", Reachable: true},
		{ID: "down", Profile: "p1", Reachable: false},
		{ID: "nc", Profile: "p1", Reachable: true, Terms: []string{"non-commercial"}},
	}
	got := FallbackModels(entries, Settings{ExcludedTerms: []string{"non-commercial"}}, "p1", "other")
	for _, m := range got {
		if m == "down" {
			t.Error("an unreachable model was offered as a fallback")
		}
		if m == "nc" {
			t.Error("a model with an excluded term was offered as a fallback")
		}
	}
	if len(got) != 1 || got[0] != "ok" {
		t.Fatalf("got %v, want just ok", got)
	}
}

// A run of fallbacks is bounded. Every entry in the list is an attempt against
// a provider that has already refused once, so a long list turns one slow turn
// into a very slow one.
func TestFallbackModelsAreBounded(t *testing.T) {
	var entries []Entry
	for i := 0; i < 40; i++ {
		entries = append(entries, Entry{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Profile: "p1", Reachable: true})
	}
	if got := FallbackModels(entries, Settings{}, "p1", "zz"); len(got) > MaxFallbacks {
		t.Errorf("got %d fallbacks, want at most %d", len(got), MaxFallbacks)
	}
}

// No profile means the user's own endpoint, which has no catalogue siblings.
func TestFallbackModelsEmptyForOwnEndpoint(t *testing.T) {
	entries := []Entry{{ID: "a", Profile: "p1", Reachable: true}}
	if got := FallbackModels(entries, Settings{}, "", "local"); len(got) != 0 {
		t.Errorf("got %v, want none for the user's own endpoint", got)
	}
}
