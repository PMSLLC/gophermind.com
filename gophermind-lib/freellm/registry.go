// Package freellm exposes a vendored registry of free LLM API providers so
// gophermind can run against a free endpoint without a paid key, and can
// attribute and meter that usage.
//
// The registry data in data.json is vendored verbatim from
// github.com/mnfst/awesome-free-llm-apis (CC0 1.0) and is never hand-edited;
// scripts/sync-free-providers.sh refreshes it. Everything gophermind knows
// that upstream does not lives in compat.go.
package freellm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data.json
var rawRegistry []byte

// Model is one model a provider offers on its free tier. Every field is
// upstream free text; RateLimit is parsed opportunistically by quota.go.
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Context   string `json:"context"`
	MaxOutput string `json:"maxOutput"`
	Modality  string `json:"modality"`
	RateLimit string `json:"rateLimit"`
}

// Provider is one entry in the upstream registry.
type Provider struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Country     string  `json:"country"`
	URL         string  `json:"url"`
	BaseURL     string  `json:"baseUrl"`
	Description string  `json:"description"`
	Models      []Model `json:"models"`
}

// Registry is the parsed contents of the embedded data.json.
type Registry struct {
	lastUpdated string
	providers   []Provider
	byName      map[string]Provider
}

var (
	loadOnce sync.Once
	loaded   *Registry
)

// Load parses the embedded registry, once per process. The data is compiled
// in, so a parse failure is a build defect rather than a runtime condition and
// panics; TestLoadParsesEmbeddedRegistry catches it before release.
func Load() *Registry {
	loadOnce.Do(func() {
		var doc struct {
			LastUpdated string     `json:"lastUpdated"`
			Providers   []Provider `json:"providers"`
		}
		if err := json.Unmarshal(rawRegistry, &doc); err != nil {
			panic(fmt.Sprintf("freellm: embedded data.json is malformed: %v", err))
		}
		byName := make(map[string]Provider, len(doc.Providers))
		for _, p := range doc.Providers {
			byName[p.Name] = p
		}
		loaded = &Registry{lastUpdated: doc.LastUpdated, providers: doc.Providers, byName: byName}
	})
	return loaded
}

// Lookup returns the provider with the given upstream name.
func (r *Registry) Lookup(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// All returns every provider in upstream order.
func (r *Registry) All() []Provider { return r.providers }

// LastUpdated is the date upstream stamped on the vendored data, so callers
// can show how stale the registry is.
func (r *Registry) LastUpdated() string { return r.lastUpdated }
