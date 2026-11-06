package executor

import (
	"context"
	"strings"
	"testing"
)

// A failed bullet's failure text names the requirement, the command, the
// exit code and the output, or says the command printed nothing: a silent
// root test still reaches the model with a reason.
func TestAcceptanceBulletFailureText(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	_, failed, err := rc.acceptanceRun(context.Background(), oneBullets(`exit 1`, `printf 'first out\nsecond out\n'; exit 2`), 0)
	if err != nil {
		t.Fatal(err)
	}
	silent, loud := failed["A1"], failed["A2"]
	want := []string{"acceptance A1: bullet A1", "command: root test A1 #1 (tA1)", "exit code 1", "(the command printed nothing)"}
	if got := silent.Failure.Lines; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("silent failure lines = %q, want %q", got, want)
	}
	if silent.Failure.Names != nil {
		t.Errorf("names = %q, want none", silent.Failure.Names)
	}
	want = []string{"acceptance A2: bullet A2", "command: root test A2 #1 (tA2)", "exit code 2", "first out", "second out"}
	if got := loud.Failure.Lines; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("failure lines = %q, want %q", got, want)
	}
}

// A bullet with no command (unreachable through mapAcceptance) has the same
// shape and says no command ran.
func TestBulletWithoutCommandFailureText(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	pb := oneBullets(`true`).acceptance[0]
	pb.tests = nil
	_, fb, err := rc.runBullet(context.Background(), pb)
	if err != nil || fb == nil {
		t.Fatalf("runBullet = %+v, %v", fb, err)
	}
	want := []string{"acceptance A1: bullet A1", "exit code none (no command ran)", "(the command printed nothing)"}
	if got := fb.Failure.Lines; strings.Join(got, "\n") != strings.Join(want, "\n") || fb.ExitCode != -1 {
		t.Errorf("failure lines = %q exit %d, want %q and -1", got, fb.ExitCode, want)
	}
}
