package packer

import (
	"strings"
	"testing"
)

func genericExpect(t *testing.T, sig string) Expect {
	t.Helper()
	e, err := NewExpect("greet", sig)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestGenericSignatures(t *testing.T) {
	gen := genericExpect(t, "func F[T any](x T) T")
	stack := genericExpect(t, "func (s *Stack[T]) Push(x T)")
	cases := []struct {
		name string
		e    Expect
		file string
		ok   bool
	}{
		{"same", gen, "func F[T any](x T) T { return x }", true},
		{"spacing", gen, "func F[ T  any ]( x  T ) T { return x }", true},
		{"constraint", gen, "func F[T comparable](x T) T { return x }", false},
		{"tparam name", gen, "func F[U any](x U) U { return x }", false},
		{"non-generic", gen, "func F(x int) int { return x }", false},
		{"generic vs expected plain", genericExpect(t, "func F(x int) int"), "func F[T any](x int) int { return x }", false},
		{"extra tparam", gen, "func F[T any, U any](x T) T { return x }", false},
		{"method ok", stack, "func (s *Stack[T]) Push(x T) {}", true},
		{"method value recv", stack, "func (s Stack[T]) Push(x T) {}", false},
		{"method other type", stack, "func (s *Other[T]) Push(x T) {}", false},
	}
	for _, c := range cases {
		_, err := ParseReply("package greet\n\n"+c.file+"\n", c.e)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestSameMethodNameOtherReceiverAllowed(t *testing.T) {
	e := genericExpect(t, "func (a *A) Run() int")
	e.Declared = []string{}
	src := "package greet\n\ntype b struct{}\n\nfunc (a *A) Run() int { return 1 }\n\nfunc (x *b) Run() int { return 2 }\n"
	if _, err := ParseReply(src, e); err != nil {
		t.Errorf("distinct receivers rejected: %v", err)
	}
}

func TestForgedPassInitRefused(t *testing.T) {
	e := fixture(t)
	forged := "package greet\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc init() {\n\tfmt.Println(\"PASS\")\n\tos.Exit(0)\n}\n\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	r, err := ParseReply(forged, e)
	if err == nil || !strings.Contains(err.Error(), "init") || r.Source != nil {
		t.Fatalf("forged init reply accepted: %v", err)
	}
}

func TestReplyGateRefusals(t *testing.T) {
	e := fixture(t)
	e.Declared = []string{"Store", "helperTaken", "Other"}
	fn := "\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	cases := map[string]string{
		"init":                   "func init() {}",
		"generate":               "//go:generate touch x\n",
		"linkname":               "import _ \"unsafe\"\n//go:linkname a b\nfunc a()",
		"embed":                  "//go:embed x\nvar s string",
		"build tag":              "//go:build ignore\n",
		"plus build":             "// +build ignore\n",
		"export":                 "//export Foo\nfunc Foo() {}",
		"exported helper":        "func Helper() {}",
		"exported type":          "type Thing int",
		"collides":               "func helperTaken() {}",
		"blank var":              "var _ = 1",
		"method on foreign type": "func (s *Store) Extra() {}",
		"os exit":                "import \"os\"\nfunc h() { os.Exit(0) }",
		"os exit alias":          "import q \"os\"\nvar v = func() int { q.Exit(0); return 0 }()",
		"os exit value":          "import \"os\"\nvar f = os.Exit",
		"dot import os":          "import . \"os\"\nfunc h() { Exit(0) }",
		"syscall exit":           "import \"syscall\"\nfunc h() { syscall.Exit(0) }",
		"log fatal":              "import \"log\"\nfunc h() { log.Fatalf(\"x\") }",
		"log panicln":            "import \"log\"\nfunc h() { log.Panicln(\"x\") }",
		"goexit":                 "import \"runtime\"\nfunc h() { runtime.Goexit() }",
		"recover":                "func h() { defer func() { _ = recover() }() }",
	}
	for name, decl := range cases {
		src := "package greet\n\n" + decl + "\n" + fn
		if strings.HasPrefix(decl, "//go:build") || strings.HasPrefix(decl, "// +build") {
			src = decl + "\npackage greet\n" + fn
		}
		r, err := ParseReply(src, e)
		if err == nil || r.Source != nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), "helperTaken") || strings.Contains(err.Error(), "touch") {
			t.Errorf("%s: reply text in error: %v", name, err)
		}
	}
	ok := "package greet\n\nimport \"strings\"\n\nconst limit = 3\n\ntype box struct{ n int }\n\nvar cache = map[string]int{}\n\nfunc (b *box) get() int { return b.n }\n\nfunc helper(s string) string { return strings.ToUpper(s) }\n" + fn
	if _, err := ParseReply(ok, e); err != nil {
		t.Errorf("legitimate helpers refused: %v", err)
	}
}

func TestReplySizeCap(t *testing.T) {
	e := fixture(t)
	big := goodFile + "// " + strings.Repeat("a", MaxReplyBytes) + "\n"
	if _, err := ParseReply(big, e); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("bytes cap: %v", err)
	}
	many := goodFile + strings.Repeat("\n", MaxReplyLines+1)
	if _, err := ParseReply(many, e); err == nil || !strings.Contains(err.Error(), "too many lines") {
		t.Errorf("lines cap: %v", err)
	}
	if MaxReplyBytes <= 0 || MaxReplyLines <= 0 {
		t.Error("caps must be positive")
	}
}

func TestNoExportedFunctionEchoesReplyPath(t *testing.T) {
	e := fixture(t)
	p := ImportPolicy{Module: "example.com/greeter"}
	evil := "CANARY-reply/x"
	src := "package greet\n\nimport _ \"" + evil + "\"\n" + "\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	r, err := ParseReply(src, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Check(r.Imports)) != 1 {
		t.Fatal("evil import should be disallowed")
	}
	msg := p.Describe(r.Imports)
	if msg == "" || strings.Contains(msg, "CANARY") || !strings.Contains(msg, "1 ") {
		t.Errorf("describe: %q", msg)
	}
	if p.Describe([]string{"fmt"}) != "" {
		t.Error("describe of allowed set must be empty")
	}
}

func TestNilDeclaredFailsClosed(t *testing.T) {
	e := fixture(t) // Declared is nil
	src := "package greet\n\nfunc helper() {}\n\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	if _, err := ParseReply(src, e); err == nil {
		t.Error("nil Declared must refuse helpers")
	}
	e.Declared = []string{}
	if _, err := ParseReply(src, e); err != nil {
		t.Errorf("empty non-nil Declared allows helpers: %v", err)
	}
	if _, err := ParseReply(goodFile, fixture(t)); err != nil {
		t.Errorf("no helpers needs no Declared: %v", err)
	}
}
