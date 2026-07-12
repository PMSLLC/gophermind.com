package phaseflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Config is the workflow configuration persisted at .planning/config.json. It is
// a faithful subset of upstream PhaseFlow's config: the fields gophermind's
// native engine reads to decide gating, granularity, and whether optional loop
// stages (research, plan-check, verifier) run. Unknown upstream keys are
// preserved on load so a config written by either tool round-trips cleanly.
type Config struct {
	Mode        string         `json:"mode"`        // interactive | autonomous
	Granularity string         `json:"granularity"` // coarse | standard | fine
	Workflow    WorkflowConfig `json:"workflow"`
	Gates       GatesConfig    `json:"gates"`

	// extra retains any upstream keys this port does not model, so writes made
	// by gophermind do not drop configuration another tool relies on.
	extra map[string]json.RawMessage
}

// WorkflowConfig controls which optional stages of the loop run.
type WorkflowConfig struct {
	Research    bool `json:"research"`
	PlanCheck   bool `json:"plan_check"`
	Verifier    bool `json:"verifier"`
	AutoAdvance bool `json:"auto_advance"`
}

// GatesConfig controls the human-review gates between loop stages. When a gate
// is true the engine pauses for confirmation; when false it advances.
type GatesConfig struct {
	ConfirmProject    bool `json:"confirm_project"`
	ConfirmRoadmap    bool `json:"confirm_roadmap"`
	ConfirmPlan       bool `json:"confirm_plan"`
	ExecuteNextPlan   bool `json:"execute_next_plan"`
	ConfirmTransition bool `json:"confirm_transition"`
}

// validGranularity reports whether g is one of the recognized granularity
// levels. Empty is treated as valid (DefaultConfig fills it).
func validGranularity(g string) bool {
	switch g {
	case "", "coarse", "standard", "fine":
		return true
	}
	return false
}

// DefaultConfig returns the configuration a fresh project starts with. Values
// match upstream PhaseFlow's template config.json defaults.
func DefaultConfig() Config {
	return Config{
		Mode:        "interactive",
		Granularity: "standard",
		Workflow: WorkflowConfig{
			Research:    true,
			PlanCheck:   true,
			Verifier:    true,
			AutoAdvance: false,
		},
		Gates: GatesConfig{
			ConfirmProject:    true,
			ConfirmRoadmap:    true,
			ConfirmPlan:       true,
			ExecuteNextPlan:   true,
			ConfirmTransition: true,
		},
	}
}

// LoadConfig reads config.json for a project root. If the file does not exist it
// returns DefaultConfig and a false found flag rather than an error, so callers
// can treat an uninitialized project as "use defaults".
func LoadConfig(root string) (cfg Config, found bool, err error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), false, nil
		}
		return Config{}, false, err
	}
	cfg = DefaultConfig()
	if err := cfg.unmarshal(data); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", ConfigPath(root), err)
	}
	if !validGranularity(cfg.Granularity) {
		return Config{}, false, fmt.Errorf("invalid granularity %q in %s", cfg.Granularity, ConfigPath(root))
	}
	return cfg, true, nil
}

// unmarshal decodes JSON into c while capturing any keys c does not model into
// c.extra, so they survive a subsequent Save.
func (c *Config) unmarshal(data []byte) error {
	// First decode the modeled fields.
	type alias Config
	tmp := alias(*c)
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*c = Config(tmp)

	// Then capture unmodeled top-level keys.
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	known := map[string]bool{"mode": true, "granularity": true, "workflow": true, "gates": true}
	c.extra = nil
	for k, v := range all {
		if known[k] {
			continue
		}
		if c.extra == nil {
			c.extra = map[string]json.RawMessage{}
		}
		c.extra[k] = v
	}
	return nil
}

// Save writes the config to .planning/config.json, creating the directory if
// needed and preserving any unmodeled upstream keys captured on load.
func (c Config) Save(root string) error {
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		return err
	}
	// Merge modeled fields with preserved extras into one object.
	base, err := json.Marshal(struct {
		Mode        string         `json:"mode"`
		Granularity string         `json:"granularity"`
		Workflow    WorkflowConfig `json:"workflow"`
		Gates       GatesConfig    `json:"gates"`
	}{c.Mode, c.Granularity, c.Workflow, c.Gates})
	if err != nil {
		return err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return err
	}
	for k, v := range c.extra {
		if _, ok := merged[k]; !ok {
			merged[k] = v
		}
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(ConfigPath(root), out, 0o644)
}
