package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/phaseflow"
)

func TestIsSpecReady(t *testing.T) {
	if !isSpecReady("great, that's enough.\n[[SPEC-READY]]") {
		t.Error("should detect the readiness sentinel")
	}
	if isSpecReady("what is your target audience?") {
		t.Error("a normal question is not ready")
	}
}

func TestParseApproval(t *testing.T) {
	cases := []struct {
		in     string
		kind   projectApproval
		revise string
	}{
		{"y", approvalApprove, ""},
		{"YES", approvalApprove, ""},
		{"approve", approvalApprove, ""},
		{"cancel", approvalCancel, ""},
		{"abort", approvalCancel, ""},
		{"revise: split phase 3", approvalRevise, "split phase 3"},
		{"make the CLI a separate phase", approvalRevise, "make the CLI a separate phase"},
	}
	for _, c := range cases {
		kind, revise := parseApproval(c.in)
		if kind != c.kind || revise != c.revise {
			t.Errorf("parseApproval(%q) = (%v,%q), want (%v,%q)", c.in, kind, revise, c.kind, c.revise)
		}
	}
}

// TestParseProjectCommandNameOnly pins the existing "/project [name]" grammar:
// a name with no trailing file path is not mistaken for one.
func TestParseProjectCommandNameOnly(t *testing.T) {
	name, brief := parseProjectCommand("/project My Cool App")
	if name != "My Cool App" || brief != "" {
		t.Errorf("got (%q,%q), want (%q,%q)", name, brief, "My Cool App", "")
	}
}

// TestParseProjectCommandNoName covers the bare "/project" case that asks for
// a name interactively.
func TestParseProjectCommandNoName(t *testing.T) {
	name, brief := parseProjectCommand("/project")
	if name != "" || brief != "" {
		t.Errorf("got (%q,%q), want empty name and brief", name, brief)
	}
}

// TestParseProjectCommandWithBrief is the new grammar: a trailing token that
// is a real file is the brief, and everything between the name and it is the
// project name.
func TestParseProjectCommandWithBrief(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("a CLI tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, gotBrief := parseProjectCommand("/project My Cool App " + brief)
	if name != "My Cool App" || gotBrief != brief {
		t.Errorf("got (%q,%q), want (%q,%q)", name, gotBrief, "My Cool App", brief)
	}
}

// TestParseProjectCommandSingleTokenNotMistakenForBrief guards the two-field
// case: "/project <path>" alone has no name before the path, so per the
// design it is treated as a (probably odd-looking) name, not a nameless
// brief -- /project always requires a name.
func TestParseProjectCommandSingleTokenNotMistakenForBrief(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("a CLI tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, gotBrief := parseProjectCommand("/project " + brief)
	if name != brief || gotBrief != "" {
		t.Errorf("got (%q,%q), want the lone token treated as the name", name, gotBrief)
	}
}

// TestParseProjectCommandNonexistentTrailingPath makes sure a name that
// merely ends in something path-shaped, but does not exist as a file, is
// never misread as a brief.
func TestParseProjectCommandNonexistentTrailingPath(t *testing.T) {
	name, brief := parseProjectCommand("/project Widget /no/such/file.md")
	if name != "Widget /no/such/file.md" || brief != "" {
		t.Errorf("got (%q,%q), want the whole thing kept as the name", name, brief)
	}
}

// TestStartProjectReadsBriefFile: a brief path given to startProject must
// land in m.projBrief so the interview prompt (see
// TestInterviewPromptCarriesBrief) actually sees it.
func TestStartProjectReadsBriefFile(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("a CLI todo app"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkdir(t, dir, func() {
		m := model{}
		nm, _ := m.startProject("Demo", brief)
		if nm.projBrief != "a CLI todo app" {
			t.Errorf("projBrief = %q, want the brief file's content", nm.projBrief)
		}
	})
}

// TestStartProjectMissingBriefFileErrors: a path the user typed but that
// cannot be read must surface as an error, not silently fall back to a
// brief-less interview.
func TestStartProjectMissingBriefFileErrors(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		m := model{}
		nm, _ := m.startProject("Demo", filepath.Join(dir, "missing.md"))
		if nm.proj != projNone {
			t.Errorf("proj = %v, want projNone after a brief read error", nm.proj)
		}
		if !strings.Contains(nm.content, "project:") {
			t.Errorf("expected an error line, got:\n%s", nm.content)
		}
	})
}

func TestGenerationPromptMentionsArtifactsAndCatalog(t *testing.T) {
	cat := []phaseflow.CatalogAgent{
		{Name: "coder", DefaultModel: "strong", Description: "writes code"},
		{Name: "reviewer", DefaultModel: "strong", Description: "reviews code"},
	}
	p := generationPrompt("Widget Factory", cat)
	for _, want := range []string{"SPEC.md", "ROADMAP.md", "assignments.json", "acceptance", "coder (default strong)", "reviewer (default strong)", "Widget Factory"} {
		if !strings.Contains(p, want) {
			t.Errorf("generation prompt missing %q", want)
		}
	}
}

func TestProjectDialogText(t *testing.T) {
	if !strings.Contains(projectDialogText(projAwaitName, ""), "name") {
		t.Error("await-name dialog should ask for a name")
	}
	if !strings.Contains(projectDialogText(projReview, "Demo"), "approve") {
		t.Error("review dialog should mention approve")
	}
	if projectDialogText(projNone, "") != "" {
		t.Error("no dialog text when not in a project flow")
	}
}

func TestProjectReviewApproveWritesMarker(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		m := model{proj: projReview, projName: "Demo"}
		nm, _, handled := m.handleProjectInput("y")
		if !handled {
			t.Fatal("review input should be handled")
		}
		if nm.proj != projNone {
			t.Errorf("proj should reset to none after approval, got %v", nm.proj)
		}
		if !phaseflow.New(dir).Approved() {
			t.Error("approval marker should be written")
		}
	})
}

func TestProjectReviewCancel(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		m := model{proj: projReview, projName: "Demo"}
		nm, _, handled := m.handleProjectInput("cancel")
		if !handled || nm.proj != projNone {
			t.Errorf("cancel should end the flow: handled=%v proj=%v", handled, nm.proj)
		}
		if phaseflow.New(dir).Approved() {
			t.Error("cancel must not approve")
		}
	})
}
