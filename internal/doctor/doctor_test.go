package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksAllPass(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)

	res := Checks(Params{
		BaseURL: "http://endpoint.local",
		Model:   "test-model",
		Root:    dir,
		HTTPGet: func(url string) (int, error) { return 200, nil },
		// look for a binary that always exists in CI/dev
		lookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
	})

	byName := map[string]Result{}
	for _, r := range res {
		byName[r.Name] = r
	}
	for _, want := range []string{"endpoint configured", "endpoint reachable", "model", "ripgrep (rg)", "git", "git repository"} {
		r, ok := byName[want]
		if !ok {
			t.Fatalf("missing check %q", want)
		}
		if !r.OK {
			t.Errorf("check %q should pass: %s", want, r.Detail)
		}
	}
}

func TestEndpointUnreachableFails(t *testing.T) {
	res := Checks(Params{
		BaseURL:  "http://nope.local",
		Root:     t.TempDir(),
		HTTPGet:  func(url string) (int, error) { return 0, errConn },
		lookPath: func(name string) (string, error) { return "", errConn },
	})
	for _, r := range res {
		if r.Name == "endpoint reachable" && r.OK {
			t.Error("endpoint reachable should fail when HTTPGet errors")
		}
		if r.Name == "ripgrep (rg)" && r.OK {
			t.Error("rg check should fail when not on PATH")
		}
	}
}

func TestNoBaseURLFails(t *testing.T) {
	res := Checks(Params{Root: t.TempDir(), lookPath: func(string) (string, error) { return "", nil }})
	for _, r := range res {
		if r.Name == "endpoint configured" && r.OK {
			t.Error("endpoint configured should fail with empty BaseURL")
		}
	}
}

func TestReportFormat(t *testing.T) {
	var b strings.Builder
	ok := Report(&b, []Result{
		{Name: "a", OK: true, Detail: "fine"},
		{Name: "b", OK: false, Detail: "broken"},
	})
	if ok {
		t.Error("Report should return false when any check fails")
	}
	out := b.String()
	if !strings.Contains(out, "✓") || !strings.Contains(out, "✗") {
		t.Errorf("report missing status marks: %q", out)
	}
	if !strings.Contains(out, "broken") {
		t.Errorf("report missing detail: %q", out)
	}
}

var errConn = &connErr{}

type connErr struct{}

func (*connErr) Error() string { return "connection refused" }
