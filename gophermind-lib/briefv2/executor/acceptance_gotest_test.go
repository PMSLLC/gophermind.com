package executor

import (
	"context"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// The pass rule of a go test acceptance command is the runner's, not the exit
// code alone: a test ran, none failed, every named test passed, none skipped.
func TestAcceptanceGoTestUsesTheRunnersPassRule(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	mk := func(dir, src string) {
		write(t, filepath.Join(rc.o.Repo, dir, "x_test.go"), "package "+dir+"\n\nimport \"testing\"\n\n"+src)
	}
	mk("accept_x", "func TestPass(t *testing.T) {}\n\nfunc TestSkip(t *testing.T) { t.Skip(\"later\") }\n")
	mk("accept_s", "func TestOnlySkip(t *testing.T) { t.Skip(\"later\") }\n")
	mk("accept_f", "func TestFail(t *testing.T) { t.Fatal(\"no\") }\n")
	cases := []struct {
		name, cmd string
		pass      bool
	}{
		{"a named test passes", `go test ./accept_x -run '^TestPass$'`, true},
		{"a package with a pass and a skip", `go test ./accept_x`, true},
		{"a named test that skips", `go test ./accept_x -run '^TestSkip$'`, false},
		{"one of two named tests skips", `go test ./accept_x -run '^(TestPass|TestSkip)$'`, false},
		{"a named test that does not exist", `go test ./accept_x -run '^TestNoSuch$'`, false},
		{"every test skips", `go test ./accept_s`, false},
		{"a failing test", `go test ./accept_f`, false},
		{"unset first", `unset GREETER_TOKEN; go test ./accept_x -run '^TestPass$'`, true},
		{"flags after the package", `go test ./accept_x -count=1 -v -run '^TestPass$'`, true},
		{"not a plain go test falls back to the exit status", `go test ./accept_x -run '^TestNoSuch$' && echo done`, true},
	}
	for _, c := range cases {
		file, failed, err := rc.acceptanceRun(context.Background(), oneBullets(c.cmd), 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		cp := file.Bullets[0].Commands[0]
		if cp.Passed != c.pass {
			t.Errorf("%s: passed = %v (exit %d), want %v", c.name, cp.Passed, cp.ExitCode, c.pass)
		}
		if _, bad := failed["A1"]; bad == c.pass {
			t.Errorf("%s: failed = %v", c.name, failed)
		}
	}
	// the no-test case exits 0 and is still a failure: that is the point
	file, _, _ := rc.acceptanceRun(context.Background(), oneBullets(`go test ./accept_x -run '^TestNoSuch$'`), 0)
	if cp := file.Bullets[0].Commands[0]; cp.ExitCode != 0 || cp.Passed {
		t.Errorf("no tests ran: exit %d passed %v", cp.ExitCode, cp.Passed)
	}
}

var _ = planner.AcceptanceDir
