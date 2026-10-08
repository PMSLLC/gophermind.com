package projectrun

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/vault"
)

// SecretStore is the slice of the vault this file needs.
type SecretStore interface {
	Get(scope, name string) (string, bool)
	Set(scope, name, value string) error
}

// Source says where a declared secret's value comes from.
type Source string

const (
	SourceVault       Source = "vault"
	SourceHex32       Source = "generated:hex32"
	SourcePlaceholder Source = "generated:placeholder"
	SourcePrompt      Source = "prompt"
	SourceMissing     Source = "missing"
)

// Provisioned names a secret and its source. It never carries a value.
type Provisioned struct {
	Name   string `json:"name"`
	Source Source `json:"source"`
}

// Generate returns a fresh value of the given kind ("hex32" or "placeholder")
// read from rnd; a nil rnd means crypto/rand.
func Generate(kind string, rnd io.Reader) (string, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	var n int
	var prefix string
	switch kind {
	case "hex32":
		n = 32
	case "placeholder":
		n, prefix = 16, "gm-placeholder-"
	default:
		return "", fmt.Errorf("projectrun: unknown generate kind %q", kind)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(rnd, buf); err != nil {
		return "", fmt.Errorf("projectrun: generate %s: random source failed: %w", kind, err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func get(store SecretStore, scope, name string) (string, bool) {
	if store == nil {
		return "", false
	}
	return store.Get(scope, name)
}

// Sources is read only: where each declared secret's value would come from.
func Sources(store SecretStore, b *brief.Brief, gen map[string]string, canPrompt bool) []Provisioned {
	var out []Provisioned
	for _, s := range b.Front.Secrets {
		src := SourceMissing
		if _, ok := get(store, vault.HarnessScope, s.Name); ok {
			src = SourceVault
		} else if kind, ok := gen[s.Name]; ok {
			src = Source("generated:" + kind)
		} else if canPrompt {
			src = SourcePrompt
		}
		out = append(out, Provisioned{s.Name, src})
	}
	return out
}

// ProvisionGenerated writes generated values into the harness scope, before
// the planner runs. On a fresh run it also replaces a run-scope value that
// differs from the harness value, so a stale copy cannot mask a change.
func ProvisionGenerated(store SecretStore, b *brief.Brief, gen map[string]string, resume bool, rnd io.Reader) ([]Provisioned, error) {
	if len(b.Front.Secrets) == 0 {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("projectrun: no secret store for %d declared secrets", len(b.Front.Secrets))
	}
	var out []Provisioned
	for _, s := range b.Front.Secrets {
		if _, ok := store.Get(vault.HarnessScope, s.Name); ok {
			out = append(out, Provisioned{s.Name, SourceVault})
			continue
		}
		kind, ok := gen[s.Name]
		if !ok {
			out = append(out, Provisioned{s.Name, SourceMissing})
			continue
		}
		val, err := Generate(kind, rnd)
		if err != nil {
			return out, fmt.Errorf("projectrun: secret %s: %w", s.Name, err)
		}
		if err := store.Set(vault.HarnessScope, s.Name, val); err != nil {
			return out, fmt.Errorf("projectrun: store generated secret %s failed", s.Name)
		}
		out = append(out, Provisioned{s.Name, Source("generated:" + kind)})
	}
	if resume {
		return out, nil
	}
	run := vault.RunScope(b.Front.ID)
	for _, s := range b.Front.Secrets {
		hv, ok := store.Get(vault.HarnessScope, s.Name)
		if !ok {
			continue
		}
		if rv, ok := store.Get(run, s.Name); ok && rv != hv {
			if err := store.Set(run, s.Name, hv); err != nil {
				return out, fmt.Errorf("projectrun: refresh run-scope secret %s failed", s.Name)
			}
		}
	}
	return out, nil
}
