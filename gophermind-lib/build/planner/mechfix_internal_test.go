package planner

import (
	"go/format"
	"reflect"
	"strings"
	"testing"
)

const mechParsingSrc = "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = srv }\n"

func TestApplyMechanicalFixes(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		diags   []compileDiag
		want    string
		fixes   []mechanicalFix
		same    bool
		unfixed int
	}{
		{name: "unused variable after a plain statement",
			src:   "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\twantErr := true\n\tt.Log(1)\n}\n",
			diags: []compileDiag{{6, 2, "declared and not used: wantErr"}},
			want:  "wantErr := true\n\t_ = wantErr\n\tt.Log(1)", fixes: []mechanicalFix{{"unused variable", "wantErr"}}},
		{name: "header-declared variable",
			src:   "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tfor _, tc := range []int{1} {\n\t\tt.Log(1)\n\t}\n}\n",
			diags: []compileDiag{{6, 9, "declared and not used: tc"}},
			want:  "range []int{1} {\n\t\t_ = tc\n\t\tt.Log(1)", fixes: []mechanicalFix{{"unused variable", "tc"}}},
		{name: "type switch variable goes in every clause",
			src:   "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tvar v any\n\tswitch x := v.(type) {\n\tcase int:\n\t\tt.Log(1)\n\tcase string:\n\t\tt.Log(2)\n\t}\n}\n",
			diags: []compileDiag{{7, 9, "declared and not used: x"}},
			want:  "case int:\n\t\t_ = x\n\t\tt.Log(1)\n\tcase string:\n\t\t_ = x\n\t\tt.Log(2)", fixes: []mechanicalFix{{"unused variable", "x"}}},
		{name: "missing standard import",
			src:   "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Log(strings.ToUpper(\"a\")) }\n",
			diags: []compileDiag{{5, 34, "undefined: strings"}},
			want:  "\"strings\"", fixes: []mechanicalFix{{"missing import", "strings"}}},
		{name: "undefined non-standard name is left",
			src:   mechParsingSrc,
			diags: []compileDiag{{5, 32, "undefined: srv"}}, same: true, unfixed: 1},
		{name: "type mismatch is left",
			src:   mechParsingSrc,
			diags: []compileDiag{{5, 1, "cannot use x (variable of type int) as string value"}}, same: true, unfixed: 1},
		{name: "file that does not parse is returned unchanged",
			src: "package p\n\nfunc TestX( {\n", diags: []compileDiag{{3, 1, "declared and not used: x"}}, same: true, unfixed: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, fixes, unfixed := applyMechanicalFixes(tc.src, tc.diags)
			if tc.same {
				if got != tc.src {
					t.Errorf("src changed:\n%s", got)
				}
			} else if !strings.Contains(got, tc.want) {
				t.Errorf("result lacks %q:\n%s", tc.want, got)
			}
			if !reflect.DeepEqual(fixes, tc.fixes) {
				t.Errorf("fixes = %v, want %v", fixes, tc.fixes)
			}
			if len(unfixed) != tc.unfixed {
				t.Errorf("unfixed = %v, want %d", unfixed, tc.unfixed)
			}
		})
	}
}

