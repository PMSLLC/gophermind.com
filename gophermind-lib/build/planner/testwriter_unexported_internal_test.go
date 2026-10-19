package planner

import (
	"gophermind/gophermind-lib/build/contract"
	"strings"
	"testing"
)

const unexpMod = "github.com/example/csvstat"

func unexpSrc(clause string) string {
	return "package " + clause + "\n\nimport \"testing\"\n\nfunc TestRenderText(t *testing.T) {}\n"
}

func TestUnexportedTargetKinds(t *testing.T) {
	cases := []struct {
		sig    string
		name   string
		method bool
		want   bool
	}{
		{"func renderText(w int) error", "renderText", false, true},
		{"func RenderText(w int) error", "RenderText", false, false},
		{"func (r *table) Render() error", "Render", true, true},
		{"func (r table) Render() error", "Render", true, true},
		{"func (r *Table) render() error", "render", true, true},
		{"func (r *Table) Render() error", "Render", true, false},
		{"func (r *Table[T]) Render() error", "Render", true, false},
		{"func (r *table[T]) Render() error", "Render", true, true},
		{"", "", false, false},
	}
	for _, c := range cases {
		name, method, why := internalTarget(c.sig)
		ok := why != whyNone
		if name != c.name || method != c.method || ok != c.want {
			t.Errorf("%q: got (%q, %v, %v), want (%q, %v, %v)", c.sig, name, method, ok, c.name, c.method, c.want)
		}
	}
}

func TestCheckTestSourceForUnexported(t *testing.T) {
	err := checkTestSourceFor(unexpSrc("render_test"), "internal/render", "TestRenderText", unexpMod, whyFunc)
	want := "the function under test is unexported, so the test file must declare package render (not render_test) and call it without a qualifier"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if err := checkTestSourceFor(unexpSrc("render"), "internal/render", "TestRenderText", unexpMod, whyFunc); err != nil {
		t.Fatalf("plain clause refused: %v", err)
	}
	if err := checkTestSourceFor(unexpSrc("other"), "internal/render", "TestRenderText", unexpMod, whyFunc); err == nil || !strings.Contains(err.Error(), "is not in package render") {
		t.Fatalf("wrong clause: %v", err)
	}
}

func TestCheckTestSourceForExportedKeepsBothClauses(t *testing.T) {
	for _, c := range []string{"render", "render_test"} {
		if err := checkTestSourceFor(unexpSrc(c), "internal/render", "TestRenderText", unexpMod, whyNone); err != nil {
			t.Errorf("exported, clause %s refused: %v", c, err)
		}
	}
}

func TestCheckTestSourceForMainUnchanged(t *testing.T) {
	if err := checkTestSourceFor(unexpSrc("main"), "cmd/csvstat", "TestRenderText", unexpMod, whyFunc); err != nil {
		t.Fatalf("main refused: %v", err)
	}
	err := checkTestSourceFor(unexpSrc("csvstat_test"), "cmd/csvstat", "TestRenderText", unexpMod, whyFunc)
	if err == nil || err.Error() != mainRefusal {
		t.Fatalf("err = %v, want main refusal", err)
	}
}

func unexpReply(clause, sig string) (string, map[string]any) {
	ct := map[string]any{"file": "internal/render/render.go", "signature": sig, "errors": []any{}}
	reply := `{"tests":[{"name":"a","given":"g","expect":"e","polarity":"success","covers":"happy"}],"test_file":"` +
		strings.ReplaceAll(strings.ReplaceAll(unexpSrc(clause), "\"", `\"`), "\n", `\n`) + `"}`
	return reply, ct
}

func TestParseTestwriteUsesTheContractSignature(t *testing.T) {
	reply, ct := unexpReply("render_test", "func renderText(w int) error")
	_, _, err := parseTestwrite(reply, ct, "internal/render", "TestRenderText", unexpMod, nil)
	if err == nil || !strings.Contains(err.Error(), "is unexported, so the test file must declare package render (not render_test)") {
		t.Fatalf("unexported function: %v", err)
	}
	if _, _, err := parseTestwrite(reply, map[string]any{"file": ct["file"], "signature": "func (r *table) Render() error"}, "internal/render", "TestRenderText", unexpMod, nil); err == nil {
		t.Fatal("method on unexported type accepted _test clause")
	}
	reply, ct = unexpReply("render", "func renderText(w int) error")
	if _, _, err := parseTestwrite(reply, ct, "internal/render", "TestRenderText", unexpMod, nil); err != nil {
		t.Fatalf("plain clause: %v", err)
	}
	reply, ct = unexpReply("render_test", "func RenderText(w int) error")
	if _, _, err := parseTestwrite(reply, ct, "internal/render", "TestRenderText", unexpMod, nil); err != nil {
		t.Fatalf("exported with _test: %v", err)
	}
}

