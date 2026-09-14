package doctor

import (
	"errors"
	"testing"
)

func TestAutoFixGitInit(t *testing.T) {
	called := ""
	p := Params{Root: "/repo", GitInit: func(dir string) error { called = dir; return nil }}
	results := []Result{
		{Name: "git repository", OK: false, Detail: "not inside a git repository"},
		{Name: "endpoint configured", OK: false, Detail: "no BaseURL"},
		{Name: "git", OK: true, Detail: "/usr/bin/git"},
	}
	fixes := AutoFix(p, results)

	byName := map[string]FixResult{}
	for _, f := range fixes {
		byName[f.Name] = f
	}
	// The git repo failure is auto-fixed via git init.
	git := byName["git repository"]
	if !git.Fixed {
		t.Errorf("git repository should be auto-fixed: %+v", git)
	}
	if called != "/repo" {
		t.Errorf("GitInit called with %q, want /repo", called)
	}
	// The endpoint failure is not auto-fixable; it should be reported with a hint.
	ep := byName["endpoint configured"]
	if ep.Fixed {
		t.Errorf("endpoint should not be auto-fixed: %+v", ep)
	}
	if ep.Detail == "" {
		t.Errorf("non-fixable failure should carry a hint: %+v", ep)
	}
	// Passing checks are not touched.
	if _, ok := byName["git"]; ok {
		t.Error("passing checks should not appear in fix results")
	}
}

func TestAutoFixGitInitError(t *testing.T) {
	p := Params{Root: "/repo", GitInit: func(string) error { return errors.New("boom") }}
	results := []Result{{Name: "git repository", OK: false, Detail: "x"}}
	fixes := AutoFix(p, results)
	if len(fixes) != 1 || fixes[0].Fixed {
		t.Errorf("a failing git init should report Fixed=false: %+v", fixes)
	}
}
