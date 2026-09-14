package tools

import (
	"strings"
	"testing"
)

func TestSparklineRender(t *testing.T) {
	out, err := run(t, Chart(), `{"values":[0,1,2,3,4,5,6,7]}`)
	if err != nil {
		t.Fatal(err)
	}
	// Sparkline uses the 8 block glyphs; lowest maps to the first, highest to last.
	if !strings.ContainsAny(out, "▁▂▃▄▅▆▇█") {
		t.Errorf("expected spark glyphs, got: %q", out)
	}
	if !strings.HasPrefix(out, "▁") {
		t.Errorf("min value should map to lowest glyph: %q", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("max value should map to full block: %q", out)
	}
}

func TestSparklineFlat(t *testing.T) {
	// All equal values shouldn't divide by zero; render a flat line.
	out, err := run(t, Chart(), `{"values":[5,5,5]}`)
	if err != nil {
		t.Fatalf("flat series errored: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("flat series should still render: %q", out)
	}
}

func TestBarChartRender(t *testing.T) {
	out, err := run(t, Chart(), `{"values":[1,2,4],"type":"bar","labels":["a","b","c"]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a", "b", "c", "█"} {
		if !strings.Contains(out, want) {
			t.Errorf("bar chart missing %q:\n%s", want, out)
		}
	}
	// Largest value should have the longest bar.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 bar lines, got %d:\n%s", len(lines), out)
	}
	if strings.Count(lines[2], "█") <= strings.Count(lines[0], "█") {
		t.Errorf("largest value should have the longest bar:\n%s", out)
	}
}

func TestChartEmpty(t *testing.T) {
	if _, err := run(t, Chart(), `{"values":[]}`); err == nil {
		t.Error("empty values should error")
	}
}
