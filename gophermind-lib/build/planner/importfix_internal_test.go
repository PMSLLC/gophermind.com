package planner

import (
	"strings"
	"testing"
)

func TestDropUnusedImports(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    string // substrings that must be present
		absent  string // substring that must be gone ("" for none)
		removed []string
		same    bool
	}{
		{
			name:    "unused std import removed",
			src:     "package p\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {}\n",
			want:    "\"testing\"",
			absent:  "bytes",
			removed: []string{"bytes"},
		},
		{
			name: "used import kept",
			src:  "package p\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _ = bytes.NewBuffer(nil) }\n",
			same: true,
		},
		{
			name: "aliased import used is kept",
			src:  "package p\n\nimport (\n\tb \"bytes\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _ = b.NewBuffer(nil) }\n",
			same: true,
		},
		{
			name:    "aliased import unused is removed",
			src:     "package p\n\nimport (\n\tb \"bytes\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _ = bytes.NewBuffer(nil) }\n",
			want:    "\"testing\"",
			absent:  "b \"bytes\"",
			removed: []string{"b"},
		},
		{
			name: "blank and dot imports kept",
			src:  "package p\n\nimport (\n\t_ \"embed\"\n\t. \"strings\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {}\n",
			same: true,
		},
		{
			name:    "single-line import removed whole",
			src:     "package p\n\nimport \"bytes\"\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
			want:    "\"testing\"",
			absent:  "bytes",
			removed: []string{"bytes"},
		},
		{
			name:    "every import unused leaves a valid file",
			src:     "package p\n\nimport (\n\t\"bytes\"\n\t\"fmt\"\n)\n\nfunc TestX() {}\n",
			want:    "func TestX()",
			absent:  "import",
			removed: []string{"bytes", "fmt"},
		},
		{
			name:    "versioned path uses the previous element",
			src:     "package p\n\nimport (\n\t\"math/rand/v2\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {}\n",
			want:    "\"testing\"",
			absent:  "rand",
			removed: []string{"rand"},
		},
		{
			name: "unparseable file unchanged",
			src:  "package p\n\nimport \"bytes\"\n\nfunc TestX( {\n",
			same: true,
		},
		{
			name: "path whose package name is unknown is kept",
			src:  "package p\n\nimport (\n\t\"example.com/go-thing\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {}\n",
			same: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, removed := dropUnusedImports(c.src)
			if c.same {
				if got != c.src || len(removed) != 0 {
					t.Fatalf("changed: removed=%v\n%s", removed, got)
				}
				return
			}
			if strings.Join(removed, ",") != strings.Join(c.removed, ",") {
				t.Errorf("removed = %v, want %v", removed, c.removed)
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("missing %q in\n%s", c.want, got)
			}
			if c.absent != "" && strings.Contains(got, c.absent) {
				t.Errorf("%q still present in\n%s", c.absent, got)
			}
		})
	}
}
