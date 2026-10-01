package executor

import (
	"strings"
	"testing"
)

// Options.EnvNotes are lines the caller wants in the report's environment
// section (the CLI records which provider base URL host answered).
func TestOptionsEnvNotesReachTheReportEnvironment(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), func(o *Options) {
		o.EnvNotes = []string{"provider a: base url host 10.8.0.6 answered"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if env := strings.Join(rep.Environment, "|"); !strings.Contains(env, "provider a: base url host 10.8.0.6 answered") || !strings.Contains(env, "sandbox: off") {
		t.Errorf("environment = %q", rep.Environment)
	}
	if got := readReportFile(t, g); strings.Join(got.Environment, "|") != strings.Join(rep.Environment, "|") {
		t.Errorf("the written report differs: %q", got.Environment)
	}
}
