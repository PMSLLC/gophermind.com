package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	greeterFixture = "../../gophermind-lib/briefv2/planner/testdata/greeter"
	greeterRunID   = "gm-2026-09-29-900"
)

// planEnv gives a test its own config dir (settings, run registry, database)
// and a target repo, and returns the repo and a greeter brief pointing at it.
func planEnv(t *testing.T) (repo, briefPath string) {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	repo = t.TempDir()
	raw, err := os.ReadFile(filepath.Join(greeterFixture, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	briefPath = filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(briefPath, []byte(strings.Replace(string(raw), "REPO_DIR", repo, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, briefPath
}

// fill writes text into the first empty or default-holding fenced block of
// the given kind in a gate file.
func fill(t *testing.T, path, kind, text string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "```"+kind) {
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(lines[end], "```") {
				end++
			}
			out := append(append(append([]string{}, lines[:i+1]...), text), lines[end:]...)
			if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("%s has no %s block", path, kind)
}

// The unattended flow: plan stops for the questions (exit 3), resume stops
// for the approval (exit 3), resume finishes (exit 0).
func TestBriefPlanWithTheFileGate(t *testing.T) {
	repo, briefPath := planEnv(t)
	runDir := filepath.Join(repo, ".gophermind", greeterRunID)

	code, out, errs := runBriefCmd(t, "", "plan", briefPath, "--fake", greeterFixture, "--gate", "file")
	if code != 3 {
		t.Fatalf("plan: code=%d out=%q err=%q, want 3 (waiting)", code, out, errs)
	}
	if !strings.Contains(out, "gophermind brief resume "+greeterRunID) || !strings.Contains(errs, "clarify: started") {
		t.Errorf("plan output: out=%q err=%q", out, errs)
	}
	fill(t, filepath.Join(runDir, "QUESTIONS.md"), "answer", "Yes, and collapse inner spaces too.")

	code, out, errs = runBriefCmd(t, "", "resume", greeterRunID, "--fake", greeterFixture, "--gate", "file")
	if code != 3 {
		t.Fatalf("first resume: code=%d out=%q err=%q, want 3 (waiting for approval)", code, out, errs)
	}
	approval, err := os.ReadFile(filepath.Join(runDir, "APPROVAL.md"))
	if err != nil || !strings.Contains(string(approval), "Requirements covered: 7 of 7") {
		t.Fatalf("APPROVAL.md does not show the coverage: %v", err)
	}
	if entries, _ := os.ReadDir(repo); len(entries) != 1 {
		t.Errorf("the repo holds %d entries before approval, want only .gophermind", len(entries))
	}
	code, out, _ = runBriefCmd(t, "", "status", greeterRunID)
	if code != 0 || !strings.Contains(out, "waiting on a human: approve") || !strings.Contains(out, "approve     not done") {
		t.Errorf("status while waiting: code=%d out=%q", code, out)
	}
	fill(t, filepath.Join(runDir, "APPROVAL.md"), "decision", "approve")

	code, out, errs = runBriefCmd(t, "", "resume", greeterRunID, "--fake", greeterFixture, "--gate", "file")
	if code != 0 || !strings.Contains(out, "planned: "+greeterRunID) || !strings.Contains(out, "Requirements covered: 7 of 7") {
		t.Fatalf("second resume: code=%d out=%q err=%q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(repo, "internal", "greet", "fn_greet_test.go")); err != nil {
		t.Errorf("the test file was not written: %v", err)
	}

	code, out, _ = runBriefCmd(t, "", "tree", "check", runDir)
	if code != 0 || !strings.Contains(out, "ok: 7 nodes, waves 0-1") {
		t.Errorf("tree check on the planned run: code=%d out=%q", code, out)
	}
	code, out, _ = runBriefCmd(t, "", "status", greeterRunID)
	if code != 0 || strings.Contains(out, "not done") || strings.Contains(out, "waiting on a human") {
		t.Errorf("status of a finished run: code=%d out=%q", code, out)
	}
	for _, want := range []string{"testwriter  done", "Requirements covered: 7 of 7", "TASK", "CLASS", "testwrite  validation  fake/fixture  2", "contract   -           fake/fixture  4"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = runBriefCmd(t, "", "calls", greeterRunID)
	if code != 0 || !strings.Contains(out, "12 call(s)") || !strings.Contains(out, "testwrite:fn-greet") || !strings.Contains(out, "contract:outline") {
		t.Errorf("calls: code=%d out=%q", code, out)
	}
	for _, want := range []string{"TASK", "CLASS", "validation", "OUTCOME"} {
		if !strings.Contains(out, want) {
			t.Errorf("calls lacks the %q column:\n%s", want, out)
		}
	}
	code, out, _ = runBriefCmd(t, "", "coverage", greeterRunID)
	if code != 0 || !strings.Contains(out, "Requirements covered: 7 of 7 (fill rounds: 0)") || !strings.Contains(out, "formatted and vetted") {
		t.Errorf("coverage: code=%d out=%q", code, out)
	}
}

// At a terminal with nothing typed, the default answer is taken and --yes
// stands in for the approval.
func TestBriefPlanWithYesAtATerminal(t *testing.T) {
	repo, briefPath := planEnv(t)
	code, out, errs := runBriefCmd(t, "", "plan", "--yes", "--fake", greeterFixture, "--gate", "terminal", "--allow-public", briefPath)
	if code != 0 || !strings.Contains(out, "planned: "+greeterRunID) {
		t.Fatalf("plan: code=%d out=%q err=%q", code, out, errs)
	}
	for _, want := range []string{"Should a name be trimmed", "coverage: done", "testwriter: done", "warning: --allow-public"} {
		if !strings.Contains(errs, want) {
			t.Errorf("progress output lacks %q:\n%s", want, errs)
		}
	}
	approval, err := os.ReadFile(filepath.Join(repo, ".gophermind", greeterRunID, "approval.json"))
	if err != nil || !strings.Contains(string(approval), `"approved_by": "flag"`) {
		t.Errorf("approval.json = %s, %v", approval, err)
	}
	if _, out, _ := runBriefCmd(t, "", "status", greeterRunID); !strings.Contains(out, "public providers were allowed") {
		t.Errorf("status does not say the run allowed public providers:\n%s", out)
	}
	code, _, errs = runBriefCmd(t, "", "plan", briefPath, "--yes", "--fake", greeterFixture)
	if code != 1 || !strings.Contains(errs, "gophermind brief resume "+greeterRunID) {
		t.Errorf("planning the same brief twice: code=%d err=%q, want 1 and a pointer at resume", code, errs)
	}
}

func TestBriefPlanExitCodes(t *testing.T) {
	_, briefPath := planEnv(t)
	raw, _ := os.ReadFile(briefPath)
	write := func(text string) string {
		p := filepath.Join(t.TempDir(), "brief.md")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	noVersion := write(strings.Replace(string(raw), "spec_version: \"2.0\"\n", "", 1))
	urlRepo := write(strings.Replace(string(raw), "repo: ", "repo: https://example.com/x.git #", 1))
	stuck := "../../gophermind-lib/briefv2/planner/testdata/greeter-stuck"

	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"an invalid brief is 2", []string{"plan", noVersion, "--fake", greeterFixture}, 2, "spec_version"},
		{"a repo that is a URL is 2", []string{"plan", urlRepo, "--fake", greeterFixture}, 2, "is a URL"},
		{"an unreadable brief is 1", []string{"plan", filepath.Join(t.TempDir(), "nope.md")}, 1, "no such file"},
		{"no brief is a usage error", []string{"plan"}, 1, "usage:"},
		{"two briefs is a usage error", []string{"plan", briefPath, briefPath}, 1, "usage:"},
		{"an unknown flag is a usage error", []string{"plan", briefPath, "--fast"}, 1, "usage:"},
		{"an unknown gate is 1", []string{"plan", briefPath, "--fake", greeterFixture, "--gate", "pigeon"}, 1, `unknown gate "pigeon"`},
		{"a missing fixture directory is 1", []string{"plan", briefPath, "--fake", filepath.Join(t.TempDir(), "nope")}, 1, "fixture directory"},
		{"an unknown run is 1", []string{"resume", "gm-2026-01-01-001", "--fake", greeterFixture}, 1, "no run gm-2026-01-01-001"},
		{"status of an unknown run is 1", []string{"status", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"calls of an unknown run is 1", []string{"calls", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"coverage of an unknown run is 1", []string{"coverage", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"status needs a run id", []string{"status"}, 1, "usage:"},
		{"uncovered requirements are 1", []string{"plan", briefPath, "--yes", "--fake", stuck + "," + greeterFixture}, 1, "1 requirement(s) of the brief are not covered by the plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errs := runBriefCmd(t, "", c.args...)
			if code != c.code || !strings.Contains(errs, c.want) {
				t.Errorf("code=%d out=%q err=%q, want %d and %q", code, out, errs, c.code, c.want)
			}
		})
	}
}
