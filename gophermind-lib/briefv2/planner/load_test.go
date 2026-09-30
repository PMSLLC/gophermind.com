package planner_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/vault"
)

func TestPlanCreatesTheRunFolderTheRegistryAndTheRequirements(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load", AllowPublic: true})

	if got := string(g.read("brief.md")); !strings.Contains(got, "id: "+greeterID) {
		t.Error("brief.md was not copied into the run folder")
	}
	var reqs []planner.Requirement
	if err := json.Unmarshal(g.read("requirements.json"), &reqs); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, q := range reqs {
		ids = append(ids, q.ID)
	}
	if got := strings.Join(ids, " "); got != "F1 F2 C1 C2 A1 A2 A3" {
		t.Errorf("requirements = %s, want F1 F2 C1 C2 A1 A2 A3", got)
	}
	rec, err := planner.LookupRun(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Repo != g.repo || rec.RunDir != g.runDir || rec.BriefPath != g.briefPath || rec.StartedAt == "" {
		t.Errorf("run record = %+v", rec)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.AllowPublic || st.Requirements != 7 || st.Covered != 0 || len(st.Stages) == 0 || st.Stages[0] != (planner.StageState{Name: "load", Done: true}) {
		t.Errorf("status = %+v", st)
	}
	for _, s := range st.Stages[1:] {
		if s.Done {
			t.Errorf("stage %s is done after load only", s.Name)
		}
	}
	if len(g.fake.Requests()) != 0 {
		t.Error("Load made a model call")
	}
}

func TestPlanRefusesARepoItCannotUse(t *testing.T) {
	cases := []struct{ name, repo, want string }{
		{"https url", "https://github.com/acme/greeter.git", "is a URL"},
		{"ssh url", "git@github.com:acme/greeter.git", "is a URL"},
		{"missing directory", filepath.Join(t.TempDir(), "nope"), "is not an existing directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving())
			g.briefPath = writeBrief(t, g.repo, func(s string) string { return strings.Replace(s, "repo: "+g.repo, "repo: "+c.repo, 1) })
			_, err := g.plan(planner.Options{})
			var inv *brief.InvalidError
			if !errors.As(err, &inv) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want an invalid-brief error containing %q", err, c.want)
			}
			if g.has("brief.md") {
				t.Error("a run folder was created for a refused brief")
			}
		})
	}
}

func TestPlanOnAnExistingRunPointsAtResume(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	_, err := g.plan(planner.Options{StopAfter: "load"})
	if err == nil || !strings.Contains(err.Error(), "gophermind brief resume "+greeterID) {
		t.Fatalf("second plan = %v, want an error pointing at resume", err)
	}
	if _, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "load"}); err != nil {
		t.Fatalf("resume of an existing run: %v", err)
	}
}

func TestRunNeedsABriefOrARunID(t *testing.T) {
	g := newRig(t, approving())
	if _, err := planner.New(g.deps).Run(t.Context(), planner.Options{}); err == nil {
		t.Error("Run with neither a brief nor a run id must fail")
	}
	if _, err := planner.LookupRun("gm-2026-09-29-901"); err == nil || !strings.Contains(err.Error(), "no run") {
		t.Errorf("LookupRun of an unknown run = %v", err)
	}
	if _, err := planner.LookupRun("../../etc/passwd"); err == nil || !strings.Contains(err.Error(), "not a run id") {
		t.Errorf("LookupRun of a path = %v", err)
	}
}

// memSecrets is a vault stand-in. It records every Set.
type memSecrets struct {
	vals map[string]string
	sets []string
}

func (m *memSecrets) Get(scope, name string) (string, bool) {
	v, ok := m.vals[scope+"/"+name]
	return v, ok
}

func (m *memSecrets) Set(scope, name, value string) error {
	if m.vals == nil {
		m.vals = map[string]string{}
	}
	m.vals[scope+"/"+name] = value
	m.sets = append(m.sets, scope+"/"+name)
	return nil
}

const canary = "canary-9f8e7d"

func withSecret(s string) string {
	return strings.Replace(s, "on_ambiguity: halt\n", "on_ambiguity: halt\nsecrets:\n  - name: GREETER_API_KEY\n    purpose: Signs greetings\n", 1)
}

func TestLoadPutsDeclaredSecretsInTheRunScope(t *testing.T) {
	runScope := vault.RunScope(greeterID) + "/GREETER_API_KEY"
	cases := []struct {
		name     string
		have     map[string]string
		prompt   bool
		wantSets int
		wantErr  string
	}{
		{"already stored for the run", map[string]string{runScope: canary}, false, 0, ""},
		{"copied from the harness scope", map[string]string{vault.HarnessScope + "/GREETER_API_KEY": canary}, false, 1, ""},
		{"asked for when someone can be asked", nil, true, 1, ""},
		{"missing with nobody to ask", nil, false, 0, "gophermind brief vault set GREETER_API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving())
			g.briefPath = writeBrief(t, g.repo, withSecret)
			store := &memSecrets{vals: c.have}
			g.deps.OpenSecrets = func() (planner.Secrets, error) { return store, nil }
			if c.prompt {
				g.deps.PromptSecret = func(name, purpose string) (string, error) {
					if name != "GREETER_API_KEY" || purpose != "Signs greetings" {
						t.Errorf("prompted for %q (%q)", name, purpose)
					}
					return canary, nil
				}
			}
			_, err := g.plan(planner.Options{StopAfter: "load"})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
				}
				if g.has("brief.md") {
					t.Error("a run folder was created although a secret is missing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := store.Get(vault.RunScope(greeterID), "GREETER_API_KEY"); got != canary {
				t.Errorf("run scope holds %q", got)
			}
			if len(store.sets) != c.wantSets {
				t.Errorf("Set was called %d times, want %d", len(store.sets), c.wantSets)
			}
			// The value is in the vault and nowhere in the run folder.
			filepath.WalkDir(g.repo, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if raw, _ := os.ReadFile(p); strings.Contains(string(raw), canary) {
						t.Errorf("%s contains the secret value", p)
					}
				}
				return nil
			})
		})
	}
}

func TestABriefWithoutSecretsNeverOpensTheVault(t *testing.T) {
	g := newRig(t, approving())
	g.deps.OpenSecrets = func() (planner.Secrets, error) {
		t.Error("the vault was opened for a brief that declares no secret")
		return nil, errors.New("unexpected")
	}
	g.mustPlan(planner.Options{StopAfter: "load"})
}

func TestARunIDAndADifferentBriefPathDisagree(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	other := writeBrief(t, g.repo, nil)
	_, err := g.plan(planner.Options{RunID: greeterID, BriefPath: other, StopAfter: "load"})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err = %v, want a disagreement error", err)
	}
	if _, err := g.plan(planner.Options{RunID: greeterID, BriefPath: g.briefPath, StopAfter: "load"}); err != nil {
		t.Fatalf("matching run id and brief path: %v", err)
	}
}
