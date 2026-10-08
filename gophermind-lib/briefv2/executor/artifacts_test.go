package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
)

// buildBreakingReply is a reply that does not compile: the compiler output
// names the leaf's file.
func buildBreakingReply() string {
	return "package greet\n\nfunc Greet(name string) (string, error) {\n\tvar n int = \"not a number\"\n\treturn \"\", nil\n}\n"
}

func TestExecutorSavesScrubbedAttemptArtifacts(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(buildBreakingReply()), reply(good(leafID))}}, false)
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	dir := filepath.Join(g.runDir, "attempts", leafID)
	for _, n := range []string{"1", "2"} {
		for _, f := range []string{"reply.go", "reply.txt", "attempt.json"} {
			if _, err := os.Stat(filepath.Join(dir, n, f)); err != nil {
				t.Errorf("attempt %s: %s missing: %v", n, f, err)
			}
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "1", "check-output.txt"))
	if err != nil || !strings.Contains(string(got), "greet.go") {
		t.Errorf("the failed attempt must keep its compiler output, got %q, %v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "2", "check-output.txt")); err == nil && strings.Contains(string(b), canarySecret) {
		t.Error("the canary secret reached check-output.txt")
	}
}

func TestExecutorArtifactsOffWritesNothing(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.cfg.Executor.Artifacts = "off"
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, false)
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "attempts")); !os.IsNotExist(err) {
		t.Errorf("artifacts: off must not create attempts/: %v", err)
	}
}

func TestExecutorArtifactsDoNotLeakACanarySecret(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	// A failing reply that writes the declared secret into a comment, then a good one.
	echo := "package greet\n\n// token: " + canarySecret + "\nfunc Greet(name string) (string, error) {\n\tvar n int = \"x\"\n\treturn \"\", nil\n}\n"
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(echo), reply(good(leafID))}}, false)
	runOne(t, rc, leafID)
	if _, err := os.Stat(filepath.Join(g.runDir, "attempts", leafID, "1")); err != nil {
		t.Fatalf("the echoing attempt was not saved, so the test proves nothing: %v", err)
	}
	filepath.WalkDir(g.runDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), canarySecret) {
			t.Errorf("%s holds the canary secret", p)
		}
		return nil
	})
}

func TestRuntimeWritesInsideTheRunFolderAreNotStrayFiles(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	before, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join("attempts", leafID, "1"), "greet"} {
		if err := os.MkdirAll(filepath.Join(g.runDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(g.runDir, "attempts", leafID, "1", "reply.txt"), "x")
	write(t, filepath.Join(g.runDir, "greet", leafID+".runtime.json"), "{}")
	stray, err := before.Stray(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if len(stray) != 0 {
		t.Errorf("run folder writes were reported as stray: %v", stray)
	}
}

// The runner cuts check output at output_cap_bytes before the scrubbers see
// it, so a secret straddling the cap would leave an unscrubbed prefix. The
// saved check output drops the cut line.
func TestCheckOutputCutThroughASecretLeaksNoPrefix(t *testing.T) {
	// The source holds the secret in two halves, so the source scan passes it;
	// the panic message of the failing test prints it whole.
	half := strings.Index(canarySecret, "-") + 1
	boomOf := func(secret string) string {
		return "package greet\n\nfunc Greet(name string) (string, error) {\n\tpanic(\"" + secret[:half] + "\" + \"" + secret[half:] + "\")\n}\n"
	}
	run := func(boom string, capBytes int) string {
		g := newRig(t)
		g.cfg.Executor.OutputCapBytes = capBytes
		rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(boom), reply(good(leafID))}}, false)
		runOne(t, rc, leafID)
		b, err := os.ReadFile(filepath.Join(g.runDir, "attempts", leafID, "1", "check-output.txt"))
		if err != nil {
			t.Fatalf("cap %d: attempt 1 has no check output: %v", capBytes, err)
		}
		return string(b)
	}
	// A reference run with a look-alike that is not a secret shows where the
	// panic message sits in the output.
	full := run(boomOf(canarySecret[:len(canarySecret)-1]+"x"), 65536)
	at := strings.Index(full, "panic:")
	if at < 0 {
		t.Fatalf("the panic is not in the check output, the test proves nothing: %q", full)
	}
	// The panic message starts 7 bytes after "panic:" begins; cut the output
	// 8 bytes into the secret.
	capBytes := at + len("panic: ") + 8
	got := run(boomOf(canarySecret), capBytes)
	if strings.Contains(got, canarySecret[:8]) {
		t.Errorf("cap %d: the saved check output holds a prefix of the canary secret: %q", capBytes, got)
	}
}
