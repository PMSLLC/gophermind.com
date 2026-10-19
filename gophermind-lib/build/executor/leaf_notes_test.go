package executor

import (
	"errors"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/packer"
)

func TestMalformedNoteByKind(t *testing.T) {
	e, err := packer.NewExpect("stats", "func Finalize() int")
	if err != nil {
		t.Fatal(err)
	}
	e.Declared = []string{}
	const canary = "CANARYTEXT"
	cases := []struct{ name, text, want string }{
		{"declaration", "package stats\n\nfunc Extra() int { return 1 }\n\nfunc Finalize() int { return 1 }\n", noteDeclaration},
		{"signature", "package stats\n\nfunc Finalize() string { return \"" + canary + "\" }\n", noteSignature},
		{"not found", "package stats\n\nfunc " + canary + "() {}\n", noteNotFound},
		{"package", "package " + strings.ToLower(canary) + "\n\nfunc Finalize() int { return 1 }\n", notePackage},
		{"syntax", "package stats\n\nfunc Finalize( {" + canary + "\n", noteSyntax},
		{"unknown kind", "", noteMalformed},
	}
	for _, c := range cases {
		_, perr := packer.ParseReply(c.text, e)
		if perr == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		got := malformedNote(perr)
		if got != c.want || strings.Contains(got, canary) {
			t.Errorf("%s: note %q", c.name, got)
		}
	}
	if malformedNote(errors.New("x")) != noteMalformed {
		t.Error("a plain error must keep noteMalformed")
	}
	if !strings.Contains(noteDeclaration, "do not declare them again") {
		t.Error("declaration note lost its advice")
	}
}
