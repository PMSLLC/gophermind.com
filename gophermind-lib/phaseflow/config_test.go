package phaseflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaultsWhenMissing(t *testing.T) {
	root := t.TempDir()
	cfg, found, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if found {
		t.Error("found should be false for a missing config")
	}
	if cfg.Mode != "interactive" || cfg.Granularity != "standard" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if !cfg.Workflow.Verifier || !cfg.Gates.ConfirmRoadmap {
		t.Error("default workflow/gates flags wrong")
	}
}

func TestConfigRoundTripPreservesUnknownKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	// A config with a key this port does not model.
	raw := `{"mode":"autonomous","granularity":"fine","security":{"asvs_level":2},"project_code":"XYZ"}`
	if err := os.WriteFile(ConfigPath(root), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := LoadConfig(root)
	if err != nil || !found {
		t.Fatalf("load: %v found=%v", err, found)
	}
	if cfg.Mode != "autonomous" || cfg.Granularity != "fine" {
		t.Errorf("modeled fields wrong: %+v", cfg)
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, _ := os.ReadFile(ConfigPath(root))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["security"]; !ok {
		t.Error("unknown key 'security' was dropped on save")
	}
	if m["project_code"] != "XYZ" {
		t.Error("unknown key 'project_code' was dropped on save")
	}
}

func TestConfigInvalidGranularity(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(PlanningDir(root), 0o755)
	_ = os.WriteFile(ConfigPath(root), []byte(`{"granularity":"bogus"}`), 0o644)
	if _, _, err := LoadConfig(root); err == nil {
		t.Error("expected error for invalid granularity")
	}
}

func TestConfigSaveDefault(t *testing.T) {
	root := t.TempDir()
	if err := DefaultConfig().Save(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(PlanningDir(root), "config.json")); err != nil {
		t.Errorf("config.json not written: %v", err)
	}
}
