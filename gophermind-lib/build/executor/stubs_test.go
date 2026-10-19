package executor

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/packer"
	"gophermind/gophermind-lib/build/planner"
)

const panicLine = `panic("gm: not implemented")`

func goIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestStubSource(t *testing.T) {
	tests := []struct {
		name, pkg, sig, support string
		imports                 []string
	}{
		{"greet", "greet", "func Greet(name string) (string, error)", "", nil},
		{"farewell", "greet", "func Farewell(name string) (string, error)", "", nil},
		{"method", "greet", "func (s *Store) Put(key string) error", "type Store struct{}\n", nil},
		{"context", "greet", "func Fetch(ctx context.Context, id int) error", "", []string{"context"}},
		{"request", "main", "func helloHandler(w http.ResponseWriter, r *http.Request)", "", []string{"net/http"}},
		{"serve", "main", "func serve(addr string) error", "", nil},
		{"named results", "greet", "func Count() (n int, err error)", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, err := StubSource(tc.pkg, tc.sig, tc.imports)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), "stub.go", src, 0); err != nil {
				t.Fatalf("stub does not parse: %v", err)
			}
			if fm, err := format.Source(src); err != nil || string(fm) != string(src) {
				t.Fatalf("stub is not gofmt-clean (%v)", err)
			}
			if !strings.HasPrefix(string(src), "package "+tc.pkg+"\n") {
				t.Fatalf("stub does not start with the package clause: %q", src)
			}
			for _, imp := range tc.imports {
				if !strings.Contains(string(src), `"`+imp+`"`) {
					t.Errorf("stub lacks import %s", imp)
				}
			}
			if !strings.Contains(string(src), tc.sig+" {\n\t"+panicLine+"\n}\n") {
				t.Fatalf("stub body is not exactly the panic line:\n%s", src)
			}
			if strings.Count(string(src), "return") != 0 {
				t.Fatal("stub has a return statement")
			}

			dir := t.TempDir()
			write(t, filepath.Join(dir, "go.mod"), "module example.com/stubtest\n\ngo 1.22\n")
			write(t, filepath.Join(dir, "stub.go"), string(src))
			if tc.support != "" {
				write(t, filepath.Join(dir, "support.go"), "package "+tc.pkg+"\n\n"+tc.support)
			}
			if out, err := goIn(t, dir, "vet", "./..."); err != nil {
				t.Fatalf("go vet: %v\n%s", err, out)
			}
		})
	}
}

func TestStubSourceRejectsBadSignature(t *testing.T) {
	for _, sig := range []string{"", "func (", "type X int", "func F() {}\nfunc G()"} {
		if _, err := StubSource("p", sig, nil); err == nil {
			t.Errorf("signature %q accepted", sig)
		}
	}
	if _, err := StubSource("p", "func F()", nil); err != nil {
		t.Fatal(err)
	}
}

func TestStubPath(t *testing.T) {
	if got := StubPath("internal/greet", "fn-greet"); got != "internal/greet/zz_gm_stub_fn-greet.go" {
		t.Fatalf("StubPath = %q", got)
	}
	if got := StubPath(".", "fn-x"); got != "zz_gm_stub_fn-x.go" {
		t.Fatalf("StubPath(.) = %q", got)
	}
}

func TestStubForGreeterLeaves(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	pol := g.plan.Policy()
	want := map[string]string{"fn-greet": "", "fn-bye": "net/http", "fn-hello": "net/http", "fn-serve": "", "fn-farewell": ""}
	for id, imp := range want {
		l := g.plan.Leaf(id)
		src, err := stubFor(g.plan.Contracts, pol, l)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if StubPath(l.Dir, id) != l.StubFile {
			t.Errorf("%s: StubPath %q != StubFile %q", id, StubPath(l.Dir, id), l.StubFile)
		}
		if imp != "" && !strings.Contains(string(src), `"`+imp+`"`) {
			t.Errorf("%s: stub lacks %s", id, imp)
		}
		if imp == "" && strings.Contains(string(src), "import") {
			t.Errorf("%s: stub has imports it does not need", id)
		}
		if !strings.Contains(string(src), l.Signature+" {\n\t"+panicLine+"\n}") {
			t.Errorf("%s: wrong body", id)
		}
	}
}

