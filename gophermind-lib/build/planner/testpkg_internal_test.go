package planner

import "testing"

// A model wrote "package internal_cli_test" for the package internal/cli: the
// import path with slashes turned into underscores. The harness knows the
// package, so it rewrites the clause instead of refusing the whole reply.
func TestFixTestPackageClause(t *testing.T) {
	cases := []struct{ name, src, pkg, want string }{
		{"path as a name", "package internal_cli_test\n\nimport \"testing\"\n", "internal/cli", "package cli\n\nimport \"testing\"\n"},
		{"unrelated name", "package slugify\n\nfunc TestX(t *testing.T) {}\n", "internal/slug", "package slug\n\nfunc TestX(t *testing.T) {}\n"},
		{"main package", "package slugify_test\n\nimport \"testing\"\n", "cmd/slugify", "package main\n\nimport \"testing\"\n"},
		{"already the package", "package cli\n\nimport \"testing\"\n", "internal/cli", "package cli\n\nimport \"testing\"\n"},
		{"already the external form", "package cli_test\n\nimport \"testing\"\n", "internal/cli", "package cli_test\n\nimport \"testing\"\n"},
		{"not Go", "this is not go code", "internal/cli", "this is not go code"},
		{"a comment first", "// Tests for run.\npackage run_test\n\nimport \"testing\"\n", "internal/cli", "// Tests for run.\npackage cli\n\nimport \"testing\"\n"},
	}
	for _, c := range cases {
		if got := fixTestPackageClause(c.src, c.pkg); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
