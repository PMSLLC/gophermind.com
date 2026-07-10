package llm

import "testing"

func TestToolChoiceString(t *testing.T) {
	cases := []struct {
		cfg  ToolChoiceConfig
		want string
	}{
		{ToolChoiceConfig{Mode: ToolChoiceAuto}, "auto"},
		{ToolChoiceConfig{Mode: ToolChoiceNone}, "none"},
		{ToolChoiceConfig{Mode: ToolChoiceRequired}, "required"},
		{ToolChoiceConfig{Forced: &ToolChoiceForced{Name: "read_file"}}, `{"type":"function","function":{"name":"read_file"}}`},
	}
	for _, c := range cases {
		if got := c.cfg.toolChoiceString(); got != c.want {
			t.Errorf("toolChoiceString(%+v) = %q, want %q", c.cfg, got, c.want)
		}
	}
}
