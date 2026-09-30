package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
)

func TestTypeDeclsWritten(t *testing.T) {
	g := newRig(t)
	pol := g.plan.Policy()
	paths, err := WriteTypes(g.repo, g.plan.Contracts, pol)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "internal/greet/errors.go" {
		t.Fatalf("paths = %v", paths)
	}
	first, err := os.ReadFile(filepath.Join(g.repo, paths[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first), "package greet\n") || !strings.Contains(string(first), "type NameError struct") {
		t.Fatalf("file is wrong:\n%s", first)
	}
	if out, err := goIn(t, g.repo, "build", "./internal/greet"); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if _, err := WriteTypes(g.repo, g.plan.Contracts, pol); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(g.repo, paths[0]))
	if string(first) != string(second) {
		t.Fatal("a second call changed the file")
	}
}

func TestTypeDeclsSharingAFileKeepContractOrder(t *testing.T) {
	repo := t.TempDir()
	c := &contract.Contracts{Module: "example.com/m", Types: []contract.Type{
		{ID: "t-b", Package: "p", File: "p/types.go", Decl: "type B struct{ At time.Time }"},
		{ID: "t-a", Package: "p", File: "p/types.go", Decl: "type A int"},
		{ID: "t-c", Package: "q", File: "q/c.go", Decl: "type C struct{}"},
	}}
	paths, err := WriteTypes(repo, c, packer.ImportPolicy{Module: "example.com/m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "p/types.go,q/c.go" {
		t.Fatalf("paths = %v, want sorted and deduplicated", paths)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, "p/types.go"))
	s := string(raw)
	if !strings.Contains(s, `import "time"`) && !strings.Contains(s, "import (\n\t\"time\"\n)") {
		t.Fatalf("missing the derived import:\n%s", s)
	}
	if strings.Index(s, "type B") > strings.Index(s, "type A") || strings.Count(s, "package p") != 1 {
		t.Fatalf("order or package clause wrong:\n%s", s)
	}
}

func TestTypeDeclBadImportStopsRun(t *testing.T) {
	const canary = "CANARYDECLTEXT"
	cases := []struct {
		name, decl, want string
		deps             []string
	}{
		{"os/exec", "type Runner struct{ " + canary + " exec.Cmd }", "import not allowed", nil},
		{"does not parse", "type Runner struct{ " + canary + " int", "declaration does not parse", nil},
		{"unresolvable", "type Runner struct{ " + canary + " frobnicate.Thing }", "cannot resolve package qualifier", nil},
		{"rand ambiguity", "type Runner struct{ " + canary + " rand.Reader }", "cannot resolve package qualifier", nil},
		{"rand in a comment about crypto/rand", "// uses crypto/rand " + canary + "\ntype Runner struct{ R *rand.Rand }", "cannot resolve package qualifier", nil},
		{"template ambiguity", "type Runner struct{ " + canary + " *template.Template }", "cannot resolve package qualifier", nil},
		{"dependency not listed", "type Runner struct{ " + canary + " uuid.UUID }", "cannot resolve package qualifier", nil},
		{"import inside a declaration", "import \"os\"\ntype Runner struct{ " + canary + " int }", "declaration does not parse", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			c := &contract.Contracts{Module: "example.com/m", Types: []contract.Type{
				{ID: "t-ok", Package: "p", File: "p/ok.go", Decl: "type Ok struct{}"},
				{ID: "t-bad", Package: "p", File: "p/bad.go", Decl: tc.decl},
			}}
			paths, err := WriteTypes(repo, c, packer.ImportPolicy{Module: "example.com/m", Deps: tc.deps})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(paths) != 0 {
				t.Fatalf("paths = %v on error", paths)
			}
			if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "exec.Cmd") || strings.Contains(err.Error(), "os/exec") {
				t.Fatalf("error quotes declaration text: %v", err)
			}
			if !strings.Contains(err.Error(), "t-bad") {
				t.Fatalf("error does not name the type: %v", err)
			}
			if fileExists(filepath.Join(repo, "p/ok.go")) || fileExists(filepath.Join(repo, "p/bad.go")) {
				t.Fatal("a file was written although a type of the run failed")
			}
		})
	}
}

func TestTypeDeclDependencyAllowed(t *testing.T) {
	repo := t.TempDir()
	c := &contract.Contracts{Module: "example.com/m", Types: []contract.Type{
		{ID: "t-id", Package: "p", File: "p/id.go", Decl: "type ID struct{ V uuid.UUID }"},
	}}
	pol := packer.ImportPolicy{Module: "example.com/m", Deps: []string{"github.com/google/uuid"}}
	if _, err := WriteTypes(repo, c, pol); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, "p/id.go"))
	if !strings.Contains(string(raw), `"github.com/google/uuid"`) {
		t.Fatalf("dependency import missing:\n%s", raw)
	}
	// Plan.Policy carries dependencies.json the same way.
	g := newRig(t)
	g.plan.Deps = nil
	if p := g.plan.Policy(); len(p.Deps) != 0 {
		t.Fatal("policy should have no deps")
	}
}

func TestTypeDeclSiblingPackage(t *testing.T) {
	repo := t.TempDir()
	c := &contract.Contracts{Module: "example.com/m", Types: []contract.Type{
		{ID: "t-user", Package: "model", File: "internal/model/user.go", Decl: "type User struct{}"},
		{ID: "t-acct", Package: "acct", File: "internal/acct/acct.go", Decl: "type Account struct{ Owner model.User }"},
	}}
	if _, err := WriteTypes(repo, c, packer.ImportPolicy{Module: "example.com/m"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, "internal/acct/acct.go"))
	if !strings.Contains(string(raw), `"example.com/m/internal/model"`) {
		t.Fatalf("sibling import missing:\n%s", raw)
	}
}

func TestPackageImports(t *testing.T) {
	c := &contract.Contracts{Module: "example.com/m",
		Types:     []contract.Type{{ID: "t", Package: "model", File: "internal/model/user.go"}},
		Functions: []contract.Function{{ID: "f", Package: "main", File: "cmd/x/main.go"}, {ID: "g", Package: "util", File: "util.go"}}}
	got := packageImports(c)
	if got["model"] != "example.com/m/internal/model" || got["main"] != "example.com/m/cmd/x" || got["util"] != "example.com/m" {
		t.Fatalf("packageImports = %v", got)
	}
}

func TestWriteTypesRefusesUnsafePath(t *testing.T) {
	repo := t.TempDir()
	for _, f := range []string{"../evil.go", "/abs.go", ".git/x.go", "p/x_test.go"} {
		c := &contract.Contracts{Module: "example.com/m", Types: []contract.Type{{ID: "t", Package: "p", File: f, Decl: "type T int"}}}
		if _, err := WriteTypes(repo, c, packer.ImportPolicy{Module: "example.com/m"}); err == nil {
			t.Errorf("path %q accepted", f)
		}
	}
}
