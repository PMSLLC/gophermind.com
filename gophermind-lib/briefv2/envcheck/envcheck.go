// Package envcheck holds the environment checks shared by `brief run
// --check-env`, the run start and /project: reachability of the provider base
// URLs, tools on the toolchain PATH, loopback hosts, free disk space and a
// clean working tree.
package envcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/gitenv"
)

// DefaultProbeTimeout bounds one reachability probe of a provider base URL.
const DefaultProbeTimeout = 3 * time.Second

// DefaultMinFreeBytes is the free space the module cache's file system must have.
const DefaultMinFreeBytes uint64 = 2 << 30

// ProbeResult is what ResolveBaseURLs found for one provider of the tier chains.
type ProbeResult struct {
	Provider string
	Host     string // host name only of the base URL that answered
	Answered bool
	Fallback bool // a base_url_fallbacks entry answered, not base_url
}

// ResolveBaseURLs probes every provider the tier chains use: base_url first,
// then base_url_fallbacks in order. The first that answers within
// timeout is used. A fallback that would change who sees the prompts (a
// public host for a private provider, or any public host under
// privacy.mode private_only) is skipped without being dialled, even when the
// config was not validated. It returns a copy of cfg with those base URLs, so
// the override lasts for the process and the config file is never rewritten; a
// provider nothing answered for keeps its base_url.
func ResolveBaseURLs(ctx context.Context, cfg *settings.Config, client *http.Client, timeout time.Duration) (*settings.Config, []ProbeResult) {
	c := *cfg
	c.Providers = append([]settings.ProviderConfig(nil), cfg.Providers...)
	var res []ProbeResult
	seen := map[string]bool{}
	for _, tier := range settings.Tiers {
		for _, entry := range cfg.Models[tier] {
			name, _, ok := settings.SplitEntry(entry)
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			for i, p := range c.Providers {
				if p.Name != name {
					continue
				}
				r := ProbeResult{Provider: name}
				bases := []string{p.BaseURL}
				for _, fb := range p.BaseURLFallbacks {
					if cfg.FallbackAllowed(p, fb) {
						bases = append(bases, fb)
					}
				}
				for j, base := range bases {
					if ProbeBaseURL(ctx, client, base, timeout) {
						r.Answered, r.Fallback = true, j > 0
						if u, err := url.Parse(base); err == nil {
							r.Host = u.Hostname()
						}
						c.Providers[i].BaseURL = base
						break
					}
				}
				res = append(res, r)
			}
		}
	}
	return &c, res
}

// ProbeBaseURL says whether base answers, within timeout in all: GET
// /api/version on its origin answers with 200, or GET <base>/models answers
// 2xx, or 401 or 403 (a server that wants a key is there). Anything else, a
// redirect, a 404 or a 5xx, is not an answer.
func ProbeBaseURL(ctx context.Context, client *http.Client, base string, timeout time.Duration) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	targets := []struct {
		url string
		ok  func(code int) bool
	}{
		{u.Scheme + "://" + u.Host + "/api/version", func(code int) bool { return code == http.StatusOK }},
		{strings.TrimRight(base, "/") + "/models", func(code int) bool {
			return code/100 == 2 || code == http.StatusUnauthorized || code == http.StatusForbidden
		}},
	}
	for _, t := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		if err != nil {
			return false
		}
		resp, err := c.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if t.ok(resp.StatusCode) {
			return true
		}
	}
	return false
}

// EnvNotes is what the report's environment section says about the providers:
// the host that answered, host only.
func EnvNotes(res []ProbeResult) []string {
	var notes []string
	for _, r := range res {
		if r.Answered {
			notes = append(notes, fmt.Sprintf("provider %s: base url host %s answered", r.Provider, SafeLine(r.Host)))
		}
	}
	return notes
}

// SafeLine keeps the first line of s, printable ASCII only, at most 160 bytes.
func SafeLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b = append(b, byte(r))
		if len(b) == 160 {
			break
		}
	}
	return string(b)
}

func LookInPath(name, path string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// DirtyOutsideTests counts changed paths that are neither the test files the
// planner placed (_state/test_files.json) nor under .gophermind/. An empty
// runDir means no test files are allowed.
func DirtyOutsideTests(repo, runDir string) (int, error) {
	allowed := map[string]bool{}
	if runDir != "" {
		if raw, err := os.ReadFile(filepath.Join(runDir, "_state", "test_files.json")); err == nil {
			var files []string
			if json.Unmarshal(raw, &files) == nil {
				for _, f := range files {
					allowed[filepath.ToSlash(f)] = true
				}
			}
		}
	}
	// gitenv.Command drops every inherited GIT_* variable (a hook exports
	// GIT_DIR), so git reads the repository it is given and no other.
	cmd := gitenv.Command(repo, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.sshCommand=",
		"status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
	raw, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range strings.Split(string(raw), "\x00") {
		if len(e) < 4 {
			continue
		}
		p := e[3:]
		if allowed[p] || p == ".gophermind" || strings.HasPrefix(p, ".gophermind/") {
			continue
		}
		n++
	}
	return n, nil
}

// LoopbackAddr returns host:port when val is a URL whose host is loopback.
func LoopbackAddr(val string) (string, bool) {
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"postgres": "5432", "postgresql": "5432", "mysql": "3306", "redis": "6379", "http": "80", "https": "443"}[u.Scheme]
	}
	if port == "" {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// ModuleCacheFree is the free space on the file system that holds the module
// cache (or its nearest existing parent).
func ModuleCacheFree(modCache string) (uint64, error) {
	p := modCache
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		p = filepath.Join(home, p[2:])
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return freeBytes(p)
		}
		next := filepath.Dir(p)
		if next == p {
			return 0, os.ErrNotExist
		}
		p = next
	}
}
