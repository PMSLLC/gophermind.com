package planner

import (
	"strings"
	"testing"
)

var runObligations = []TestObligation{
	{ID: "T1", Target: "fn-run", Line: 50, Text: "`Run(nil, stdout, stderr)` returns 0 and writes `Hello, World!` and a newline to stdout; stderr is empty."},
	{ID: "T2", Target: "fn-run", Line: 51, Text: "`Run([]string{}, stdout, stderr)` returns 0 and writes `Hello, World!` and a newline to stdout; stderr is empty."},
}

const runNilOnly = `package cli_test

import (
	"bytes"
	"testing"

	"hello/internal/cli"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantOut string
		wantErr string
		code    int
	}{
		{name: "no args", args: nil, wantOut: "Hello, World!\n", code: 0},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		if got := cli.Run(tc.args, &stdout, &stderr); got != tc.code || stdout.String() != tc.wantOut || stderr.String() != tc.wantErr {
			t.Errorf("%s: got %d %q %q", tc.name, got, stdout.String(), stderr.String())
		}
	}
}
`

// johnsTable is the table from John's note of 2026-11-05: nil, an empty
// slice, a name, an empty name, two arguments, separate buffers per case.
var johnsTable = strings.Replace(runNilOnly, `		{name: "no args", args: nil, wantOut: "Hello, World!\n", code: 0},
`, `		{name: "nil args", args: nil, wantOut: "Hello, World!\n", code: 0},
		{name: "empty args", args: []string{}, wantOut: "Hello, World!\n", code: 0},
		{name: "one name", args: []string{"Ada"}, wantOut: "Hello, Ada!\n", code: 0},
		{name: "empty name", args: []string{""}, wantOut: "Hello, World!\n", code: 0},
		{name: "two args", args: []string{"a", "b"}, wantErr: "usage: hello [NAME]\n", code: 2},
`, 1)

func TestObligationCheckRefusesAnUncitedObligation(t *testing.T) {
	err := checkObligations(runObligations, map[string]bool{"T1": true}, runNilOnly)
	if err == nil || !strings.Contains(err.Error(), "no test case covers obligation T2") {
		t.Fatalf("err = %v, want no test case covers obligation T2", err)
	}
}

func TestObligationCheckRefusesAMissingArgument(t *testing.T) {
	err := checkObligations(runObligations, map[string]bool{"T1": true, "T2": true}, runNilOnly)
	if err == nil || !strings.Contains(err.Error(), "test file does not cover obligation T2: the argument []string{} is absent") {
		t.Fatalf("err = %v, want the argument []string{} is absent", err)
	}
}

func TestObligationCheckPassesJohnsTable(t *testing.T) {
	if johnsTable == runNilOnly {
		t.Fatal("the table was not built")
	}
	if err := checkObligations(runObligations, map[string]bool{"T1": true, "T2": true}, johnsTable); err != nil {
		t.Fatal(err)
	}
}

func TestObligationCheckRefusesMissingExpectedText(t *testing.T) {
	obs := []TestObligation{{ID: "T1", Target: "fn-greeting", Line: 48, Text: "`Greeting(\"\")` returns `Hello, World!`."}}
	src := `package greet

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting(""); got != "Hello, world" {
		t.Errorf("got %q", got)
	}
}
`
	err := checkObligations(obs, map[string]bool{"T1": true}, src)
	if err == nil || !strings.Contains(err.Error(), "test file does not cover obligation T1: expected text Hello, World! is absent") {
		t.Fatalf("err = %v, want expected text Hello, World! is absent", err)
	}
	if err := checkObligations(obs, map[string]bool{"T1": true}, strings.Replace(src, "Hello, world", "Hello, World!", 1)); err != nil {
		t.Fatal(err)
	}
}

func TestObligationLiteralArguments(t *testing.T) {
	for span, want := range map[string][]string{
		"Run(nil, stdout, stderr)":                     {"nil"},
		`Run([]string{"a", "b"}, stdout, stderr)`:      {`[]string{"a", "b"}`},
		`Greeting("a, b")`:                             {`"a, b"`},
		`Make(s, 0, -1, Options{Max: 2}, &x, f(1, 2))`: {"0", "-1", "Options{Max: 2}"},
		`x.Do(map[string]int{"a": 1}, t)`:              {`map[string]int{"a": 1}`},
	} {
		got := literalArgs(span)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("literalArgs(%s) = %q, want %q", span, got, want)
		}
	}
}
