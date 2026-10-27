package packer

import (
	"reflect"
	"strings"
	"testing"
)

func TestAddMissingStdImports(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{"missing utf8", "package p\n\nfunc F(s string) int { return utf8.RuneCountInString(s) }\n", []string{"unicode/utf8"}},
		{"two missing", "package p\n\nimport \"os\"\n\nfunc F() { _ = filepath.Join(\"a\"); _ = strings.ToUpper(\"a\"); _ = os.Args }\n", []string{"path/filepath", "strings"}},
		{"already imported", "package p\n\nimport \"unicode/utf8\"\n\nfunc F() { _ = utf8.RuneLen('a') }\n", nil},
		{"aliased import", "package p\n\nimport u \"unicode/utf8\"\n\nfunc F() { _ = u.RuneLen('a') }\n", nil},
		{"local variable named like a package", "package p\n\nfunc F() { strings := []string{}; _ = strings[0]; var errors struct{ X int }; _ = errors.X }\n", nil},
		{"ambiguous name left alone", "package p\n\nfunc F() { _ = rand.Intn(3) }\n", nil},
		{"not a package", "package p\n\nfunc F(x T) { _ = x.Name }\n", nil},
	}
	for _, c := range cases {
		out, added := addMissingStdImports(c.src)
		if !reflect.DeepEqual(added, c.want) {
			t.Errorf("%s: added %v, want %v", c.name, added, c.want)
		}
		if c.want == nil && out != c.src {
			t.Errorf("%s: source changed", c.name)
		}
		for _, p := range c.want {
			if !strings.Contains(out, "import \""+p+"\"") {
				t.Errorf("%s: no import of %s in\n%s", c.name, p, out)
			}
		}
	}
	if out, added := addMissingStdImports("package p\nfunc ("); out != "package p\nfunc (" || added != nil {
		t.Errorf("unparsable source was changed: %q %v", out, added)
	}
}

func TestParseReplyAddsAMissingStdImport(t *testing.T) {
	e, err := NewExpect("cipher", "func Count(s string) int")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseReply("package cipher\n\nfunc Count(s string) int {\n\treturn utf8.RuneCountInString(s)\n}\n", e)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !reflect.DeepEqual(r.AddedImports, []string{"unicode/utf8"}) || !reflect.DeepEqual(r.Imports, []string{"unicode/utf8"}) {
		t.Errorf("added %v imports %v", r.AddedImports, r.Imports)
	}
	if !strings.Contains(string(r.Source), "unicode/utf8") {
		t.Errorf("source lacks the import:\n%s", r.Source)
	}
}
