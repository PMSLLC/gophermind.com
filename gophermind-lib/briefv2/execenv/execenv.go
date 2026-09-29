// Package execenv builds the exact environment handed to an exec.Cmd running a
// node's commands: the brief's declared env (defaults, then config overrides),
// the declared secrets (values from the vault), the proxy variables, and the
// node id. Nothing from the harness process environment is ever included, and
// a secret name never gets a default or an override.
package execenv

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Inputs struct {
	// Env is the brief's declared non-secret environment.
	Env []brief.EnvVar
	// Overrides comes from harness config ([v2.env]); every key must be a
	// declared env name.
	Overrides map[string]string
	// Secrets are the declared secret names; SecretValues returns NAME=value
	// entries for exactly those names (normally vault.Env bound to a scope).
	Secrets      []string
	SecretValues func(names []string) ([]string, error)
	// ProxyURL is optional until the proxy exists; empty omits the proxy vars.
	ProxyURL string
	NodeID   string
}

// Build returns the sorted NAME=value environment for exec.Cmd.Env.
func Build(in Inputs) ([]string, error) {
	secrets := map[string]bool{}
	for _, s := range in.Secrets {
		secrets[s] = true
	}
	declared := map[string]bool{}
	for _, e := range in.Env {
		if secrets[e.Name] {
			return nil, fmt.Errorf("ENV_SECRET_OVERLAP: %s", e.Name)
		}
		if declared[e.Name] {
			return nil, fmt.Errorf("execenv: env %s is declared twice", e.Name)
		}
		declared[e.Name] = true
	}
	for name := range in.Overrides {
		if secrets[name] {
			return nil, fmt.Errorf("execenv: %s is a secret and cannot be set from config", name)
		}
		if !declared[name] {
			return nil, fmt.Errorf("execenv: override for undeclared env %s", name)
		}
	}
	if !idRE.MatchString(in.NodeID) {
		return nil, fmt.Errorf("execenv: invalid node id %q", in.NodeID)
	}

	var out []string
	for _, e := range in.Env {
		if v, ok := in.Overrides[e.Name]; ok {
			out = append(out, e.Name+"="+v)
		} else if e.Default != nil {
			out = append(out, e.Name+"="+*e.Default)
		}
	}
	if len(in.Secrets) > 0 {
		if in.SecretValues == nil {
			return nil, errors.New("execenv: secrets are declared but no SecretValues source was given")
		}
		vals, err := in.SecretValues(in.Secrets)
		if err != nil {
			return nil, err
		}
		if len(vals) != len(in.Secrets) {
			return nil, fmt.Errorf("execenv: secret source returned %d entries for %d declared secrets", len(vals), len(in.Secrets))
		}
		for _, kv := range vals {
			name, _, ok := strings.Cut(kv, "=")
			if !ok || !secrets[name] {
				return nil, errors.New("execenv: secret source returned a malformed or undeclared entry")
			}
			out = append(out, kv)
		}
	}
	if in.ProxyURL != "" {
		out = append(out, "HTTP_PROXY="+in.ProxyURL, "HTTPS_PROXY="+in.ProxyURL)
	}
	out = append(out, "GOPHERMIND_NODE="+in.NodeID)
	sort.Strings(out)
	return out, nil
}
