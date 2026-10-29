package planner

import "testing"

func TestCamelDashedTypeNames(t *testing.T) {
	in := "// slug-config holds the configuration.\ntype slug-config struct {\n\tmaxLen int\n}\n\nfunc (c *slug-config) Max() int { return c.maxLen }\n"
	got, n := camelDashedTypeNames(in)
	if n != 1 {
		t.Fatalf("renamed %d, want 1", n)
	}
	if err := TypeDeclShape(got); err != nil {
		t.Fatalf("still refused: %v\n%s", err, got)
	}
	want := "// slugConfig holds the configuration.\ntype slugConfig struct {\n\tmaxLen int\n}\n\nfunc (c *slugConfig) Max() int { return c.maxLen }\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	plain := "type Slug string\n"
	if got, n := camelDashedTypeNames(plain); n != 0 || got != plain {
		t.Errorf("a valid declaration was changed: %q", got)
	}
}
