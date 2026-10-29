package planner

import (
	"slices"
	"testing"
)

const slugifyArch = "- `cmd/slugify` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdin`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.\n" +
	"- `internal/slug`: `Make(s string, max int) string` is pure; a `max` of 0 or less means no limit. No printing, no files.\n" +
	"- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` parses the arguments, reads the whole input from the file or from `stdin`, calls `slug.Make` and prints the result. It never calls `os.Exit`.\n"

func TestArchitectureUses(t *testing.T) {
	doc := map[string]any{
		"module": "example.com/slugify",
		"functions": []any{
			map[string]any{"id": "fn-make", "package": "internal/slug", "signature": "func Make(s string, max int) string", "uses": []any{}},
			map[string]any{"id": "fn-parse-args", "package": "internal/cli", "signature": "func parseArgs(args []string) (int, string, error)", "uses": []any{}},
			map[string]any{"id": "fn-run", "package": "internal/cli", "signature": "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", "uses": []any{"fn-parse-args"}},
			map[string]any{"id": "fn-main", "package": "cmd/slugify", "signature": "func main()", "uses": []any{"fn-run"}},
		},
	}
	added := architectureUses(doc, slugifyArch)
	if !slices.Equal(added, []string{"fn-run"}) {
		t.Fatalf("added = %v, want [fn-run]", added)
	}
	run := objects(doc["functions"])[2]
	if u := strList(run["uses"]); !slices.Equal(u, []string{"fn-parse-args", "fn-make"}) {
		t.Errorf("fn-run uses = %v", u)
	}
	if again := architectureUses(doc, slugifyArch); len(again) != 0 {
		t.Errorf("not idempotent: %v", again)
	}
	// parseArgs is not named in the bullet, so it gains nothing.
	if u := strList(objects(doc["functions"])[1]["uses"]); len(u) != 0 {
		t.Errorf("fn-parse-args uses = %v", u)
	}
}
