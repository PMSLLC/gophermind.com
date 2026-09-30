package settings

import (
	"fmt"
	"net/http"

	"gophermind/gophermind-lib/briefv2/provider"
)

// BuildProviders constructs one OpenAI-compatible provider per configured
// entry. A provider with api_key_secret gets its key from secret(name); the
// value goes to the provider and nowhere else, and it never appears in an
// error. client is the HTTP client every provider uses; nil means
// http.DefaultClient (the harness proxy plan replaces this).
func (c *Config) BuildProviders(client *http.Client, secret func(name string) (string, error)) (map[string]provider.Provider, error) {
	if client == nil {
		client = http.DefaultClient
	}
	out := make(map[string]provider.Provider, len(c.Providers))
	for _, p := range c.Providers {
		key := ""
		if p.APIKeySecret != "" {
			if secret == nil {
				return nil, fmt.Errorf("settings: provider %s needs the secret %q but no secret source was given", p.Name, p.APIKeySecret)
			}
			v, err := secret(p.APIKeySecret)
			if err != nil {
				return nil, fmt.Errorf("settings: provider %s: secret %q: %w", p.Name, p.APIKeySecret, err)
			}
			key = v
		}
		models := make([]provider.ModelInfo, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens})
		}
		out[p.Name] = provider.NewOpenAI(provider.Config{
			Name: p.Name, BaseURL: p.BaseURL, APIKey: key, MaxConcurrent: p.MaxConcurrent,
			HTTPClient: client, Models: models, ReasoningEffort: p.ReasoningEffort,
		})
	}
	return out, nil
}
