package planner_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

var replyNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func TestPlannerSavesStageRepliesFreeOfSecrets(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, withSecret)
	g.deps.OpenSecrets = func() (planner.Secrets, error) {
		return &memSecrets{vals: map[string]string{"harness/GREETER_API_KEY": canary}}, nil
	}
	g.cfg.Executor.Artifacts = "on"
	g.mustPlan(planner.Options{})

	dir := filepath.Join(g.runDir, "_state", "replies")
	stages := g.stagesCalled()
	if len(stages) == 0 {
		t.Fatal("no model call was made")
	}
	seen := map[string]bool{}
	for _, s := range stages {
		name := replyNameRE.ReplaceAllString(s, "_") + "-1.txt"
		seen[strings.SplitN(s, ":", 2)[0]] = true
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("stage %s: %v", s, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, fi.Mode().Perm())
		}
	}
	for _, want := range []string{"clarify", "coverage"} {
		if !seen[want] {
			t.Errorf("stage %s did not run; saw %v", want, stages)
		}
	}

	filepath.WalkDir(g.runDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if raw, _ := os.ReadFile(p); strings.Contains(string(raw), canary) {
				t.Errorf("%s holds the secret value", p)
			}
		}
		return nil
	})

	loaded, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Nodes["fn-greet"]; !ok || len(loaded.Nodes) < 2 {
		t.Errorf("Load returned %d nodes, want the plan nodes", len(loaded.Nodes))
	}
}

func TestPlannerSavesNoRepliesWhenArtifactsAreOff(t *testing.T) {
	g := newRig(t, approving())
	g.cfg.Executor.Artifacts = "off"
	g.mustPlan(planner.Options{})
	if g.has("_state/replies") {
		t.Error("_state/replies exists although artifacts are off")
	}
	if loaded, err := tree.NewStore(g.runDir).Load(); err != nil || len(loaded.Nodes) < 2 {
		t.Errorf("Load = %d nodes, %v", len(loaded.Nodes), err)
	}
}
