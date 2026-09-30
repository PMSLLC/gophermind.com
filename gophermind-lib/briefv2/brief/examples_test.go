package brief_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
)

// examplesDir is the folder the desktop app ships as its Examples. The short
// starters (01 to 03) are plain text with no frontmatter; 04 and later are
// complete v2 briefs.
const examplesDir = "../../../gophermind-osx/examples/briefs"

// completeExamples parses every example that starts with a frontmatter block,
// keyed by file name. A brief that no longer parses fails the calling test.
func completeExamples(t *testing.T) map[string]*brief.Brief {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(examplesDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*brief.Brief{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(raw), "---\n") {
			continue
		}
		b, err := brief.Parse(raw)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		out[filepath.Base(f)] = b
	}
	return out
}

func TestCompleteExampleBriefsValidateWithoutWarnings(t *testing.T) {
	ex := completeExamples(t)
	if len(ex) != 5 {
		names := make([]string, 0, len(ex))
		for n := range ex {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("expected 5 complete example briefs, found %d: %v", len(ex), names)
	}
	ids := map[string]string{}
	for name, b := range ex {
		if w := b.UndeclaredSecrets(); len(w) != 0 {
			t.Errorf("%s: %d scan warnings, first %+v", name, len(w), w[0])
		}
		if prev, dup := ids[b.Front.ID]; dup {
			t.Errorf("%s and %s share the id %s", name, prev, b.Front.ID)
		}
		ids[b.Front.ID] = name
		if b.Front.Language != "go" {
			t.Errorf("%s: language %q", name, b.Front.Language)
		}
	}
}

// The set exists to show every option the format has. If a brief is edited or
// removed so that an option is no longer demonstrated, this fails and names it.
func TestCompleteExampleBriefsCoverEveryOption(t *testing.T) {
	ex := completeExamples(t)

	landing, ambiguity, revisions := map[string]bool{}, map[string]bool{}, map[int]bool{}
	var milestoneOn, milestoneOff, explicitBranch, defaultBranch bool
	var envDefault, envNoDefault, critical, notCritical, wildcard bool
	minSecrets, maxSecrets := 1<<30, 0
	minFeatures, maxFeatures := 1<<30, 0

	for _, b := range ex {
		f := b.Front
		landing[f.Landing] = true
		ambiguity[f.OnAmbiguity] = true
		if f.MilestoneApprovals {
			milestoneOn = true
		} else {
			milestoneOff = true
		}
		if f.WorkBranch != "" {
			explicitBranch = true
		} else {
			defaultBranch = true
		}
		if n := len(f.Secrets); n < minSecrets {
			minSecrets = n
		}
		if n := len(f.Secrets); n > maxSecrets {
			maxSecrets = n
		}
		for _, e := range f.Env {
			if e.Default != nil {
				envDefault = true
			} else {
				envNoDefault = true
			}
		}
		for _, h := range f.Network {
			if h.Critical {
				critical = true
			} else {
				notCritical = true
			}
			if strings.HasPrefix(h.Host, "*.") {
				wildcard = true
			}
		}
		if f.Budget != nil && f.Budget.MaxRevisions != nil {
			revisions[*f.Budget.MaxRevisions] = true
		}
		if n := len(b.Features); n < minFeatures {
			minFeatures = n
		}
		if n := len(b.Features); n > maxFeatures {
			maxFeatures = n
		}
	}

	checks := []struct {
		what string
		ok   bool
	}{
		{"landing diff_only", landing["diff_only"]},
		{"landing commit", landing["commit"]},
		{"landing pull_request", landing["pull_request"]},
		{"on_ambiguity halt", ambiguity["halt"]},
		{"on_ambiguity assume_and_document", ambiguity["assume_and_document"]},
		{"milestone approvals on", milestoneOn},
		{"milestone approvals off", milestoneOff},
		{"an explicit work_branch", explicitBranch},
		{"the default work branch", defaultBranch},
		{"a brief with no secrets", minSecrets == 0},
		{"a brief with three or more secrets", maxSecrets >= 3},
		{"an env setting with a default", envDefault},
		{"an env setting without a default", envNoDefault},
		{"a critical host", critical},
		{"a host that may fail", notCritical},
		{"a wildcard host", wildcard},
		{"max_revisions 0", revisions[0]},
		{"four different max_revisions values", len(revisions) >= 4},
		{"a brief with two features or fewer", minFeatures <= 2},
		{"a brief with seven features or more", maxFeatures >= 7},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("no example brief demonstrates: %s", c.what)
		}
	}
}
