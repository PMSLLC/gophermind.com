// Package proxy is the run's loopback forward proxy: an allowlist, per-node
// attribution, a log with no paths or bodies, critical-host failure tracking
// and the three-in-a-row streak. It is policy and audit; containment is the
// sandbox.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
)

// Rule allows one host. Host is exact, or "*.suffix" (any subdomain of suffix,
// never suffix itself); lowercase, no port.
//
// Port 0 means any port (a brief network entry has no port, so it keeps that
// meaning). A non-zero Port pins the rule to that port: BuildRules pins every
// provider-derived rule to the base URL's port (80 or 443 by scheme when the URL
// has none), so a provider host that is an IP literal such as the Mac mini cannot
// be used to reach its ssh, MySQL or Postgres ports.
type Rule struct {
	Host     string
	Critical bool
	Port     int
}

// errInvalidRuleHost is the one error for a rule host that fails validation; it
// names nothing from the input.
var errInvalidRuleHost = errors.New("proxy: rule has an invalid host")

var goHosts = []string{"proxy.golang.org", "sum.golang.org"}

// BuildRules is the allowlist: the brief's network entries (any port), the host
// of every provider base URL pinned to that URL's port (80 or 443 by scheme when
// it has none) and the Go module hosts unless noGoHosts. Duplicates merge and
// Critical is OR-ed. Errors name the index only.
func BuildRules(network []brief.Network, providerBaseURLs []string, noGoHosts bool) ([]Rule, error) {
	type key struct {
		host string
		port int
	}
	m := map[key]bool{}
	add := func(h string, port int, crit bool) {
		k := key{h, port}
		m[k] = m[k] || crit
	}
	for _, n := range network {
		h, ok := normalizeRuleHost(n.Host)
		if !ok {
			return nil, errInvalidRuleHost
		}
		add(h, 0, n.Critical)
	}
	for i, raw := range providerBaseURLs {
		bad := fmt.Errorf("proxy: provider base URL %d is not a valid URL", i)
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return nil, bad
		}
		h, ok := normalizeHost(u.Hostname())
		if !ok {
			return nil, bad
		}
		port := 0
		switch ps := u.Port(); {
		case ps != "":
			port, err = strconv.Atoi(ps)
			if err != nil || port < 1 || port > 65535 {
				return nil, bad
			}
		case strings.EqualFold(u.Scheme, "https"):
			port = 443
		case strings.EqualFold(u.Scheme, "http"):
			port = 80
		default:
			return nil, bad
		}
		add(h, port, false)
	}
	if !noGoHosts {
		for _, h := range goHosts {
			add(h, 0, false)
		}
	}
	out := make([]Rule, 0, len(m))
	for k, c := range m {
		out = append(out, Rule{Host: k.host, Critical: c, Port: k.port})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Port < out[j].Port
	})
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

// matchRules reports whether host (already normalized) on port is allowed and
// whether the matching rule is critical. A rule with a non-zero Port matches that port only. An IP literal matches only an exact rule.
func matchRules(rules []Rule, host string, port int) (allowed, critical bool) {
	ip := isIPLiteral(host)
	for _, r := range rules {
		if r.Port != 0 && r.Port != port {
			continue
		}
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
