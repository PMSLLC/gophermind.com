package planner

import (
	"strings"
	"testing"
)

const exercisedSrc = `package cli_test

import (
	"testing"

	"example.com/linkcheck/cli"
)

func TestRun(t *testing.T) {
	if got := cli.Run([]string{"x"}, nil, nil, nil); got != 2 {
		t.Fatalf("Run = %d, want 2", got)
	}
}
`

func TestCheckTestExercisesAcceptsACallAndAnAssertion(t *testing.T) {
	if err := checkTestExercises(exercisedSrc, "Run", true); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesRefusesAFileThatNeverCallsTheFunction(t *testing.T) {
	src := strings.Replace(exercisedSrc, `got := cli.Run([]string{"x"}, nil, nil, nil)`, `got := 2`, 1)
	src = strings.Replace(src, "\n\t\"example.com/linkcheck/cli\"\n", "\n", 1)
	err := checkTestExercises(src, "Run", true)
	if err == nil || !strings.Contains(err.Error(), "never calls Run") {
		t.Fatalf("err = %v, want never calls Run", err)
	}
}

func TestCheckTestExercisesRefusesAFileThatCannotFail(t *testing.T) {
	src := strings.Replace(exercisedSrc, `t.Fatalf("Run = %d, want 2", got)`, `t.Log(got)`, 1)
	err := checkTestExercises(src, "Run", true)
	if err == nil || !strings.Contains(err.Error(), "cannot fail") {
		t.Fatalf("err = %v, want cannot fail", err)
	}
}

func TestCheckTestExercisesAcceptsAMethodAndAHelperAssertion(t *testing.T) {
	src := `package store

import "testing"

func check(tb testing.TB, got, want int) {
	tb.Helper()
	if got != want {
		tb.Fatalf("got %d, want %d", got, want)
	}
}

func TestGet(t *testing.T) {
	var s Store
	check(t, s.Get("k"), 0)
}
`
	if err := checkTestExercises(src, "Get", true); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesSkipsMainAndAnEmptyName(t *testing.T) {
	src := "package main\n\nimport \"testing\"\n\nfunc TestMain2(t *testing.T) { _ = main }\n"
	if err := checkTestExercises(src, "main", true); err != nil {
		t.Error(err)
	}
	if err := checkTestExercises(src, "", true); err != nil {
		t.Error(err)
	}
}

// The refusal goes back to the model, so it may name the contract function
// and must not echo anything else from the file.
func TestCheckTestExercisesRefusalQuotesOnlyTheContractName(t *testing.T) {
	src := "package cli_test\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {\n\tMARKERz9q7 := 1\n\tif MARKERz9q7 != 2 {\n\t\tt.Fatal(\"MARKERz9q7\")\n\t}\n}\n"
	err := checkTestExercises(src, "CANARY4c81e0x", true)
	if err == nil || !strings.Contains(err.Error(), "never calls CANARY4c81e0x") || strings.Contains(err.Error(), "MARKERz9q7") {
		t.Fatalf("err = %v, want a refusal naming CANARY4c81e0x and nothing from the file", err)
	}
}

func TestCheckTestExercisesAsksForAComparisonOnlyWithResults(t *testing.T) {
	src := "package cli_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Fatal(\"x\")\n}\n"
	err := checkTestExercises(src, "Run", true)
	if err == nil || !strings.HasSuffix(err.Error(), "test file never calls Run, the production function under test: every case must call it and compare what it returns") {
		t.Fatalf("err = %v, want the message with the comparison", err)
	}
	err = checkTestExercises(src, "Serve", hasResults("func Serve(addr string)"))
	if err == nil || !strings.HasSuffix(err.Error(), "test file never calls Serve, the production function under test: every case must call it") {
		t.Fatalf("err = %v, want the message ending at must call it", err)
	}
	if !hasResults("func Run(args []string, stdout, stderr io.Writer) int") {
		t.Error("Run has a result")
	}
}
