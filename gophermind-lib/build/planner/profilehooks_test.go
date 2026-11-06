package planner

import (
	"reflect"
	"testing"
)

// mergeBench merges a reply whose profile_hooks is hooks under the env the
// requirements give.
func mergeBench(t *testing.T, reqs []Requirement, hooks []any) (map[string]any, []string) {
	t.Helper()
	env := enrichEnvFixture()
	env.DropBench = !benchWanted(reqs)
	merged, ds, warns := mergeEnrichment(draftFixture(t), replyFixture(t, func(m map[string]any) { m["profile_hooks"] = hooks }), contractFixtureForEnrich(), env)
	if len(ds) != 0 {
		t.Fatalf("defects: %v", kindsOf(ds))
	}
	return merged, warns
}

const benchDropped = "profile_hook_dropped: fn-register: bench (no performance requirement in the brief)"

func TestBenchIsDroppedWithoutAPerformanceRequirement(t *testing.T) {
	reqs := []Requirement{
		{ID: "C1", Kind: ReqConstraint, Text: "Standard library only."},
		{ID: "F1", Kind: ReqFeature, Name: "Register", Text: "A user registers with an email."},
	}
	merged, warns := mergeBench(t, reqs, []any{"bench"})
	if _, ok := merged["profile_hooks"].(map[string]any)["not_applicable"].(string); !ok {
		t.Errorf("profile_hooks = %#v, want a not_applicable object", merged["profile_hooks"])
	}
	if n := countOf(warns, benchDropped); n != 1 {
		t.Errorf("warnings = %v, want %q once", warns, benchDropped)
	}
	merged, _ = mergeBench(t, reqs, []any{"bench", "pprof"})
	if !reflect.DeepEqual(merged["profile_hooks"], []any{"pprof"}) {
		t.Errorf("profile_hooks = %#v, want [pprof]", merged["profile_hooks"])
	}
}

func TestBenchIsKeptWithAPerformanceRequirement(t *testing.T) {
	reqs := []Requirement{{ID: "C1", Kind: ReqConstraint, Text: "Benchmarks of Greeting run in under 100ns"}}
	merged, warns := mergeBench(t, reqs, []any{"bench"})
	if !reflect.DeepEqual(merged["profile_hooks"], []any{"bench"}) {
		t.Errorf("profile_hooks = %#v, want [bench]", merged["profile_hooks"])
	}
	if countOf(warns, benchDropped) != 0 {
		t.Errorf("warnings = %v, want no bench drop", warns)
	}
	for _, text := range []string{"Latency stays low", "THROUGHPUT of 10 MB", "faster than the old one", "1000 requests per second", "Performance matters"} {
		if !benchWanted([]Requirement{{Kind: ReqFeature, Text: text}}) {
			t.Errorf("%q does not ask for a benchmark", text)
		}
	}
}

func TestHelloProfileHooksHaveNoBench(t *testing.T) {
	reqs := ParseRequirements(helloTestsBrief(t))
	if len(reqs) == 0 {
		t.Fatal("the hello brief has no requirements")
	}
	if benchWanted(reqs) {
		t.Fatal("the hello brief asks for a benchmark")
	}
	d := map[string]any{"id": "fn-greeting", "profile_hooks": []any{"bench"}}
	if !dropBench(d) {
		t.Fatal("bench was not dropped")
	}
	if _, ok := d["profile_hooks"].(map[string]any)["not_applicable"]; !ok {
		t.Errorf("profile_hooks = %#v, want not_applicable", d["profile_hooks"])
	}
}

func countOf(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}
