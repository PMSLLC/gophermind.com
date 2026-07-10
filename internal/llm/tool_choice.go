package llm

// ToolChoiceMode controls how the model selects tools for a turn.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model choose any tool or no tool.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone forces the model to produce a text response with no tool calls.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired forces the model to call at least one tool.
	ToolChoiceRequired ToolChoiceMode = "required"
)

// ToolChoiceForced forces a specific tool for a turn.
type ToolChoiceForced struct {
	Name string // the exact tool name to force
}

// ToolChoiceConfig holds the tool choice configuration for a request.
type ToolChoiceConfig struct {
	Mode   ToolChoiceMode
	Forced *ToolChoiceForced
}

// toolChoiceString converts a ToolChoiceConfig to the wire-format string.
func (tc ToolChoiceConfig) toolChoiceString() string {
	if tc.Forced != nil && tc.Forced.Name != "" {
		return `{"type":"function","function":{"name":"` + tc.Forced.Name + `"}}`
	}
	switch tc.Mode {
	case ToolChoiceNone:
		return "none"
	case ToolChoiceRequired:
		return "required"
	default:
		return "auto"
	}
}
