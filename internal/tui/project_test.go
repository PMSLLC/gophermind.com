package tui

import (
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
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

func TestInterviewPromptCarriesSentinel(t *testing.T) {
	p := interviewPrompt("Demo")
	if !strings.Contains(p, specReadySentinel) || !strings.Contains(p, "Demo") {
		t.Errorf("interview prompt should name the project and the sentinel: %q", p)
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
