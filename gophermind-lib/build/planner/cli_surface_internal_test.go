package planner

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func csvstatBrief(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/csvstat-cli/brief.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCLISurfaceOfTheCsvstatBrief(t *testing.T) {
	s := ExtractCLISurface(csvstatBrief(t))
	if !reflect.DeepEqual(s.Flags, []string{"--format"}) {
		t.Errorf("Flags = %v, want only --format (go build/vet/test and printf are checks)", s.Flags)
	}
	if v := s.Values["--format"]; !reflect.DeepEqual(v, []string{"text", "json"}) {
		t.Errorf("--format values = %v", v)
	}
	if !s.Stdin {
		t.Error("`-` as standard input not found")
	}
	if !reflect.DeepEqual(s.ExitCodes, []string{"0", "1", "2"}) {
		t.Errorf("ExitCodes = %v", s.ExitCodes)
	}
	if !strings.Contains(s.Text(), "Reading `-` as the file name reads standard input") {
		t.Errorf("the brief's own line is not quoted:\n%s", s.Text())
	}
}

func TestCLISurfaceOfALibraryBriefIsEmpty(t *testing.T) {
	if s := ExtractCLISurface("## Features\n\n- `Greet(name)` returns `Hello`. Run `go test -race ./...`.\n"); !s.Empty() {
		t.Errorf("surface = %+v", s)
	}
}

func TestCLISurfaceMissingNamesWhatTheTextLeavesOut(t *testing.T) {
	s := ExtractCLISurface(csvstatBrief(t))
	bad := "Parses args to determine input source and output format."
	miss := strings.Join(s.Missing(bad), "|")
	for _, want := range []string{"flag --format", "standard input", "exit code 2"} {
		if !strings.Contains(miss, want) {
			t.Errorf("Missing(bad) = %q lacks %q", miss, want)
		}
	}
	good := "Parses --format text|json (default text) and a file name; - reads stdin. Exits 0 on success, 1 on any error and exit code 2 for an invalid --format."
	if m := s.Missing(good); len(m) != 0 {
		t.Errorf("Missing(good) = %v", m)
	}
}

func TestCLISurfaceRejectsAGuessThatContradictsTheBrief(t *testing.T) {
	s := ExtractCLISurface(csvstatBrief(t))
	if !s.Touches("Should csvstat read from stdin?", "No, stdin is unsupported; use the -f flag.") {
		t.Error("a default about stdin and -f is not caught")
	}
	if s.Touches("How are ties in distinct counts ordered?", "Insertion order.") {
		t.Error("an unrelated question was caught")
	}
}
