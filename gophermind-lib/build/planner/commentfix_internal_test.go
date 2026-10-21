package planner

import (
	"strings"
	"testing"
)

func notes(n int) string { return strings.Repeat("// a note\n", n) }

func TestDropLongComments(t *testing.T) {
	head := "package p\n\nimport \"testing\"\n\n"
	body := "func TestX(t *testing.T) {\n\t_ = 1\n}\n"
	cases := []struct {
		name    string
		src     string
		want    int
		must    []string
		mustNot []string
		same    bool
	}{
		{name: "long block removed", src: head + notes(12) + body, want: 1, must: []string{body}, mustNot: []string{"a note"}},
		{name: "short block kept", src: head + notes(maxCommentRun) + body, same: true},
		{name: "directive kept", src: "//go:build linux\n\npackage p\n\n" + notes(12) + "func TestX() {}\n", want: 1, must: []string{"//go:build linux\n\npackage p"}, mustNot: []string{"a note"}},
		{name: "mixed group keeps directives only", src: head + notes(6) + "//go:noinline\n" + notes(6) + "func TestX(t *testing.T) {}\n", want: 1, must: []string{"//go:noinline\nfunc TestX"}, mustNot: []string{"a note"}},
		{name: "long directive-only group kept", src: head + strings.Repeat("//go:generate echo x\n", 10) + body, same: true},
		{name: "trailing comment on code line", src: head + body[:len(body)-2] + "\t_ = 2 /* " + strings.Repeat("x\n", 10) + "*/\n}\n", want: 1, must: []string{"_ = 2"}, mustNot: []string{"/*"}},
		{name: "block between declarations", src: head + "func A() {}\n\n" + notes(10) + "\nfunc TestX(t *testing.T) {}\n", want: 1, must: []string{"func A() {}", "func TestX"}, mustNot: []string{"a note"}},
		{name: "cgo preamble kept", src: "package p\n\n" + notes(12) + "import \"C\"\n\nfunc TestX() {}\n", same: true},
		{name: "unparseable unchanged", src: head + notes(12) + "func TestX( {\n", same: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := dropLongComments(c.src)
			if c.same {
				if got != c.src || n != 0 {
					t.Fatalf("changed (n=%d):\n%s", n, got)
				}
				return
			}
			if n != c.want {
				t.Errorf("count = %d, want %d", n, c.want)
			}
			for _, m := range c.must {
				if !strings.Contains(got, m) {
					t.Errorf("missing %q in\n%s", m, got)
				}
			}
			for _, m := range c.mustNot {
				if strings.Contains(got, m) {
					t.Errorf("%q still in\n%s", m, got)
				}
			}
		})
	}
}
