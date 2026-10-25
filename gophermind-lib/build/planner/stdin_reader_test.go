package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func TestStdinNeedsReader(t *testing.T) {
	cases := []struct {
		name string
		fn   contract.Function
		bad  bool
	}{
		{"path function that reads stdin", contract.Function{ID: "fn-summarize-file", Signature: "func SummarizeFile(path string, maxRows int) ([]ColumnSummary, error)", Doc: "Opens the file at path, or reads standard input when path is \"-\"."}, true},
		{"stdin spelled short", contract.Function{ID: "fn-a", Signature: "func Load(path string) (string, error)", Doc: "Reads stdin when path is -."}, true},
		{"takes an io.Reader", contract.Function{ID: "fn-summarize", Signature: "func Summarize(r io.Reader, maxRows int) ([]ColumnSummary, error)", Doc: "Reads records from standard input or any reader."}, false},
		{"run takes stdin as a parameter", contract.Function{ID: "fn-run", Signature: "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", Doc: "Reads stdin when the file name is -."}, false},
		{"takes a bufio reader", contract.Function{ID: "fn-b", Signature: "func Scan(br *bufio.Reader) error", Doc: "Reads from stdin."}, false},
		{"main is the entry point", contract.Function{ID: "fn-main", Signature: "func main()", Doc: "Calls run with os.Stdin and exits with its code; standard input is read there."}, false},
		{"no mention of stdin", contract.Function{ID: "fn-c", Signature: "func Parse(s string) (int, bool)", Doc: "Parses a base-10 integer."}, false},
	}
	for _, tc := range cases {
		err := stdinNeedsReader(tc.fn)
		if tc.bad && err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
		if !tc.bad && err != nil {
			t.Errorf("%s: refused: %v", tc.name, err)
		}
		if err != nil && (strings.Contains(err.Error(), "SummarizeFile") || strings.Contains(err.Error(), "path")) {
			t.Errorf("%s: error quotes the function: %v", tc.name, err)
		}
	}
}

func TestLocalChecksRefusesStdinWithoutReader(t *testing.T) {
	c := &contract.Contracts{
		BriefID:    "b",
		Components: []contract.Component{{ID: "stats", Package: "internal/stats"}},
		Functions: []contract.Function{{ID: "fn-summarize-file", Package: "internal/stats", File: "internal/stats/s.go", Component: "stats",
			Signature: "func SummarizeFile(path string) error", Doc: "Reads standard input when path is -."}},
	}
	err := localChecks(c, "b")
	if err == nil || !strings.Contains(err.Error(), "io.Reader") {
		t.Fatalf("localChecks = %v, want the reader rule", err)
	}
}