func TestApplyMechanicalFixesFormatsTheResult(t *testing.T) {
	src := "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\twantErr := true\n\tt.Log(strings.ToUpper(\"a\"))\n}\n"
	diags := []compileDiag{{6, 2, "declared and not used: wantErr"}, {7, 8, "undefined: strings"}}
	got, fixes, unfixed := applyMechanicalFixes(src, diags)
	if len(fixes) != 2 || len(unfixed) != 0 {
		t.Fatalf("fixes = %v, unfixed = %v", fixes, unfixed)
	}
	formatted, err := format.Source([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != got {
		t.Errorf("result is not gofmt-clean:\n%s", got)
	}
}

func TestApplyMechanicalFixesImportAlreadyPresentIsLeft(t *testing.T) {
	src := "package p\n\nimport (\n\t\"testing\"\n\tstrings \"example.com/other\"\n)\n\nfunc TestX(t *testing.T) { t.Log(strings.X) }\n"
	got, fixes, unfixed := applyMechanicalFixes(src, []compileDiag{{8, 34, "undefined: strings"}})
	if got != src || len(fixes) != 0 || len(unfixed) != 1 {
		t.Errorf("got %q, %v, %v", got, fixes, unfixed)
	}
}

func TestApplyMechanicalFixesNoImportDecl(t *testing.T) {
	src := "package p\n\nfunc TestX() { _ = strings.ToUpper(\"a\") }\n"
	got, fixes, _ := applyMechanicalFixes(src, []compileDiag{{3, 18, "undefined: strings"}})
	if !strings.Contains(got, "import (\n\t\"strings\"\n)") || len(fixes) != 1 {
		t.Errorf("got %q, %v", got, fixes)
	}
}

// TestApplyMechanicalFixesScopes checks the whole result, so a misplaced or
// extra insertion cannot pass.
func TestApplyMechanicalFixesScopes(t *testing.T) {
	const head = "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n"
	tests := []struct {
		name  string
		body  string
		diag  compileDiag
		name2 string
		want  string
	}{
		{name: "if header", name2: "v", diag: compileDiag{6, 5, "declared and not used: v"},
			body: "\tif v := 1; t != nil {\n\t\tt.Log(1)\n\t}\n",
			want: "\tif v := 1; t != nil {\n\t\t_ = v\n\t\tt.Log(1)\n\t}\n"},
		{name: "select comm clause", name2: "v", diag: compileDiag{8, 7, "declared and not used: v"},
			body: "\tch := make(chan int)\n\tselect {\n\tcase v := <-ch:\n\t\tt.Log(1)\n\t}\n",
			want: "\tch := make(chan int)\n\tselect {\n\tcase v := <-ch:\n\t\t_ = v\n\t\tt.Log(1)\n\t}\n"},
		{name: "nested block of a loop", name2: "w", diag: compileDiag{7, 3, "declared and not used: w"},
			body: "\tfor i := 0; i < 1; i++ {\n\t\tw := i\n\t\tt.Log(1)\n\t}\n",
			want: "\tfor i := 0; i < 1; i++ {\n\t\tw := i\n\t\t_ = w\n\t\tt.Log(1)\n\t}\n"},
		{name: "else if header", name2: "y", diag: compileDiag{8, 12, "declared and not used: y"},
			body: "\tif t == nil {\n\t\tt.Log(1)\n\t} else if y := 2; t != nil {\n\t\tt.Log(2)\n\t}\n",
			want: "\tif t == nil {\n\t\tt.Log(1)\n\t} else if y := 2; t != nil {\n\t\t_ = y\n\t\tt.Log(2)\n\t}\n"},
		{name: "labeled loop header", name2: "tc", diag: compileDiag{7, 9, "declared and not used: tc"},
			body: "L:\n\tfor _, tc := range []int{1} {\n\t\tt.Log(1)\n\t\tbreak L\n\t}\n",
			want: "L:\n\tfor _, tc := range []int{1} {\n\t\t_ = tc\n\t\tt.Log(1)\n\t\tbreak L\n\t}\n"},
		{name: "wrong column falls back to the name on the line", name2: "wantErr", diag: compileDiag{6, 40, "declared and not used: wantErr"},
			body: "\twantErr := true\n\tt.Log(1)\n",
			want: "\twantErr := true\n\t_ = wantErr\n\tt.Log(1)\n"},
		{name: "name not on the line is left", name2: "wantErr", diag: compileDiag{7, 2, "declared and not used: wantErr"},
			body: "\twantErr := true\n\tt.Log(1)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := head + tc.body + "}\n"
			got, fixes, unfixed := applyMechanicalFixes(src, []compileDiag{tc.diag})
			if tc.want == "" {
				if got != src || len(fixes) != 0 || len(unfixed) != 1 {
					t.Errorf("got %q, %v, %v", got, fixes, unfixed)
				}
				return
			}
			if want := head + tc.want + "}\n"; got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if !reflect.DeepEqual(fixes, []mechanicalFix{{"unused variable", tc.name2}}) || len(unfixed) != 0 {
				t.Errorf("fixes = %v, unfixed = %v", fixes, unfixed)
			}
		})
	}
}
