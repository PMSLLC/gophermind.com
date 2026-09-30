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

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
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
