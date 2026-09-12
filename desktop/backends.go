package main

import (
	"fmt"
	"sync"
)

// BackendKind distinguishes how a backend is reached, which is also the order
// of how much has to be trusted to use one.
type BackendKind string

const (
	// BackendLocal is the server embedded in this process. It needs no
	// network beyond loopback and works with no connectivity at all.
	BackendLocal BackendKind = "local"

	// BackendURL is a gophermind server reached by ordinary HTTP at a base
	// URL, typically over a VPN. No new dependency and no tunnel.
	BackendURL BackendKind = "url"

	// BackendTunnel is a gophermind server reached through a gocloak tunnel
	// by service name. Not implemented yet; named here so the registry's
	// shape does not have to change when it arrives.
	BackendTunnel BackendKind = "tunnel"
)

// Backend is one gophermind server this desktop can run sessions against.
//
// Token is the credential for THAT server and is deliberately not part of
// anything the frontend can read: see backendRegistry.Public and the router,
// which swaps the frontend's own token for this one on the way upstream. A
// remote backend's token authorizes shell execution on another machine, so
// handing it to a WebView would turn a page-level scripting bug into remote
// code execution rather than local.
type Backend struct {
	Name    string
	Kind    BackendKind
	BaseURL string
	Token   string
}

// PublicBackend is the view of a backend the frontend is allowed to see. It
// carries no credential.
type PublicBackend struct {
	Name    string      `json:"name"`
	Kind    BackendKind `json:"kind"`
	Default bool        `json:"default"`
}

// backendRegistry holds the backends this desktop knows about. The first one
// added is the default, which is what an unprefixed request routes to.
//
// It is safe for concurrent use: the router reads it per request while
// configuration changes can add to it.
type backendRegistry struct {
	mu   sync.RWMutex
	list []Backend
}

// Add registers a backend. The first backend added becomes the default.
// A duplicate name is an error rather than a silent replacement, since two
// backends answering to one name would make it ambiguous which machine a
// command ran on.
func (r *backendRegistry) Add(b Backend) error {
	if b.Name == "" {
		return fmt.Errorf("backend needs a name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.list {
		if existing.Name == b.Name {
			return fmt.Errorf("backend %q is already registered", b.Name)
		}
	}
	r.list = append(r.list, b)
	return nil
}

// Get returns the backend with this name.
func (r *backendRegistry) Get(name string) (Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.list {
		if b.Name == name {
			return b, true
		}
	}
	return Backend{}, false
}

// Default returns the backend an unprefixed request routes to, which is the
// first one registered.
func (r *backendRegistry) Default() (Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.list) == 0 {
		return Backend{}, false
	}
	return r.list[0], true
}

// Public returns the credential-free view of every backend, for the frontend.
func (r *backendRegistry) Public() []PublicBackend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PublicBackend, 0, len(r.list))
	for i, b := range r.list {
		out = append(out, PublicBackend{Name: b.Name, Kind: b.Kind, Default: i == 0})
	}
	return out
}
