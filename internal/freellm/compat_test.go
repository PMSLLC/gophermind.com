package freellm

import (
	"regexp"
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

// versionSegmentRe matches a URL path segment that looks like an API version
// marker: v1, v4, v1beta, and so on.
var versionSegmentRe = regexp.MustCompile(`^v[0-9]+[a-z0-9]*$`)

// assertJoinIsWellFormed is the shared body of the ChatPath/ModelsPath join
// invariants. It checks, on the already-scheme-stripped path:
//   - no "//" (a double slash, e.g. a BaseURL with a trailing slash joined
//     with a path that also starts with one)
//   - ends in exactly one occurrence of wantSuffix (not doubled, as happens
//     when BaseURL already ends in a version segment and the override path
//     is left empty)
//   - at most one path segment that looks like an API version marker
//
// That last check is the one a weaker "no // and ends in wantSuffix exactly
// once" invariant CANNOT catch: https://api.groq.com/openai/v1 joined with
// an EMPTY ChatPath (the client default /v1/chat/completions) produces
// .../openai/v1/v1/chat/completions, no double slash, ends in exactly one
// "/chat/completions", and would pass a suffix-only check despite being
// exactly the doubled-version-segment bug this task exists to fix. Checking
// for the literal substring "/v1/v1" alone would also miss it for an entry
// whose own version differs from "v1" (e.g. free-zai's BaseURL ends in
// /v4): .../v4/v1/chat/completions has no "/v1/v1" but still carries two
// distinct version segments back to back. Counting version-LOOKING segments
// generically catches both shapes.
//
// Also note: the pre-fix free-gemini entry (BaseURL with a trailing slash,
// paired with a special-cased bare "chat/completions" override with no
// leading slash) would have PASSED every check here, no double slash, no
// doubled suffix, and only one version segment (/v1beta). Passing this
// invariant is not proof a join avoids every possible mistake; it is proof
// against the specific doubled-version-segment and doubled-slash bug
// classes this task fixes.
func assertJoinIsWellFormed(t *testing.T, profile, joined, wantSuffix string) {
	t.Helper()
	afterScheme := joined
	if i := strings.Index(joined, "://"); i >= 0 {
		afterScheme = joined[i+len("://"):]
	}
	if strings.Contains(afterScheme, "//") {
		t.Errorf("profile %q: %q contains a double slash after the scheme", profile, joined)
	}
	if !strings.HasSuffix(joined, wantSuffix) {
		t.Errorf("profile %q: %q does not end in %s", profile, joined, wantSuffix)
	}
	if n := strings.Count(joined, wantSuffix); n != 1 {
		t.Errorf("profile %q: %q contains %s %d times, want exactly 1", profile, joined, wantSuffix, n)
	}
	versionSegs := 0
	for _, seg := range strings.Split(afterScheme, "/") {
		if versionSegmentRe.MatchString(seg) {
			versionSegs++
		}
	}
	if versionSegs > 1 {
		t.Errorf("profile %q: %q contains %d version-like path segments, want at most 1", profile, joined, versionSegs)
	}
}

// TestChatPathJoinNeverDoublesSlashOrPath enforces the join convention every
// supported entry must follow, turning it from a comment someone has to
// notice into an enforced rule. For each supported entry, BaseURL is joined
// with the EFFECTIVE ChatPath (empty defaults to "/v1/chat/completions",
// matching llm.Client.chatPath()'s default exactly) and checked by
// assertJoinIsWellFormed.
func TestChatPathJoinNeverDoublesSlashOrPath(t *testing.T) {
	const defaultChatPath = "/v1/chat/completions"
	for _, c := range Compats() {
		if !c.Supported {
			continue
		}
		effective := c.ChatPath
		if effective == "" {
			effective = defaultChatPath
		}
		assertJoinIsWellFormed(t, c.Profile, c.BaseURL+effective, "/chat/completions")
	}
}

// TestModelsPathJoinNeverDoublesSlashOrPath is TestChatPathJoinNeverDoublesSlashOrPath's
// counterpart for ModelsPath (empty defaults to "/v1/models", matching
// llm.Client.modelsPath()'s default exactly).
func TestModelsPathJoinNeverDoublesSlashOrPath(t *testing.T) {
	const defaultModelsPath = "/v1/models"
	for _, c := range Compats() {
		if !c.Supported {
			continue
		}
		effective := c.ModelsPath
		if effective == "" {
			effective = defaultModelsPath
		}
		assertJoinIsWellFormed(t, c.Profile, c.BaseURL+effective, "/models")
	}
}
