//go:build !windows

package executor

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/router"
)

// clineRig wires the rig's real router over Cline providers whose binary is a
// fake shell script. Call n (1-based) of the script writes bodies[n-1], or the
// last body when n is past the end, into the scratch copy's target file, and
// also drops stray.txt there. It records, per call, the -c directory, a listing
// of it and the prompt, under the returned logs directory.
func clineRig(t *testing.T, g *rig, l *Leaf, bodies []string) (*runCtx, string) {
	t.Helper()
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	settings := filepath.Join(dir, "settings")
	for _, d := range []string{logs, settings} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(settings, "providers.json"), `{}`)
	var cases strings.Builder
	for i, b := range bodies {
		src := filepath.Join(dir, fmt.Sprintf("body%d.go", i+1))
		write(t, src, b)
		fmt.Fprintf(&cases, "%d) cp %q \"$cwd/%s\" ;;\n", i+1, src, l.File)
	}
	okJSON, err := filepath.Abs("../provider/testdata/cline_run_ok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fakecline")
	write(t, bin, fmt.Sprintf(`#!/bin/sh
L=%q
n=$(ls "$L" | wc -l | tr -d ' '); n=$((n+1)); mkdir "$L/$n"
cwd=; prev=; p=
for a in "$@"; do
  [ "$prev" = "-c" ] && cwd=$a
  prev=$a; p=$a
done
printf '%%s\n' "$cwd" > "$L/$n/cwd"
printf '%%s' "$p" > "$L/$n/prompt"
(cd "$cwd" && find . | sort) > "$L/$n/tree"
case $n in
%s*) cp %q "$cwd/%s" ;;
esac
echo stray > "$cwd/stray.txt"
cat %q
`, logs, cases.String(), filepath.Join(dir, fmt.Sprintf("body%d.go", len(bodies))), l.File, okJSON))
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	g.wire(Script{})
	provs := map[string]provider.Provider{}
	for _, pc := range g.cfg.Providers {
		var infos []provider.ModelInfo
		for _, m := range pc.Models {
			infos = append(infos, provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens})
		}
		provs[pc.Name] = provider.NewCline(provider.ClineConfig{
			Name: pc.Name, Binary: bin, ClineProvider: "cline-pass", SettingsDir: settings,
			Timeout: time.Minute, Models: infos,
		})
	}
	instant := func(context.Context, time.Duration) error { return nil }
	g.router = router.New(g.cfg, provs, g.led, g.sink, router.WithSleep(instant))
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
	for _, x := range g.plan.Leaves {
		if err := g.board.SetStatus(context.Background(), g.id, x.ID, blackboard.StatusPending, blackboard.StatusReady); err != nil {
			t.Fatal(err)
		}
	}
	return rc, logs
}

func readLog(t *testing.T, logs, n, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(logs, n, name))
	if err != nil {
		t.Fatalf("the fake cline did not record call %s %s: %v", n, name, err)
	}
	return string(b)
}

// assertScratchOnly checks that a Cline call worked in a copy, never in the
// leaf's worktree, and that the copy held the test file and no .git.
func assertScratchOnly(t *testing.T, g *rig, l *Leaf, logs, n string) {
	t.Helper()
	cwd := strings.TrimSpace(readLog(t, logs, n, "cwd"))
	if cwd == "" || cwd == g.repo || strings.HasPrefix(cwd, g.repo+string(filepath.Separator)) {
		t.Errorf("call %s ran in %q, want a scratch copy outside %s", n, cwd, g.repo)
	}
	tree := readLog(t, logs, n, "tree")
	if !strings.Contains(tree, "./"+l.TestFile+"\n") {
		t.Errorf("call %s: the scratch copy has no %s:\n%s", n, l.TestFile, tree)
	}
	if strings.Contains(tree, ".git\n") || strings.Contains(tree, ".git/") {
		t.Errorf("call %s: the scratch copy holds a .git:\n%s", n, tree)
	}
}

// assertScratchOmits checks that the copy holds no .gophermind entry and not
// this leaf's own stub, while the other leaves' stubs and repo files remain.
func assertScratchOmits(t *testing.T, g *rig, l *Leaf, logs, n string) {
	t.Helper()
	tree := readLog(t, logs, n, "tree")
	if strings.Contains(tree, ".gophermind") {
		t.Errorf("call %s: the scratch copy holds a .gophermind entry:\n%s", n, tree)
	}
	if strings.Contains(tree, "/"+path.Base(l.StubFile)+"\n") {
		t.Errorf("call %s: the scratch copy holds the leaf's own stub %s:\n%s", n, l.StubFile, tree)
	}
	if !strings.Contains(tree, "./go.mod\n") {
		t.Errorf("call %s: the scratch copy lacks go.mod:\n%s", n, tree)
	}
}

// assertWorktreeUntouched checks that nothing the script wrote outside the
// executor's own commit reached the real worktree.
func assertWorktreeUntouched(t *testing.T, g *rig) {
	t.Helper()
	if fileExists(filepath.Join(g.repo, "stray.txt")) {
		t.Error("stray.txt, written by the script in the scratch copy, is in the real worktree")
	}
	if st := strings.TrimSpace(g.gitCmd("status", "--porcelain")); st != "" {
		t.Errorf("the worktree is dirty after the leaf:\n%s", st)
	}
}

func TestClineProviderBuildsALeafThroughTheRouter(t *testing.T) {
	g := newRig(t)
	l := g.plan.Leaf(leafID)
	rc, logs := clineRig(t, g, l, []string{good(leafID)})
	write(t, filepath.Join(g.repo, ".gophermind", "_state", "x"), "run state\n")

	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified || out.Interrupted {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	assertScratchOnly(t, g, l, logs, "1")
	assertScratchOmits(t, g, l, logs, "1")
	if got := verdictsOf(t, g.board, leafID); got != "pass" {
		t.Fatalf("verdicts = %s, want pass", got)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d, want 1", n)
	}
	want, err := os.ReadFile(g.realPath(l))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(want), "func Greet(") {
		t.Errorf("the committed file does not hold the script's source:\n%s", want)
	}
	assertWorktreeUntouched(t, g)
}

func TestClineProviderContractViolationIsAFailedAttempt(t *testing.T) {
	g := newRig(t)
	l := g.plan.Leaf(leafID)
	bad := strings.Replace(good(leafID), `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)", 1)
	bad = strings.Replace(bad, "name = strings.TrimSpace(name)", "_ = exec.Command\n\tname = strings.TrimSpace(name)", 1)
	rc, logs := clineRig(t, g, l, []string{bad, good(leafID)})

	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified on the second attempt", out)
	}
	assertScratchOnly(t, g, l, logs, "1")
	assertScratchOnly(t, g, l, logs, "2")
	if got := verdictsOf(t, g.board, leafID); got != "fail,pass" {
		t.Fatalf("verdicts = %s, want fail,pass", got)
	}
	first, second := readLog(t, logs, "1", "prompt"), readLog(t, logs, "2", "prompt")
	if strings.Contains(first, "Allowed imports:") {
		t.Error("the first prompt already carries the import note")
	}
	if !strings.Contains(second, "Allowed imports:") {
		t.Errorf("the second prompt carries no import-policy failure note:\n%s", second)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d, want 1 (the failed attempt commits nothing)", n)
	}
	assertWorktreeUntouched(t, g)
}
