package runner

import (
	"reflect"
	"strings"
	"testing"
)

func ev(action, test string) string {
	s := `{"Action":"` + action + `","Package":"pkg"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	return s + "}\n"
}

func evOut(test, out string) string {
	s := `{"Action":"output","Package":"pkg"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	return s + `,"Output":"` + out + `"}` + "\n"
}

func TestParseTestJSON(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		events int
		passed []string
		failed []string
		panic  bool
		tmo    bool
		build  bool
		pkg    bool
	}{
		{name: "all pass with subtests",
			in:     ev("run", "TestX") + ev("run", "TestX/a") + ev("pass", "TestX/a") + ev("pass", "TestX") + ev("pass", ""),
			events: 4, passed: []string{"pkg.TestX", "pkg.TestX/a"}},
		{name: "one failing subtest also fails the parent",
			in:     ev("run", "TestX") + ev("run", "TestX/case_a") + ev("fail", "TestX/case_a") + ev("fail", "TestX") + ev("fail", ""),
			events: 4, failed: []string{"pkg.TestX/case_a"}},
		{name: "skip counts as an event and is not passed",
			in:     ev("run", "TestX") + ev("skip", "TestX") + ev("pass", ""),
			events: 2},
		{name: "panic with no terminal action",
			in:     ev("run", "TestX") + evOut("TestX", "panic: boom\\n") + ev("fail", ""),
			events: 1, failed: []string{"pkg.TestX"}, panic: true},
		{name: "test timeout",
			in:     ev("run", "TestX") + evOut("", "panic: test timed out after 1s\\n") + ev("fail", ""),
			events: 1, failed: []string{"pkg.TestX"}, tmo: true},
		{name: "FailedBuild field",
			in:    `{"Action":"fail","Package":"pkg","FailedBuild":"pkg"}` + "\n",
			build: true, pkg: true},
		{name: "old raw build failure",
			in:    "# pkg\n./a.go:3:1: undefined: x\nFAIL\tpkg [build failed]\n",
			build: true},
		{name: "race warning",
			in:     ev("run", "TestX") + evOut("TestX", "WARNING: DATA RACE\\n") + ev("pass", "TestX") + ev("fail", ""),
			events: 2, passed: []string{"pkg.TestX"}, pkg: true},
		{name: "garbage lines are ignored",
			in:     "garbage\n" + ev("run", "TestX") + "{not json\n" + ev("pass", "TestX") + "\n",
			events: 2, passed: []string{"pkg.TestX"}},
		{name: "empty", in: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ParseTestJSON(strings.NewReader(c.in))
			if r.Events != c.events {
				t.Errorf("Events = %d, want %d", r.Events, c.events)
			}
			var got []string
			for k := range r.Passed {
				got = append(got, k)
			}
			if len(got) != len(c.passed) {
				t.Errorf("Passed = %v, want %v", got, c.passed)
			}
			for _, p := range c.passed {
				if !r.Passed[p] {
					t.Errorf("Passed missing %q", p)
				}
			}
			if !reflect.DeepEqual(append([]string(nil), r.Failed...), append([]string(nil), c.failed...)) {
				t.Errorf("Failed = %v, want %v", r.Failed, c.failed)
			}
			if r.Panicked != c.panic || r.TimedOut != c.tmo || r.BuildFailed != c.build || r.PkgFailed != c.pkg {
				t.Errorf("panic=%v timeout=%v build=%v pkg=%v, want %v %v %v %v", r.Panicked, r.TimedOut, r.BuildFailed, r.PkgFailed, c.panic, c.tmo, c.build, c.pkg)
			}
		})
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"TestX/case_a":           "TestX/case_a",
		"a b":                    "(unnamed)",
		"a\nb":                   "(unnamed)",
		"a=b":                    "(unnamed)",
		"CANARY secret":          "(unnamed)",
		"":                       "(unnamed)",
		strings.Repeat("a", 200): "(unnamed)",
		strings.Repeat("a", 80):  strings.Repeat("a", 80),
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLocations(t *testing.T) {
	repo := "/work/repo"
	cases := []struct {
		name string
		in   string
		want []Location
	}{
		{"compiler line", "./internal/greet/greet.go:12:3: undefined: x", []Location{{"internal/greet/greet.go", 12, 3}}},
		{"no prefix", "internal/greet/greet.go:12:3: undefined: x", []Location{{"internal/greet/greet.go", 12, 3}}},
		{"line only", "a.go:12: something", []Location{{"a.go", 12, 0}}},
		{"vet prefix", "vet: a.go:5:2: undefined: y", []Location{{"a.go", 5, 2}}},
		{"absolute under repo", "/work/repo/a/b.go:7:1: bad", []Location{{"a/b.go", 7, 1}}},
		{"absolute outside repo", "/usr/local/go/src/fmt/print.go:7:1: bad", nil},
		{"outside via dotdot", "../../usr/local/go/src/fmt/print.go:7:1: bad", nil},
		{"header ignored", "# example.com/t/internal/greet", nil},
		{"panic stack", "goroutine 1 [running]:\nexample.com/t.F(...)\n\t/work/repo/x.go:9 +0x1d\n\t/usr/local/go/src/testing/testing.go:1 +0x1", []Location{{"x.go", 9, 0}}},
		{"duplicates collapse", "a.go:1:1: m\na.go:1:1: m\nb.go:2:2: m\na.go:1:1: m", []Location{{"a.go", 1, 1}, {"b.go", 2, 2}}},
		{"indented test log ignored", "    a_test.go:3: got 1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLocations(repo, c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
