package planner

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
)

func typesFixture() *contract.Contracts {
	return &contract.Contracts{
		Module: "example.com/acme",
		Types: []contract.Type{
			{ID: "user", Package: "store", File: "store/user.go", Decl: "type User struct {\n\tID string\n}\n\ntype Store interface {\n\tGet(id string) (User, error)\n}"},
		},
		Components: []contract.Component{{ID: "registration", Package: "httpapi"}, {ID: "types", Package: "store"}},
	}
}

func TestDeriveGoTypesReadsTheSignature(t *testing.T) {
	g, err := deriveGoTypes("func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request, opts ...Option) (User, error)", typesFixture())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"http.ResponseWriter", "*http.Request", "...Option"}; !reflect.DeepEqual(g.Params, want) {
		t.Errorf("params = %v, want %v", g.Params, want)
	}
	if want := []string{"User", "error"}; !reflect.DeepEqual(g.Results, want) {
		t.Errorf("results = %v, want %v", g.Results, want)
	}
	if len(g.Unresolved) != 1 || g.Unresolved[0] != "Option" {
		t.Errorf("unresolved = %v, want [Option]", g.Unresolved)
	}
	if g.ParamByName["r"] != "*http.Request" || g.ParamByName["opts"] != "...Option" || len(g.ParamByName) != 3 {
		t.Errorf("ParamByName = %v", g.ParamByName)
	}
}

func TestDeriveGoTypesResolvesEveryKindOfName(t *testing.T) {
	for _, sig := range []string{
		"func F(a map[string][]*User, b chan<- error, c func(ctx context.Context) error, d interface{ Close() error }) (int, string, any)",
		"func F(s store.Store) (store.User, bool)",
		"func Map[T any, U comparable](in []T, f func(T) U) []U",
		"func F(n int8, r rune, b byte, f float64, c complex128, e error) uintptr",
	} {
		g, err := deriveGoTypes(sig, typesFixture())
		if err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
		if len(g.Unresolved) != 0 {
			t.Errorf("%s: unresolved %v", sig, g.Unresolved)
		}
	}
}

func TestDeriveGoTypesFlagsUnknownNames(t *testing.T) {
	g, err := deriveGoTypes("func F(x Ghost, y map[string]Phantom, z other.Thing) error", typesFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Unresolved) != 3 {
		t.Errorf("unresolved = %v, want 3 (Ghost, Phantom, other)", g.Unresolved)
	}
}

func TestDeriveGoTypesRejectsABadSignature(t *testing.T) {
	if _, err := deriveGoTypes("this is not Go", typesFixture()); err == nil {
		t.Fatal("a signature that does not parse must be an error")
	}
}
