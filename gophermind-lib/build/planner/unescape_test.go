package planner

import (
	"strings"
	"testing"
)

func TestUnescapeQuotedSource(t *testing.T) {
	broken := "package cli\n\nvar cases = []struct{ a, b string }{\n\t{\\\"happy\\\", \\\"42\\\"},\n}\n"
	got := unescapeQuotedSource(broken)
	if strings.Contains(got, `\"`) || !strings.Contains(got, `{"happy", "42"}`) {
		t.Fatalf("not repaired:\n%s", got)
	}
	// Valid Go with a real escaped quote is left alone.
	ok := "package cli\n\nvar s = \"a \\\"b\\\"\"\n"
	if unescapeQuotedSource(ok) != ok {
		t.Fatal("valid source was changed")
	}
	// A repair that does not parse either is not kept.
	bad := "package cli\n\nfunc {\\\"x\\\"\n"
	if unescapeQuotedSource(bad) != bad {
		t.Fatal("an unparseable repair was kept")
	}
}

func TestPlaceholderTestFileRefused(t *testing.T) {
	err := checkNotPlaceholder("<full Go source of the test file>")
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("err = %v", err)
	}
	if checkNotPlaceholder("package cli\n") != nil {
		t.Fatal("real source refused")
	}
}
