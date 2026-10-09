package settings_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/settings"
)

func TestReviseDefaults(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	want := settings.ReviseConfig{Mode: "rewrite", ModelTier: "strong", Context: "node", RewriteTests: "on", MaxTokens: 4096, MaxRefusals: 2}
	if got := settings.DefaultRevise(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultRevise = %+v", got)
	}
	if got := settings.Default().Revise; !reflect.DeepEqual(got, want) {
		t.Fatalf("Default().Revise = %+v", got)
	}
	// A file written before the section existed loads with the defaults and is not rewritten.
	fresh := filepath.Join(t.TempDir(), "fresh.yaml")
	if _, err := settings.Load(fresh); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fresh)
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "revise")
	old := filepath.Join(t.TempDir(), "old.yaml")
	body, _ := yaml.Marshal(m)
	if err := os.WriteFile(old, body, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := settings.Load(old)
	if err != nil {
		t.Fatalf("old file: %v", err)
	}
	if !reflect.DeepEqual(c.Revise, want) {
		t.Fatalf("old file Revise = %+v, want the defaults", c.Revise)
	}
	if after, _ := os.ReadFile(old); string(after) != string(body) {
		t.Fatal("Load rewrote an existing settings file")
	}
}

// loadWith loads the defaults file with the revise section replaced by section.
func loadWith(t *testing.T, section map[string]any) (*settings.Config, error) {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	fresh := filepath.Join(t.TempDir(), "fresh.yaml")
	if _, err := settings.Load(fresh); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fresh)
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["revise"] = section
	body, _ := yaml.Marshal(m)
	p := filepath.Join(t.TempDir(), "x.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return settings.Load(p)
}

func TestReviseValidationNamesTheKeyNotTheValue(t *testing.T) {
	for _, c := range []struct {
		key string
		val any
	}{
		{"mode", "sideways"}, {"model_tier", "any"}, {"context", "brief"}, {"rewrite_tests", "maybe"},
		{"max_tokens", 255}, {"max_tokens", 32769}, {"max_refusals", 6}, {"max_refusals", -1},
	} {
		_, err := loadWith(t, map[string]any{c.key: c.val})
		if err == nil || !strings.Contains(err.Error(), "revise."+c.key) {
			t.Errorf("%s=%v: err = %v, want one naming revise.%s", c.key, c.val, err, c.key)
			continue
		}
		if s, ok := c.val.(string); ok && strings.Contains(err.Error(), s) {
			t.Errorf("%s: the error quotes the value: %v", c.key, err)
		}
	}
}

func TestReviseExplicitZeroAndUnknownKeyAreRefused(t *testing.T) {
	for _, key := range []string{"max_tokens", "max_refusals"} {
		if _, err := loadWith(t, map[string]any{key: 0}); err == nil || !strings.Contains(err.Error(), "revise."+key+" must be at least 1") {
			t.Errorf("explicit 0 for %s: err = %v", key, err)
		}
	}
	if _, err := loadWith(t, map[string]any{"api_key": "x"}); err == nil {
		t.Error("an unknown key in the revise section was accepted")
	}
	c, err := loadWith(t, map[string]any{"mode": "notes", "context": "component", "rewrite_tests": "off", "model_tier": "standard", "max_tokens": 512, "max_refusals": 3})
	if err != nil {
		t.Fatal(err)
	}
	want := settings.ReviseConfig{Mode: "notes", ModelTier: "standard", Context: "component", RewriteTests: "off", MaxTokens: 512, MaxRefusals: 3}
	if !reflect.DeepEqual(c.Revise, want) {
		t.Fatalf("Revise = %+v", c.Revise)
	}
}
