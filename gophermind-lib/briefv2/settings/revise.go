package settings

import (
	"fmt"
	"strings"
)

// Values of revise.mode.
const (
	ReviseModeRewrite = "rewrite" // the planner rewrites the node (sub-project E)
	ReviseModeNotes   = "notes"   // the old hint-only rung
)

// ReviseConfig is the revise: section of gophermind.yaml. An absent section,
// or an absent key, gets the default. Messages of validate name the key only.
type ReviseConfig struct {
	Mode         string `yaml:"mode"`
	ModelTier    string `yaml:"model_tier"`
	Context      string `yaml:"context"`
	RewriteTests string `yaml:"rewrite_tests"`
	MaxTokens    int    `yaml:"max_tokens"`
	MaxRefusals  int    `yaml:"max_refusals"`
}

// DefaultRevise returns the defaults of the planner revise stage design, section 12.
func DefaultRevise() ReviseConfig {
	return ReviseConfig{Mode: ReviseModeRewrite, ModelTier: "strong", Context: "node", RewriteTests: "on", MaxTokens: 4096, MaxRefusals: 2}
}

// applyReviseDefaults fills every zero value of the section. A negative number
// is left alone so validate can refuse it.
func (c *Config) applyReviseDefaults() {
	d, r := DefaultRevise(), &c.Revise
	if r.Mode == "" {
		r.Mode = d.Mode
	}
	if r.ModelTier == "" {
		r.ModelTier = d.ModelTier
	}
	if r.Context == "" {
		r.Context = d.Context
	}
	if r.RewriteTests == "" {
		r.RewriteTests = d.RewriteTests
	}
	if r.MaxTokens == 0 {
		r.MaxTokens = d.MaxTokens
	}
	if r.MaxRefusals == 0 {
		r.MaxRefusals = d.MaxRefusals
	}
}

func (r ReviseConfig) validate() error {
	for _, f := range []struct {
		key, val string
		ok       []string
	}{
		{"mode", r.Mode, []string{ReviseModeRewrite, ReviseModeNotes}},
		{"model_tier", r.ModelTier, []string{"strong", "standard"}},
		{"context", r.Context, []string{"node", "component"}},
		{"rewrite_tests", r.RewriteTests, []string{"on", "off"}},
	} {
		found := false
		for _, o := range f.ok {
			found = found || f.val == o
		}
		if !found {
			return fmt.Errorf("revise.%s must be one of: %s", f.key, strings.Join(f.ok, ", "))
		}
	}
	if r.MaxTokens < 256 || r.MaxTokens > 32768 {
		return fmt.Errorf("revise.max_tokens must be between 256 and 32768")
	}
	if r.MaxRefusals < 1 || r.MaxRefusals > 5 {
		return fmt.Errorf("revise.max_refusals must be between 1 and 5")
	}
	return nil
}
