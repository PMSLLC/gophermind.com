package planner_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

func TestSlugifyOutlineMapsPackagesToArchitectureDirs(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID)
	_, _ = g.plan(planner.Options{StopAfter: "contract"})
	for _, s := range g.stagesCalled() {
		if s == "contract:outline:entry" {
			t.Errorf("the entry pass was asked; stages = %v", g.stagesCalled())
		}
	}
	// The outline as stored: the recorded component replies put main in
	// cli-logic, so the fixed-interface check stops the run before
	// contracts.json is written.
	var st struct {
		Doc struct {
			Components []struct {
				Package string `json:"package"`
			} `json:"components"`
		} `json:"doc"`
	}
	if err := json.Unmarshal(g.read("_state/contract.json"), &st); err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for _, c := range st.Doc.Components {
		pkgs = append(pkgs, c.Package)
	}
	if got := strings.Join(pkgs, ","); got != "internal/slug,internal/cli,cmd/slugify" {
		t.Errorf("component packages = %s", got)
	}
	found := false
	for _, w := range g.sink.OfKind("warning") {
		if strings.HasPrefix(w.Message, "component_package_mapped:") && strings.Contains(w.Message, "slug-logic: slug -> internal/slug") {
			found = true
		}
	}
	if !found {
		t.Errorf("no component_package_mapped warning with slug-logic; warnings = %v", g.sink.OfKind("warning"))
	}
}
