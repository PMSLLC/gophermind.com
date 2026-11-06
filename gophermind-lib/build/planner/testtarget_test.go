package planner

import (
	"context"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/router"
)

func TestCheckTestTarget(t *testing.T) {
	const runSig = "func Run(args []string, stdout, stderr io.Writer) int"
	for _, tc := range []struct {
		name, sig, file, testFile, want string
	}{
		{"a test function", "func TestRunCLISeparateStreams(t *testing.T)", "internal/cli/run_test.go", "internal/cli/fn_run_test.go",
			"the function under test is a test function"},
		{"a test file as the contract file", runSig, "internal/cli/run_test.go", "internal/cli/fn_run_test.go",
			"the contract file is a test file"},
		{"the artifact is the target", runSig, "internal/cli/run.go", "internal/cli/run.go",
			"the artifact and the target are the same file"},
		{"no name", "func (", "internal/cli/run.go", "internal/cli/fn_run_test.go",
			"the function under test has no name"},
		{"a valid target", runSig, "internal/cli/run.go", "internal/cli/fn_run_test.go", ""},
		{"func main", "func main()", "cmd/hello/main.go", "cmd/hello/fn_main_test.go", ""},
	} {
		err := checkTestTarget("fn-run", tc.sig, tc.file, tc.testFile)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v, want nil", tc.name, err)
			}
			continue
		}
		want := "invalid_test_target: node fn-run (" + tc.sig + "): " + tc.want
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", tc.name, err, want)
		}
	}
}

// countingCaller records the stage of every call and answers none of them.
type countingCaller struct {
	mu     sync.Mutex
	stages []string
}

func (c *countingCaller) CallParsed(ctx context.Context, info router.CallInfo, req provider.Request, parse func(string) error) (router.Result, error) {
	c.mu.Lock()
	c.stages = append(c.stages, info.Stage)
	c.mu.Unlock()
	return router.Result{}, &router.ChainExhausted{Tier: info.Tier}
}

func TestWriteTestsRefusesAnInvalidTargetBeforeAnyCall(t *testing.T) {
	caller, sink := &countingCaller{}, events.NewCollector()
	p := New(Deps{Caller: caller, Sink: sink})
	r := &run{id: "gm-2026-11-05-001", dir: t.TempDir(), repo: t.TempDir()}
	const sig = "func TestX(t *testing.T)"
	c := &contract.Contracts{Module: "example.com/x", Functions: []contract.Function{
		{ID: "fn-x", Package: "internal/x", File: "internal/x/x.go", Signature: sig}}}
	d := map[string]any{"id": "fn-x", "kind": "function", "contract": map[string]any{
		"package": "internal/x", "file": "internal/x/x.go", "signature": sig}}
	_, err := p.writeTests(context.Background(), r, c, d, "pure", &testwriterState{}, nil)
	want := "invalid_test_target: node fn-x (" + sig + "): the function under test is a test function"
	if err == nil || err.Error() != want {
		t.Fatalf("writeTests = %v, want %q", err, want)
	}
	for _, s := range caller.stages {
		if strings.HasPrefix(s, "testwrite:") {
			t.Fatalf("the model was called for an invalid target: %v", caller.stages)
		}
	}
	shown := false
	for _, e := range sink.Events() {
		shown = shown || e.Message == want
	}
	if !shown {
		t.Errorf("no event carries %q: %+v", want, sink.Events())
	}
}