func TestTestwriterPromptUnexportedStatesTheFirstLine(t *testing.T) {
	node := `{"contract":{"signature":"func renderText(w int) error"}}`
	got, err := renderTestwriter(node, unexpMod, "internal/render/render.go", "TestRenderText", "internal/render/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"The function under test is unexported: the first line", "exactly `package render`", "`renderText(...)`", "without any package qualifier", "do not import the package"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(got, "package `X_test`") || strings.Contains(got, "csvstat words") {
		t.Errorf("unexported prompt still offers X_test:\n%s", got)
	}
	exp, err := renderTestwriter(`{"contract":{"signature":"func RenderText(w int) error"}}`, unexpMod, "internal/render/render.go", "TestRenderText", "internal/render/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exp, "The function under test is unexported: the first line") || !strings.Contains(exp, "package `X_test`") {
		t.Errorf("exported prompt changed:\n%s", exp)
	}
	m, err := renderTestwriter(`{"contract":{"signature":"func (r *table) Render() error"}}`, unexpMod, "internal/render/render.go", "TestX", "x_test.go")
	if err != nil || !strings.Contains(m, "call the method on a value of its receiver type") {
		t.Errorf("method prompt: %v\n%s", err, m)
	}
	mainP, err := renderTestwriter(`{"contract":{"signature":"func run() error"}}`, unexpMod, "cmd/csvstat/main.go", "TestX", "x_test.go")
	if err != nil || strings.Contains(mainP, "The function under test is unexported: the first line") || !strings.Contains(mainP, "no import of the package") {
		t.Errorf("main prompt changed: %v", err)
	}
}

func TestInternalTargetUnexportedTypesInSignature(t *testing.T) {
	cases := []struct {
		sig  string
		want string
	}{
		{"func Render(cfg renderConfig, s []stats.ColumnSummary, w io.Writer) error", whyTypes},
		{"func Render(w io.Writer) (renderResult, error)", whyTypes},
		{"func Render(c *renderConfig) error", whyTypes},
		{"func Render(c []renderConfig) error", whyTypes},
		{"func Render(c map[string]renderConfig) error", whyTypes},
		{"func Render(c map[renderKey]int) error", whyTypes},
		{"func Render(c ...renderConfig) error", whyTypes},
		{"func Render(f func(renderConfig) error) error", whyTypes},
		{"func Render(c struct{ A renderConfig }) error", whyTypes},
		{"func (t *Table) Render(c renderConfig) error", whyTypes},
		{"func Render(w io.Writer, s []stats.ColumnSummary, n int, name string, v any, err error, f float64, b []byte) error", whyNone},
		{"func Render(s stats.ColumnSummary) (*stats.ColumnSummary, error)", whyNone},
		{"func Map[T any, K comparable](xs []T, k K) map[K]T", whyNone},
		{"func Map[T any](xs ...T) []T", whyNone},
		{"func (t *Table[T]) Add(v T) error", whyNone},
		{"func Render(c Config) error", whyNone},
		{"func render(c Config) error", whyFunc},
		{"func Map[T renderConstraint](x T) T", whyTypes},
	}
	for _, c := range cases {
		_, _, got := internalTarget(c.sig)
		if got != c.want {
			t.Errorf("%q: why = %q, want %q", c.sig, got, c.want)
		}
	}
}

func TestCheckTestSourceForUnexportedTypesRefusal(t *testing.T) {
	err := checkTestSourceFor(unexpSrc("render_test"), "internal/render", "TestRenderText", unexpMod, whyTypes)
	want := "the signature of the function under test uses unexported types, which only code in the same package can build, so the test file must declare package render (not render_test) and call it without a qualifier"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if err := checkTestSourceFor(unexpSrc("render"), "internal/render", "TestRenderText", unexpMod, whyTypes); err != nil {
		t.Fatalf("plain clause refused: %v", err)
	}
}

func TestParseTestwriteExportedWithUnexportedParamType(t *testing.T) {
	sig := "func Render(cfg renderConfig, w io.Writer) error"
	reply, ct := unexpReply("render_test", sig)
	_, _, err := parseTestwrite(reply, ct, "internal/render", "TestRenderText", unexpMod, nil)
	if err == nil || !strings.Contains(err.Error(), "uses unexported types") {
		t.Fatalf("err = %v", err)
	}
	reply, ct = unexpReply("render", sig)
	if _, _, err := parseTestwrite(reply, ct, "internal/render", "TestRenderText", unexpMod, nil); err != nil {
		t.Fatalf("plain clause: %v", err)
	}
}

func TestTestwriterPromptUnexportedTypesStatesTheReason(t *testing.T) {
	node := `{"contract":{"signature":"func Render(cfg renderConfig, w io.Writer) error"}}`
	got, err := renderTestwriter(node, unexpMod, "internal/render/render.go", "TestRender", "internal/render/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"The signature of the function under test uses unexported types, which only code in the same package can build: the first line", "exactly `package render`", "`Render(...)`", "do not import the package"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(got, "The function under test is unexported") || strings.Contains(got, "package `X_test`") {
		t.Errorf("prompt gives the wrong reason:\n%s", got)
	}
}

func TestCompileMessageUnexportedTypeFromExternalPackage(t *testing.T) {
	c := compileCheckContract()
	c.Types = append(c.Types, contract.Type{ID: "t_cfg", Package: "internal/render", File: "internal/render/config.go", Decl: "type renderConfig struct {\n\tWidth int\n}"})
	m := compileMessage(c, "internal/render", "package render_test\n", "\tcfg := render.renderConfig{Width: 1}", 12, "undefined: render.renderConfig")
	for _, want := range []string{"undefined name at line 12", "uses unexported types, which only code in the same package can build", "package render (not render_test)"} {
		if !strings.Contains(m, want) {
			t.Errorf("message lacks %q: %s", want, m)
		}
	}
	if strings.Contains(m, "the function under test is unexported") {
		t.Errorf("wrong reason: %s", m)
	}
	m = compileMessage(c, "internal/stats", "package stats_test\n", "_ = stats.detectType(nil)", 3, "undefined: stats.detectType")
	if !strings.Contains(m, "the function under test is unexported") {
		t.Errorf("function reason lost: %s", m)
	}
}
