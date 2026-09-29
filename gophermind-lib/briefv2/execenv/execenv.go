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

var (
	idRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	nameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

	toolchainNames = map[string]bool{"PATH": true, "HOME": true, "GOCACHE": true, "GOMODCACHE": true, "GOPATH": true, "TMPDIR": true}
)

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
	// ProxyURL: empty omits the proxy variables and exists only for tests and
	// offline runs. Once the network proxy exists (build item 12) the executor
	// must always set it, otherwise a wiring bug runs commands unproxied.
	ProxyURL string
	NodeID   string
	// Toolchain is harness-owned: PATH, HOME, GOCACHE, GOMODCACHE, GOPATH and
	// TMPDIR, filled from harness config so the executor's own test command
	// can run (decision E5). It deliberately widens the handoff's literal list
	// of what may reach a command. Build never reads os.Environ; nil or empty
	// gives the strict handoff environment. A brief cannot declare these names
	// because they are reserved.
	Toolchain map[string]string
}

// Build returns the sorted NAME=value environment for exec.Cmd.Env.
func Build(in Inputs) ([]string, error) {
	for _, s := range in.Secrets {
		if !nameRE.MatchString(s) {
			return nil, fmt.Errorf("execenv: invalid variable name %q", s)
		}
	}
	for _, e := range in.Env {
		if !nameRE.MatchString(e.Name) {
			return nil, fmt.Errorf("execenv: invalid variable name %q", e.Name)
		}
	}
	for k, v := range in.Toolchain {
		if !toolchainNames[k] {
			return nil, fmt.Errorf("execenv: toolchain variable %s is not allowed", k)
		}
		if v == "" {
			return nil, fmt.Errorf("execenv: toolchain variable %s is empty", k)
		}
	}
	secrets := map[string]bool{}
	for _, s := range in.Secrets {
		secrets[s] = true
	}

	// Check for duplicate secret names before calling SecretValues
	seen := map[string]bool{}
	for _, s := range in.Secrets {
		if seen[s] {
			return nil, fmt.Errorf("execenv: secret %s is declared twice", s)
		}
		seen[s] = true
	}

	// Check for reserved names in secrets
	for _, s := range in.Secrets {
		if brief.IsReservedName(s) {
			return nil, fmt.Errorf("execenv: %s is reserved for the harness", s)
		}
	}

	declared := map[string]bool{}
	for _, e := range in.Env {
		// Check for reserved names in env
		if brief.IsReservedName(e.Name) {
			return nil, fmt.Errorf("execenv: %s is reserved for the harness", e.Name)
		}
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

		// Track which secret names have been seen in the source result
		seenInSource := map[string]int{}
		for _, kv := range vals {
			name, _, ok := strings.Cut(kv, "=")
			if !ok {
				return nil, errors.New("execenv: secret source returned a malformed entry")
			}
			if !secrets[name] {
				return nil, errors.New("execenv: secret source returned an undeclared entry")
			}
			seenInSource[name]++
			if seenInSource[name] > 1 {
				return nil, fmt.Errorf("execenv: secret source returned %s twice", name)
			}
			out = append(out, kv)
		}

		// Check that all declared secrets were returned
		for _, s := range in.Secrets {
			if seenInSource[s] == 0 {
				return nil, fmt.Errorf("execenv: secret source did not return %s", s)
			}
		}
	}
	if in.ProxyURL != "" {
		out = append(out, "HTTP_PROXY="+in.ProxyURL, "HTTPS_PROXY="+in.ProxyURL)
	}
	for k, v := range in.Toolchain {
		out = append(out, k+"="+v)
	}
	out = append(out, "GOPHERMIND_NODE="+in.NodeID)
	sort.Strings(out)
	return out, nil
}
