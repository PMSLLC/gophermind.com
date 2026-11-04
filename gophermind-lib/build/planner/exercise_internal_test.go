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
	if err := checkTestExercises(exercisedSrc, "Run"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesRefusesAFileThatNeverCallsTheFunction(t *testing.T) {
	src := strings.Replace(exercisedSrc, `got := cli.Run([]string{"x"}, nil, nil, nil)`, `got := 2`, 1)
	src = strings.Replace(src, "\n\t\"example.com/linkcheck/cli\"\n", "\n", 1)
	err := checkTestExercises(src, "Run")
	if err == nil || !strings.Contains(err.Error(), "never calls Run") {
		t.Fatalf("err = %v, want never calls Run", err)
	}
}

func TestCheckTestExercisesRefusesAFileThatCannotFail(t *testing.T) {
	src := strings.Replace(exercisedSrc, `t.Fatalf("Run = %d, want 2", got)`, `t.Log(got)`, 1)
	err := checkTestExercises(src, "Run")
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
	if err := checkTestExercises(src, "Get"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesSkipsMainAndAnEmptyName(t *testing.T) {
	src := "package main\n\nimport \"testing\"\n\nfunc TestMain2(t *testing.T) { _ = main }\n"
	if err := checkTestExercises(src, "main"); err != nil {
		t.Error(err)
	}
	if err := checkTestExercises(src, ""); err != nil {
		t.Error(err)
	}
}