func TestStubForResolvesSiblingPackage(t *testing.T) {
	c := &contract.Contracts{Module: "example.com/m",
		Types:     []contract.Type{{ID: "t-user", Package: "model", File: "internal/model/user.go", Decl: "type User struct{}"}},
		Functions: []contract.Function{{ID: "fn-x", Package: "svc", File: "internal/svc/x.go", Signature: "func Load() (*model.User, error)"}}}
	l := &Leaf{ID: "fn-x", Package: "svc", File: "internal/svc/x.go", Signature: c.Functions[0].Signature, Dir: "internal/svc"}
	src, err := stubFor(c, packer.ImportPolicy{Module: "example.com/m"}, l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"example.com/m/internal/model"`) {
		t.Fatalf("stub lacks the sibling import:\n%s", src)
	}
}

func TestStubSourceBodilessFunctionsDoNotPanic(t *testing.T) {
	for _, sig := range []string{"func A()\nfunc B()", "func A() {}\nfunc B()", "func A()\nfunc B() {}", "func A()\n\nfunc B() int", "func A()"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("StubSource panicked on %q: %v", sig, r)
				}
			}()
			src, err := StubSource("p", sig, nil)
			if sig == "func A()" {
				if err != nil || !strings.Contains(string(src), panicLine) {
					t.Fatalf("a single signature failed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("signature %q accepted", sig)
			}
			if strings.Contains(err.Error(), "func A") || strings.Contains(err.Error(), "func B") {
				t.Fatalf("error quotes the signature: %v", err)
			}
		}()
	}
}

func TestStubSourceRefusesBadPackageName(t *testing.T) {
	for _, pkg := range []string{"p\nimport \"os/exec\"", "func", "_", "", "a b", "p;import \"os\"", "1p"} {
		if _, err := StubSource(pkg, "func F()", nil); err == nil {
			t.Errorf("package %q accepted", pkg)
		}
	}
}

func TestStubForRefusesSmuggledImportAndDirective(t *testing.T) {
	pol := packer.ImportPolicy{Module: "example.com/m"}
	c := &contract.Contracts{Module: "example.com/m"}
	for _, l := range []*Leaf{
		{ID: "fn-a", Package: "p\nimport \"os/exec\"", Signature: "func F()"},
		{ID: "fn-a", Package: "p", Signature: "func F() //go:linkname x y\n"},
		{ID: "fn-a", Package: "p", Signature: "func F()\nimport \"os/exec\""},
	} {
		if _, err := stubFor(c, pol, l); err == nil {
			t.Errorf("stubFor accepted %q / %q", l.Package, l.Signature)
		}
	}
}

func TestStubSourceRefusesImportPolicyViolationInFinalFile(t *testing.T) {
	// StubSource takes imports from the caller; stubFor checks the assembled file against the policy.
	pol := packer.ImportPolicy{Module: "example.com/m"}
	if err := checkAssembled([]byte("package p\n\nimport (\n\t\"os/exec\"\n)\n\nfunc F() {\n\tpanic(\"gm: not implemented\")\n}\n"), "p", pol); err == nil {
		t.Fatal("an os/exec import passed checkAssembled")
	}
}

func TestStubForModulePrefixedPackageResolvesSiblingQualifier(t *testing.T) {
	c := &contract.Contracts{Module: "csvstat",
		Types: []contract.Type{{ID: "t-sum", Package: "csvstat/internal/stats", File: "internal/stats/stats.go", Decl: "type ColumnSummary struct{}"}},
		Functions: []contract.Function{
			{ID: "fn-a", Package: "csvstat/internal/stats", File: "summarize.go", Signature: "func a() []ColumnSummary"},
			{ID: "fn-b", Package: "csvstat/internal/render", File: "render.go", Signature: "func b(s stats.ColumnSummary) error"}}}
	if got := packageImports(c)["stats"]; got != "csvstat/internal/stats" {
		t.Fatalf("stats imports as %q", got)
	}
	if got := packageImports(c)["render"]; got != "csvstat/internal/render" {
		t.Fatalf("render imports as %q", got)
	}
	l := &Leaf{ID: "fn-b", Package: "render", Signature: c.Functions[1].Signature, Dir: "internal/render"}
	src, err := stubFor(c, packer.ImportPolicy{Module: "csvstat"}, l)
	if err != nil || !strings.Contains(string(src), `"csvstat/internal/stats"`) {
		t.Fatalf("err %v\n%s", err, src)
	}
}

func TestStubForUnresolvableQualifierSharesThePlannerRule(t *testing.T) {
	c := &contract.Contracts{Module: "csvstat"}
	pol := packer.ImportPolicy{Module: "csvstat"}
	for _, q := range []string{"io", "time", "csvstat", "nosuch"} {
		_, got := resolveQualifier(q, "func f(x "+q+".T)", packageImports(c), pol)
		_, want := planner.ResolveQualifier(q, "func f(x "+q+".T)", packageImports(c), pol.Deps)
		if got != want {
			t.Errorf("%s: executor %v planner %v", q, got, want)
		}
	}
}

func TestStubForQualifiedForeignTypeImportsItsPackage(t *testing.T) {
	c := &contract.Contracts{Module: "github.com/example/csvstat",
		Types:     []contract.Type{{ID: "t-cs", Package: "internal/stats", File: "internal/stats/types.go", Decl: "type ColumnSummary struct{}"}},
		Functions: []contract.Function{{ID: "fn-render-json", Package: "internal/render", File: "internal/render/r.go", Signature: "func RenderJSON(summaries []stats.ColumnSummary) ([]byte, error)"}}}
	l := &Leaf{ID: "fn-render-json", Package: "render", File: "internal/render/r.go", Signature: c.Functions[0].Signature, Dir: "internal/render"}
	src, err := stubFor(c, packer.ImportPolicy{Module: c.Module}, l)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "stub.go", src, 0)
	if err != nil {
		t.Fatalf("stub does not parse: %v", err)
	}
	if len(f.Imports) != 1 || f.Imports[0].Path.Value != `"github.com/example/csvstat/internal/stats"` {
		t.Fatalf("imports = %v\n%s", f.Imports, src)
	}
}
