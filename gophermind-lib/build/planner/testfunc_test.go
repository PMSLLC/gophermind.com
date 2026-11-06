package planner

import (
	"strings"
	"testing"
)

func TestIsTestFunction(t *testing.T) {
	cases := []struct {
		name, sig, file string
		want            bool
	}{
		{"test", "func TestRunCLI(t *testing.T)", "internal/cli/run_test.go", true},
		{"test no file", "func TestRunCLI(t *testing.T)", "", true},
		{"bench", "func BenchmarkGreeting(b *testing.B)", "", true},
		{"main", "func TestMain(m *testing.M)", "", true},
		{"fuzz", "func FuzzRun(f *testing.F)", "", true},
		{"run", "func Run(args []string, stdout, stderr io.Writer) int", "internal/cli/run.go", false},
		{"test file", "func Greeting(name string) string", "internal/greet/greet_test.go", true},
		{"helper", "func Helper(t string)", "", false},
		{"result only", "func New() *testing.T", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := map[string]any{"signature": c.sig, "file": c.file}
			if got := isTestFunction(f); got != c.want {
				t.Fatalf("isTestFunction(%q, %q) = %v, want %v", c.sig, c.file, got, c.want)
			}
		})
	}
}

func TestDropTestFunctionsKeepsOrder(t *testing.T) {
	fns := []any{
		map[string]any{"id": "fn-a", "signature": "func A() int"},
		map[string]any{"id": "fn-t", "signature": "func TestA(t *testing.T)"},
		map[string]any{"id": "fn-b", "signature": "func B() int"},
	}
	kept, dropped := dropTestFunctions(fns)
	if len(kept) != 2 || kept[0].(map[string]any)["id"] != "fn-a" || kept[1].(map[string]any)["id"] != "fn-b" {
		t.Fatalf("kept = %v", kept)
	}
	if len(dropped) != 1 || dropped[0] != "fn-t (func TestA(t *testing.T))" {
		t.Fatalf("dropped = %v", dropped)
	}
}

func TestDropTestFunctionsText(t *testing.T) {
	in := `{"functions":[{"id":"fn-a","signature":"func A()"},{"id":"fn-t","signature":"func TestA(t *testing.T)","file":"a_test.go"}],"x":1}`
	out, dropped := dropTestFunctionsText(in)
	if len(dropped) != 1 || strings.Contains(out, "fn-t") || !strings.Contains(out, "fn-a") || !strings.Contains(out, `"x":1`) {
		t.Fatalf("out = %s dropped = %v", out, dropped)
	}
	if got := testDroppedText("coverage_fill", dropped); got != "test_function_dropped: coverage_fill: fn-t (func TestA(t *testing.T))" {
		t.Fatalf("warning = %q", got)
	}
	if same, d := dropTestFunctionsText("not json"); same != "not json" || d != nil {
		t.Fatal("unparsable reply changed")
	}
}
