package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// Coverage is total: every requirement of the plan has exactly one covered
// entry, and an acceptance bullet's entry names a root test. A coverage.json
// that falls short stops the run before anything starts.
func TestStartRunAssertsCoverageTotality(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*planner.CoverageFile)
	}{
		{"a requirement with no covered entry", func(c *planner.CoverageFile) { c.Covered = c.Covered[1:] }},
		{"an entry for no requirement", func(c *planner.CoverageFile) {
			c.Covered = append(c.Covered, planner.Covered{Requirement: "ZZ99"})
		}},
		{"a requirement covered twice", func(c *planner.CoverageFile) { c.Covered = append(c.Covered, c.Covered[0]) }},
		{"an acceptance bullet whose entry names no root test", func(c *planner.CoverageFile) {
			for i := range c.Covered {
				if c.Covered[i].Requirement == "A2" {
					c.Covered[i].RootTests = nil
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			editCoverage(t, g.runDir, tc.edit)
			fc := g.fastChecker()
			g.wire(goodScript(g))
			rep, err := run(context.Background(), g.options(), runFlags{afterStart: useChecker(fc)})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != "failed" || rep.StopReason != "coverage_incomplete" {
				t.Fatalf("report = %s (%s), want failed/coverage_incomplete", rep.Status, rep.StopReason)
			}
			if n := len(g.fake.Requests()); n != 0 {
				t.Errorf("%d model calls before the tripwire", n)
			}
			if fileExists(filepath.Join(g.runDir, "_state", "executor.json")) {
				t.Error("the run started despite the tripwire")
			}
		})
	}
}

func diffOnlyRig(t *testing.T) *rig {
	return newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: diff_only", 1) }
	})
}

// diff_only lands by writing the working tree as a patch, so a path outside
// the plan's files in that tree would ride along: landing is refused.
func TestDiffOnlyLandingRefusesForeignPaths(t *testing.T) {
	t.Parallel()
	g := diffOnlyRig(t)
	g.wire(goodScript(g))
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(g.repo, "notes-of-a-person.txt"), "not part of the plan\n")
	_, se, err := rc.land(context.Background(), accResult{})
	if err != nil {
		t.Fatal(err)
	}
	if se == nil || se.Reason != "landing_blocked" {
		t.Fatalf("stop = %+v, want landing_blocked", se)
	}
	if fileExists(filepath.Join(g.runDir, "changes.patch")) {
		t.Error("a patch was written for a tree with foreign paths")
	}
}

// A patch that holds a secret value is not written to the run folder.
func TestDiffOnlyLandingRefusesASecretInThePatch(t *testing.T) {
	t.Parallel()
	g := diffOnlyRig(t)
	g.wire(goodScript(g))
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	if err != nil {
		t.Fatal(err)
	}
	l := g.plan.Leaf(leafID)
	write(t, g.realPath(l), "package greet\n\n// "+canarySecret+"\n")
	if err := os.Remove(g.stubPath(l)); err != nil {
		t.Fatal(err)
	}
	_, se, err := rc.land(context.Background(), accResult{})
	if err != nil {
		t.Fatal(err)
	}
	if se == nil || se.Reason != "landing_blocked" {
		t.Fatalf("stop = %+v, want landing_blocked", se)
	}
	if fileExists(filepath.Join(g.runDir, "changes.patch")) {
		t.Error("a patch holding the secret was written")
	}
}
