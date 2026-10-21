package main

import (
	"net/http"
	"time"

	"gophermind/gophermind-lib/build/provider"
	"gophermind/gophermind-lib/build/settings"
)

// providerHTTPClient is the client every provider call and probe uses: no
// redirects, no proxy environment, and a backstop timeout one minute past the
// longest call timeout in the settings (the router's per-call context is the
// real bound).
func providerHTTPClient(cfg *settings.Config) *http.Client {
	return provider.NewHTTPClient(cfg.MaxCallTimeout() + time.Minute)
}
