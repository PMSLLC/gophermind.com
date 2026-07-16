package orchestrate

import "testing"

func TestResolveModel(t *testing.T) {
	cases := []struct {
		name        string
		tier        string
		speedModel  string
		strongModel string
		want        string
	}{
		{"speed tier", "speed", "gpt-speed", "gpt-strong", "gpt-speed"},
		{"strong tier", "strong", "gpt-speed", "gpt-strong", "gpt-strong"},
		{"concrete name passes through", "claude-3-5-sonnet", "gpt-speed", "gpt-strong", "claude-3-5-sonnet"},
		{"empty tier defaults to strong", "", "gpt-speed", "gpt-strong", "gpt-strong"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveModel(c.tier, c.speedModel, c.strongModel); got != c.want {
				t.Errorf("resolveModel(%q, %q, %q) = %q, want %q", c.tier, c.speedModel, c.strongModel, got, c.want)
			}
		})
	}
}
