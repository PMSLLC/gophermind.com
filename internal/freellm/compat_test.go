package freellm

import (
	"sort"
	"strings"
	"testing"
)

// TestCompatUpstreamResolves is the drift guard. If a sync drops or renames a
// provider, this fails and names it, instead of shipping a profile that 404s.
func TestCompatUpstreamResolves(t *testing.T) {
	r := Load()
	for _, c := range Compats() {
		if _, ok := r.Lookup(c.Upstream); !ok {
			t.Errorf("profile %q names upstream provider %q, which is not in data.json", c.Profile, c.Upstream)
		}
	}
}

// TestDefaultModelExistsUpstream is the other half of the drift guard: a model
// upstream renamed must not stay as a profile default.
func TestDefaultModelExistsUpstream(t *testing.T) {
	r := Load()
	for _, c := range Compats() {
		if !c.Supported {
			continue
		}
		p, ok := r.Lookup(c.Upstream)
		if !ok {
			continue // reported by TestCompatUpstreamResolves
		}
		found := false
		for _, m := range p.Models {
			if m.ID == c.DefaultModel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("profile %q default model %q is not listed for %q upstream", c.Profile, c.DefaultModel, c.Upstream)
		}
	}
}

func TestSupportedEntriesAreComplete(t *testing.T) {
	for _, c := range Compats() {
		if !strings.HasPrefix(c.Profile, ProfilePrefix) {
			t.Errorf("profile %q lacks the %q prefix", c.Profile, ProfilePrefix)
		}
		if c.Website == "" {
			t.Errorf("profile %q has no website", c.Profile)
		}
		if !c.Supported {
			if c.Note == "" {
				t.Errorf("unsupported profile %q must explain why in Note", c.Profile)
			}
			continue
		}
		if !strings.HasPrefix(c.BaseURL, "https://") {
			t.Errorf("profile %q base URL %q is not https", c.Profile, c.BaseURL)
		}
		if c.DefaultModel == "" {
			t.Errorf("supported profile %q has no default model", c.Profile)
		}
	}
}

// TestNoKeyMaterial guards the invariant that compat.go never carries secrets.
func TestNoKeyMaterial(t *testing.T) {
	for _, c := range Compats() {
		for _, field := range []string{c.BaseURL, c.Website, c.Affiliate, c.Note} {
			low := strings.ToLower(field)
			for _, bad := range []string{"api_key", "apikey", "secret", "bearer ", "sk-"} {
				if strings.Contains(low, bad) {
					t.Errorf("profile %q field contains key-shaped text %q", c.Profile, bad)
				}
			}
		}
	}
}

func TestCompatsSortedNoKeyFirst(t *testing.T) {
	cs := Compats()
	// No-key entries must all precede key-required entries.
	seenKeyed := false
	for _, c := range cs {
		if !c.NoKey {
			seenKeyed = true
			continue
		}
		if seenKeyed {
			t.Errorf("no-key profile %q sorts after a key-required profile", c.Profile)
		}
	}
	// Within the no-key group, alphabetical.
	var noKey []string
	for _, c := range cs {
		if c.NoKey {
			noKey = append(noKey, c.Profile)
		}
	}
	if !sort.StringsAreSorted(noKey) {
		t.Errorf("no-key group is not alphabetical: %v", noKey)
	}
}

func TestCompatForHitAndMiss(t *testing.T) {
	if _, ok := CompatFor("free-groq"); !ok {
		t.Error("free-groq not found")
	}
	if _, ok := CompatFor("free-nope"); ok {
		t.Error("CompatFor reported a hit for an unknown profile")
	}
}

// TestTermsFlags covers all three flags set, none set, and one set.
func TestTermsFlags(t *testing.T) {
	all := TermsFlags(Terms{NonCommercial: true, TrainsOnPrompts: true, IdentityCheck: true})
	wantAll := []string{"non-commercial", "trains on prompts", "identity check"}
	if len(all) != len(wantAll) {
		t.Fatalf("all-flags: got %v, want %v", all, wantAll)
	}
	for i := range wantAll {
		if all[i] != wantAll[i] {
			t.Errorf("all-flags[%d]: got %q, want %q", i, all[i], wantAll[i])
		}
	}

	none := TermsFlags(Terms{})
	if len(none) != 0 {
		t.Errorf("no-flags: got %v, want empty", none)
	}

	one := TermsFlags(Terms{TrainsOnPrompts: true})
	if len(one) != 1 || one[0] != "trains on prompts" {
		t.Errorf("one-flag: got %v, want [trains on prompts]", one)
	}
}
