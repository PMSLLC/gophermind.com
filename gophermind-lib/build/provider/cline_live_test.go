//go:build !windows

package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClineLive runs the real Cline CLI against a tiny module. It spends a real
// subscription, so it only runs with GOPHERMIND_CLINE_LIVE=1.
func TestClineLive(t *testing.T) {
	if os.Getenv("GOPHERMIND_CLINE_LIVE") != "1" {
		t.Skip("set GOPHERMIND_CLINE_LIVE=1 to run the live Cline test")
	}
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	mod := t.TempDir()
	writeFiles(t, mod, map[string]string{
		"go.mod":      "module example.com/leaf\n\ngo 1.25\n",
		"contract.go": "package leaf\n\ntype Stat struct {\n\tCount         int\n\tMin, Max, Mean float64\n}\n",
		"summarize_test.go": `package leaf

import "testing"

func TestSummarize(t *testing.T) {
	got, err := Summarize([]float64{3, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Stat{Count: 3, Min: 1, Max: 3, Mean: 2}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if _, err := Summarize(nil); err == nil {
		t.Fatal("want an error for an empty slice")
	}
}
`,
	})
	cl := NewCline(ClineConfig{
		Name:          "cline",
		Binary:        env("GOPHERMIND_CLINE_BIN", "cline"),
		ClineProvider: env("GOPHERMIND_CLINE_PROVIDER", "cline-pass"),
		Timeout:       5 * time.Minute,
	})
	resp, err := cl.Complete(context.Background(), Request{
		Model:      env("GOPHERMIND_CLINE_MODEL", "cline-pass/qwen3.7-max"),
		Messages:   []Message{{Role: RoleUser, Content: "Implement the function Summarize in the package leaf. Contract: func Summarize(xs []float64) (Stat, error). It returns an error for an empty slice. The type Stat is already declared in contract.go: do not redeclare it. The tests in summarize_test.go must pass."}},
		Workdir:    mod,
		TargetFile: "summarize.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	src := strings.TrimSuffix(strings.TrimPrefix(resp.Text, "```go\n"), "```")
	if err := os.WriteFile(filepath.Join(mod, "summarize.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"test", "-count=1", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = mod
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s\n--- source ---\n%s", args[0], err, out, src)
		}
	}
	t.Logf("usage: %+v", resp.Usage)
}
