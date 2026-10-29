package planner

import (
	"strings"
	"testing"
)

func TestValidModulePath(t *testing.T) {
	for _, ok := range []string{"slugify", "example.com/slugify", "github.com/jbrahy/csv-stat", "a/b/c", "x_y.z~1"} {
		if !validModulePath(ok) {
			t.Errorf("%q should be a valid module path", ok)
		}
	}
	for _, bad := range []string{"", "/Users/jbrahy/.gophermind/local/repos/gm-2026-10-29-012", "~/src/x", "./x", "../x", "a//b", "a/", "with space", "a/../b", strings.Repeat("a", 300)} {
		if validModulePath(bad) {
			t.Errorf("%q should not be a valid module path", bad)
		}
	}
}

// A model wrote the repository's own file path as the module. go mod init then
// refused it and the whole build stopped before wave 0, with nothing built.
func TestOutlineWithAFilePathAsModuleIsRefusedWithAHint(t *testing.T) {
	text := strings.Replace(okOutline, `"module": "example.com/x"`, `"module": "/Users/jbrahy/.gophermind/local/repos/gm-2026-10-29-012"`, 1)
	_, _, err := parseOutline(text, testRunID)
	if err == nil {
		t.Fatal("an outline whose module is a file path was accepted")
	}
	if !strings.Contains(err.Error(), "module") || !strings.Contains(err.Error(), "not a file path") {
		t.Errorf("err = %v, want it to say what a module path is", err)
	}
	if strings.Contains(err.Error(), "/Users/jbrahy") {
		t.Errorf("the error quotes the path the model wrote: %v", err)
	}
}
