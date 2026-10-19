package planner

import (
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
		name, method, ok := unexportedTarget(c.sig)
		if name != c.name || method != c.method || ok != c.want {
			t.Errorf("%q: got (%q, %v, %v), want (%q, %v, %v)", c.sig, name, method, ok, c.name, c.method, c.want)
		}
	}
}

func TestCheckTestSourceForUnexported(t *testing.T) {
	err := checkTestSourceFor(unexpSrc("render_test"), "internal/render", "TestRenderText", unexpMod, true)
	want := "the function under test is unexported, so the test file must declare package render (not render_test) and call it without a qualifier"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if err := checkTestSourceFor(unexpSrc("render"), "internal/render", "TestRenderText", unexpMod, true); err != nil {
		t.Fatalf("plain clause refused: %v", err)
	}
	if err := checkTestSourceFor(unexpSrc("other"), "internal/render", "TestRenderText", unexpMod, true); err == nil || !strings.Contains(err.Error(), "is not in package render") {
		t.Fatalf("wrong clause: %v", err)
	}
}

func TestCheckTestSourceForExportedKeepsBothClauses(t *testing.T) {
	for _, c := range []string{"render", "render_test"} {
		if err := checkTestSourceFor(unexpSrc(c), "internal/render", "TestRenderText", unexpMod, false); err != nil {
			t.Errorf("exported, clause %s refused: %v", c, err)
		}
	}
}

func TestCheckTestSourceForMainUnchanged(t *testing.T) {
	if err := checkTestSourceFor(unexpSrc("main"), "cmd/csvstat", "TestRenderText", unexpMod, true); err != nil {
		t.Fatalf("main refused: %v", err)
	}
	err := checkTestSourceFor(unexpSrc("csvstat_test"), "cmd/csvstat", "TestRenderText", unexpMod, true)
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
