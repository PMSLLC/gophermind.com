package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestParseProjectCommandNameOnly pins the resume grammar: a name with no
// trailing file path is not mistaken for one.
func TestParseProjectCommandNameOnly(t *testing.T) {
	name, brief, err := parseProjectCommand("/project My Cool App")
	if name != "My Cool App" || brief != "" || err != nil {
		t.Errorf("got (%q,%q,%v), want (%q,%q,nil)", name, brief, err, "My Cool App", "")
	}
}

// TestParseProjectCommandNoName covers the bare "/project" case that asks for
// a name interactively.
func TestParseProjectCommandNoName(t *testing.T) {
	name, brief, err := parseProjectCommand("/project")
	if name != "" || brief != "" || err != nil {
		t.Errorf("got (%q,%q,%v), want empty name and brief", name, brief, err)
	}
}

// TestParseProjectCommandWithBrief: a trailing token that is a real file is
// the brief, and everything before it is the project name.
func TestParseProjectCommandWithBrief(t *testing.T) {
	brief := writeBrief(t, "a CLI tool")
	name, gotBrief, err := parseProjectCommand("/project My Cool App " + brief)
	if name != "My Cool App" || gotBrief != brief || err != nil {
		t.Errorf("got (%q,%q,%v), want (%q,%q,nil)", name, gotBrief, err, "My Cool App", brief)
	}
}

// TestParseProjectCommandSingleTokenNotMistakenForBrief guards the two-field
// case: "/project <path>" alone has no name before the path, so per the
// design it is treated as a (probably odd-looking) name, not a nameless
// brief. /project always requires a name.
func TestParseProjectCommandSingleTokenNotMistakenForBrief(t *testing.T) {
	brief := writeBrief(t, "a CLI tool")
	name, gotBrief, err := parseProjectCommand("/project " + brief)
	if name != brief || gotBrief != "" || err != nil {
		t.Errorf("got (%q,%q,%v), want the lone token treated as the name", name, gotBrief, err)
	}
}

// TestParseProjectCommandMissingBriefIsAnError is the M6 change: a trailing
// token that was clearly meant to be a brief path, but is not a file, used to
// become part of the project name, so a typo produced a project named after
// it and a plan built from no brief at all.
func TestParseProjectCommandMissingBriefIsAnError(t *testing.T) {
	for _, bad := range []string{"/no/such/file.md", "brief.md", "notes.txt"} {
		name, gotBrief, err := parseProjectCommand("/project Widget " + bad)
		if err == nil {
			t.Errorf("parseProjectCommand with %q = (%q,%q), want an error", bad, name, gotBrief)
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("the error does not name the path: %v", err)
		}
	}
}

// TestParseProjectCommandPlainWordsAreStillAName: only something path-shaped
// is read as a brief, so an ordinary multi-word name still works.
func TestParseProjectCommandPlainWordsAreStillAName(t *testing.T) {
	name, brief, err := parseProjectCommand("/project Widget Factory Mark II")
	if name != "Widget Factory Mark II" || brief != "" || err != nil {
		t.Errorf("got (%q,%q,%v)", name, brief, err)
	}
}

// TestParseProjectCommandDirectoryIsAnError: a directory is not a brief.
func TestParseProjectCommandDirectoryIsAnError(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := parseProjectCommand("/project Widget " + dir); err == nil {
		t.Error("a directory was accepted as a brief")
	}
}

// TestStartProjectWithoutABriefAndWithoutAPlanRefuses: there is nothing to
// plan from, and nothing to resume.
func TestStartProjectWithoutABriefAndWithoutAPlanRefuses(t *testing.T) {
	t.Chdir(t.TempDir())
	m := testModel(t)
	nm, _ := m.startProject("Demo", "")
	if nm.proj != projNone || !strings.Contains(nm.content, "give a brief file") {
		t.Errorf("proj = %v, transcript = %q", nm.proj, nm.content)
	}
}

// TestStartProjectUnreadableBriefRefuses: a path that parsed (it exists) but
// cannot be read must surface, not start a plan from nothing.
func TestStartProjectUnreadableBriefRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := testModel(t)
	nm, _ := m.startProject("Demo", filepath.Join(dir, "missing.md"))
	if nm.proj != projNone || !strings.Contains(nm.content, "reading the brief") {
		t.Errorf("proj = %v, transcript = %q", nm.proj, nm.content)
	}
}

// TestStartProjectEmptyBriefRefuses: an empty file is not a brief.
func TestStartProjectEmptyBriefRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	nm, _ := m.startProject("Demo", brief)
	if nm.proj != projNone || !strings.Contains(nm.content, "is empty") {
		t.Errorf("proj = %v, transcript = %q", nm.proj, nm.content)
	}
}

// TestStartProjectWithoutASessionRefuses: the passes need a model, so with no
// session the flow says so instead of starting a goroutine that cannot work.
func TestStartProjectWithoutASessionRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("# One\nbuild a thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := testModel(t) // no agent and no injected completer
	nm, _ := m.startProject("Demo", brief)
	if nm.proj != projNone || !strings.Contains(nm.content, "no active session") {
		t.Errorf("proj = %v, transcript = %q", nm.proj, nm.content)
	}
}

func TestProjectDialogText(t *testing.T) {
	if !strings.Contains(projectDialogText(projAwaitName, ""), "name") {
		t.Error("await-name dialog should ask for a name")
	}
	if !strings.Contains(projectDialogText(projRunning, "Demo"), "planning") {
		t.Error("running dialog should say the passes are running")
	}
	if !strings.Contains(projectDialogText(projApprove, "Demo"), "approve") {
		t.Error("review dialog should mention approve")
	}
	if projectDialogText(projNone, "") != "" {
		t.Error("no dialog text when not in a project flow")
	}
}

// writeBrief writes a brief file and returns its path.
func writeBrief(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
