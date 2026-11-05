package planner_test

import (
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
	var pkgs []string
	for _, c := range g.contracts().Components {
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
