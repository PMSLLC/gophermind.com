package planner

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// A constraint that names a tool in backticks is tested by running that tool.
func TestToolsNotInvoked(t *testing.T) {
	c := Requirement{ID: "C5", Kind: ReqConstraint, Text: "`gofmt` reports no formatting changes and `go vet ./...` reports no problems."}
	rt := func(id string, cmds ...string) []RootTest {
		var out []RootTest
		for _, cmd := range cmds {
			out = append(out, RootTest{Requirement: id, Command: cmd})
		}
		return out
	}
	if got := toolsNotInvoked(c, rt("C5", "hello | grep -q x")); !reflect.DeepEqual(got, []string{"gofmt", "go vet"}) {
		t.Errorf("binary test = %q, want [gofmt go vet]", got)
	}
	if got := toolsNotInvoked(c, rt("C5", `test -z "$(gofmt -l .)"`, "go vet ./...")); got != nil {
		t.Errorf("tools run = %q, want nil", got)
	}
	if got := toolsNotInvoked(c, append(rt("C5", "hello | grep -q x"), rt("C6", `test -z "$(gofmt -l .)" && go vet ./...`)...)); !reflect.DeepEqual(got, []string{"gofmt", "go vet"}) {
		t.Errorf("another requirement's test counted: %q", got)
	}
	plain := Requirement{ID: "C1", Kind: ReqConstraint, Text: "Standard library only."}
	if got := toolsNotInvoked(plain, rt("C1", "hello | grep -q x")); got != nil {
		t.Errorf("no backticked tool = %q, want nil", got)
	}
	a := Requirement{ID: "A2", Kind: ReqAcceptance, Text: "`go vet ./...` reports nothing."}
	if got := toolsNotInvoked(a, rt("A2", "hello | grep -q x")); got != nil {
		t.Errorf("acceptance checked = %q, want nil", got)
	}
}

// /bin/sh -n parses without running: process substitution is a syntax error
// there, and a POSIX capture is not.
func TestShellSyntaxRejectsProcessSubstitution(t *testing.T) {
	msg, bad := shellSyntax(`cmp -s "$out" <(printf 'x')`)
	if !bad || !strings.Contains(msg, "syntax error") || strings.Contains(msg, "\n") {
		t.Errorf("shellSyntax = %q, %v; want one line naming a syntax error", msg, bad)
	}
	if msg, bad := shellSyntax(`test "$(cat "$out")" = 'x' && test "$(wc -l <"$out")" -eq 1`); bad {
		t.Errorf("POSIX capture refused: %q", msg)
	}
}

// A negated grep for a token the brief's Architecture uses forbids what the
// brief requires; a token the constraint itself names is forbidden on purpose.
func TestForbidsBriefTokens(t *testing.T) {
	src := []byte("## Architecture\n\n- `cmd/hello` (package `main`): `main` calls `cli.Run` with `os.Args[1:]`.\n\n## Constraints\n\n- No `init` functions.\n")
	c4 := Requirement{ID: "C4", Kind: ReqConstraint, Text: "No logging, no flags, no configuration, no file access, no network access, no environment variables, no `init` functions."}
	rt := func(id, cmd string) []RootTest { return []RootTest{{Requirement: id, Command: cmd}} }
	full := `! grep -rq 'init(' internal/ && ! grep -rq 'os\.Getenv' . && ! grep -rq 'os\.Args' cmd/hello`
	if got := forbidsBriefTokens(c4, rt("C4", full), src); !reflect.DeepEqual(got, []string{"os.Args"}) {
		t.Errorf("forbids = %q, want [os.Args]", got)
	}
	if got := forbidsBriefTokens(c4, rt("C4", `! grep -rq 'init(' internal/ && ! grep -rq 'os\.Getenv' .`), src); got != nil {
		t.Errorf("without os.Args = %q, want nil", got)
	}
	a := Requirement{ID: "A4", Kind: ReqAcceptance, Text: "`hello` prints `Hello, World!`."}
	if got := forbidsBriefTokens(a, rt("A4", full), src); got != nil {
		t.Errorf("acceptance checked = %q, want nil", got)
	}
}

// A span in a sentence that says never, not or no is a forbidden mention, not
// a requirement; a negated grep limited to directories no requiring line names
// does not forbid what the brief requires.
func TestForbidsBriefTokensReadsNegationAndPaths(t *testing.T) {
	src := []byte("## Features\n\n- `Run(args []string, stdout, stderr io.Writer) int` in `internal/cli` handles every successful invocation and every argument-count error. It has exactly these three parameters and never calls `os.Exit`.\n\n" +
		"## Architecture\n\n- `cmd/hello` (package `main`): `main` calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`, and passes the returned integer to `os.Exit`.\n")
	c := Requirement{ID: "C4", Kind: ReqConstraint, Text: "No logging and no `init` functions."}
	cases := []struct {
		cmd  string
		want []string
	}{
		{`! grep -rq 'os\.Exit' internal/`, nil},
		{`! grep -rq 'os\.Exit' .`, []string{"os.Exit"}},
		{`! grep -rq 'os\.Exit' cmd/hello`, []string{"os.Exit"}},
		{`! grep -rq 'os\.Args' cmd/hello`, []string{"os.Args"}},
		{`! grep -rq 'os\.Args' internal/`, nil},
		{`! grep -rq 'init(' internal/`, nil},
	}
	for _, k := range cases {
		got := forbidsBriefTokens(c, []RootTest{{Requirement: "C4", Command: k.cmd}}, src)
		if !reflect.DeepEqual(got, k.want) {
			t.Errorf("%s = %q, want %q", k.cmd, got, k.want)
		}
	}
}

// Every negated grep the coverage prompt teaches passes the brief-token check
// against the hello fixture brief.
func TestCoveragePromptNegatedGrepsPassTheBriefTokenCheck(t *testing.T) {
	prompt, err := os.ReadFile("prompts/coverage.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("testdata/hello/brief.md")
	if err != nil {
		t.Fatal(err)
	}
	c := Requirement{ID: "C9", Kind: ReqConstraint, Text: "A forbidden construct."}
	n := 0
	for _, m := range backtickRE.FindAllStringSubmatch(string(prompt), -1) {
		if !strings.HasPrefix(m[1], "! grep") {
			continue
		}
		n++
		if got := forbidsBriefTokens(c, []RootTest{{Requirement: "C9", Command: m[1]}}, src); got != nil {
			t.Errorf("prompt example %q forbids %q", m[1], got)
		}
	}
	if n < 2 {
		t.Errorf("negated grep examples checked = %d, want at least 2", n)
	}
}
