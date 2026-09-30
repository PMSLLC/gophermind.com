package planner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

// StageOf returns the stage a planner request was made for, read from its
// system message, or "" for a request the planner did not build.
func StageOf(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem && strings.HasPrefix(m.Content, stagePrefixSystem) {
			return strings.TrimPrefix(m.Content, stagePrefixSystem)
		}
	}
	return ""
}

// FixtureProvider is the offline model: it answers every planner call with a
// canned reply from a directory. The stage "decompose:greeting" is served from
// decompose.greeting.txt; the nth request for the same stage (n >= 2) is
// served from decompose.greeting.<n>.txt when that file exists, otherwise from
// the base file again. A fixture set with only contract.outline.txt serves it for
// the shared outline pass (contract:outline:1) and for each batch. With several directories the first one holding the
// file wins, so a variant folder overrides single replies of a base fixture.
func FixtureProvider(dirs ...string) (*provider.Fake, error) {
	if len(dirs) == 0 {
		return nil, errors.New("planner: fixture provider needs at least one directory")
	}
	for _, d := range dirs {
		fi, err := os.Stat(d)
		if err != nil {
			return nil, fmt.Errorf("planner: fixture directory: %w", err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("planner: fixture %s is not a directory", d)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	find := func(name string) ([]byte, bool) {
		for _, d := range dirs {
			if raw, err := os.ReadFile(filepath.Join(d, name)); err == nil {
				return raw, true
			}
		}
		return nil, false
	}
	fn := func(_ int, req provider.Request) (provider.Response, error) {
		stage := StageOf(req)
		if stage == "" {
			return provider.Response{}, errors.New("planner: fixture provider got a request with no stage line")
		}
		base := strings.ReplaceAll(stage, ":", ".")
		mu.Lock()
		seen[stage]++
		n := seen[stage]
		mu.Unlock()
		var raw []byte
		var ok bool
		if n >= 2 {
			raw, ok = find(fmt.Sprintf("%s.%d.txt", base, n))
		}
		if !ok {
			raw, ok = find(base + ".txt")
		}
		if !ok && strings.HasPrefix(stage, "contract:outline:") && stage != outlineRepairStage {
			// A fixture set from before the outline was harness-driven: its one
			// contract.outline.txt is the shared pass, later batches add nothing.
			// The shared pass keeps only its types component; every batch then
			// carries the whole outline again, so the feature components arrive
			// (repeats of ids already written are dropped).
			raw, ok = find("contract.outline.txt")
		}
		if !ok {
			return provider.Response{}, fmt.Errorf("planner: no fixture reply for stage %q (looked for %s.txt in %s)", stage, base, strings.Join(dirs, ", "))
		}
		return provider.Response{Text: string(raw), Model: "fixture",
			Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
	}
	models := []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}}
	return provider.NewFake("fake", models, fn), nil
}

// FixtureSettings is the configuration that goes with FixtureProvider: one
// private provider named fake, in every tier.
func FixtureSettings() *settings.Config {
	c := settings.Default()
	c.Providers = []settings.ProviderConfig{{
		Name: "fake", BaseURL: "http://fixture.invalid/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}},
	}}
	c.Models = map[string][]string{}
	for _, tier := range settings.Tiers {
		c.Models[tier] = []string{"fake/fixture"}
	}
	return c
}
