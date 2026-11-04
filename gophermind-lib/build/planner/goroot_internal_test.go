package planner

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// A binary built with -trimpath has an empty runtime.GOROOT() unless GOROOT
// is in its environment, which the launchd service does not set. The root
// then comes from the go tool, so the standard library is still recognised
// and dropUnusedImports keeps working in the installed server.
func TestGoRootFallsBackToTheGoTool(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool")
	}
	out, err := exec.Command(goBin, "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(out))
	if got := goRootFrom("", exec.LookPath); got != want {
		t.Errorf("goRootFrom(\"\") = %q, want %q", got, want)
	}
	if got := goRootFrom("/from/runtime", exec.LookPath); got != "/from/runtime" {
		t.Errorf("runtime root not preferred: %q", got)
	}
	noGo := func(string) (string, error) { return "", errors.New("not found") }
	if got := goRootFrom("", noGo); got != "" {
		t.Errorf("without a go tool got %q, want empty", got)
	}
}
