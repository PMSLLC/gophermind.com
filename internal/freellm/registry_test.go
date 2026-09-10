package freellm

import "testing"

func TestLoadParsesEmbeddedRegistry(t *testing.T) {
	r := Load()
	if got := len(r.All()); got == 0 {
		t.Fatalf("embedded registry has no providers")
	}
	if r.LastUpdated() == "" {
		t.Error("registry has no lastUpdated date")
	}
}

func TestLookupHitAndMiss(t *testing.T) {
	r := Load()
	p, ok := r.Lookup("Groq")
	if !ok {
		t.Fatal("Groq not found in registry")
	}
	if len(p.Models) == 0 {
		t.Error("Groq has no models")
	}
	if _, ok := r.Lookup("No Such Provider"); ok {
		t.Error("Lookup reported a hit for a provider that does not exist")
	}
}

func TestEveryProviderHasNameAndURL(t *testing.T) {
	for _, p := range Load().All() {
		if p.Name == "" {
			t.Error("a provider has an empty name")
		}
		if p.URL == "" {
			t.Errorf("provider %q has no signup URL", p.Name)
		}
	}
}
