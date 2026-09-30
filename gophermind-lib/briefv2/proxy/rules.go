// Package proxy is the run's loopback forward proxy: an allowlist, per-node
// attribution, a log with no paths or bodies, critical-host failure tracking
// and the three-in-a-row streak. It is policy and audit; containment is the
// sandbox.
package proxy

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
)

// Rule allows one host. Host is exact, or "*.suffix" (any subdomain of suffix,
// never suffix itself); lowercase, no port.
type Rule struct {
	Host     string
	Critical bool
}

var goHosts = []string{"proxy.golang.org", "sum.golang.org"}

// BuildRules is the allowlist: the brief's network entries, the host of every
// provider base URL (port dropped) and the Go module hosts unless noGoHosts.
// Duplicates merge and Critical is OR-ed. Errors name the index only.
func BuildRules(network []brief.Network, providerBaseURLs []string, noGoHosts bool) ([]Rule, error) {
	m := map[string]bool{}
	add := func(h string, crit bool) {
		m[h] = m[h] || crit
	}
	for i, n := range network {
		h, ok := normalizeRuleHost(n.Host)
		if !ok {
			return nil, fmt.Errorf("proxy: network entry %d has an invalid host", i)
		}
		add(h, n.Critical)
	}
	for i, raw := range providerBaseURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return nil, fmt.Errorf("proxy: provider base URL %d is not a valid URL", i)
		}
		h, ok := normalizeHost(u.Hostname())
		if !ok {
			return nil, fmt.Errorf("proxy: provider base URL %d is not a valid URL", i)
		}
		add(h, false)
	}
	if !noGoHosts {
		for _, h := range goHosts {
			add(h, false)
		}
	}
	out := make([]Rule, 0, len(m))
	for h, c := range m {
		out = append(out, Rule{Host: h, Critical: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, nil
}

// normalizeRuleHost validates a rule host: an exact host or "*.suffix" where
// the suffix has at least two labels.
func normalizeRuleHost(h string) (string, bool) {
	h = strings.ToLower(strings.TrimSpace(h))
	if strings.HasPrefix(h, "*.") {
		s, ok := normalizeHost(h[2:])
		if !ok || !strings.Contains(s, ".") || strings.Contains(s, ":") || isIPLiteral(s) {
			return "", false
		}
		return "*." + s, true
	}
	return normalizeHost(h)
}

func isIPLiteral(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// normalizeHost lowercases a host (no port), drops one trailing dot and
// canonicalizes IP literals (IPv4-mapped IPv6 becomes IPv4). Anything
// malformed, zoned, or shaped like a non-canonical numeric IP (decimal, hex or
// octal encodings, short forms) is rejected.
func normalizeHost(h string) (string, bool) {
	h = strings.ToLower(h)
	if h == "" || len(h) > 253 {
		return "", false
	}
	if a, err := netip.ParseAddr(h); err == nil {
		if a.Zone() != "" {
			return "", false
		}
		return a.Unmap().String(), true
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return "", false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return "", false
			}
		}
	}
	last := labels[len(labels)-1]
	if last[0] >= '0' && last[0] <= '9' {
		return "", false // a TLD never starts with a digit: decimal, hex, octal and short IP forms
	}
	return h, true
}

// forbiddenNets are destinations a hostname may never resolve to.
var forbiddenNets = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"224.0.0.0/4", "240.0.0.0/4",
	"192.0.2.0/24", "192.88.99.0/24", "198.51.100.0/24", "203.0.113.0/24",
	"::/96", "2002::/16", "64:ff9b::/96", "100::/64", "fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

// forbiddenIP reports whether ip is loopback, private, link-local (including
// cloud metadata), CGNAT, unspecified, multicast or reserved, after unmapping
// IPv4-mapped IPv6.
func forbiddenIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	a = a.Unmap().WithZone("")
	for _, p := range forbiddenNets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// matchRules reports whether host (already normalized) is allowed and whether
// the matching rule is critical. An IP literal matches only an exact rule.
func matchRules(rules []Rule, host string) (allowed, critical bool) {
	ip := isIPLiteral(host)
	for _, r := range rules {
		if strings.HasPrefix(r.Host, "*.") {
			if ip {
				continue
			}
			if strings.HasSuffix(host, r.Host[1:]) && len(host) > len(r.Host)-1 {
				allowed = true
				critical = critical || r.Critical
			}
			continue
		}
		if r.Host == host {
			allowed = true
			critical = critical || r.Critical
		}
	}
	return allowed, critical
}

// criticalDenied reports whether a denied request to host counts against a
// critical rule: the rule host, minus any "*." prefix, equals host or is a
// dot-suffix of it.
func criticalDenied(rules []Rule, host string) bool {
	for _, r := range rules {
		if !r.Critical {
			continue
		}
		h := strings.TrimPrefix(r.Host, "*.")
		if host == h || (!isIPLiteral(host) && strings.HasSuffix(host, "."+h)) {
			return true
		}
	}
	return false
}
