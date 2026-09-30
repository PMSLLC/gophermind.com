package packer

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ImportPolicy is the one statement of which imports leaf code may use. Every
// caller asks this type and keeps no copy of the rules.
type ImportPolicy struct {
	Module string   // the target module path, e.g. "example.com/greeter"
	Deps   []string // module paths from dependencies.json
}

//go:embed stdlist.txt
var stdListText string

// stdPackages is every standard library import path, from `go list std` with
// internal and vendor packages left out. A test regenerates it and compares.
var stdPackages = func() map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Fields(stdListText) {
		m[l] = true
	}
	return m
}()

var pathSyntaxRE = regexp.MustCompile(`^[A-Za-z0-9._~/-]+$`)

// validPathSyntax refuses anything that is not a plain slash-separated import
// path: odd characters, empty, "." or ".." segments, leading or trailing slash.
func validPathSyntax(p string) bool {
	if !pathSyntaxRE.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func hasElem(p, elem string) bool {
	for _, e := range strings.Split(p, "/") {
		if e == elem {
			return true
		}
	}
	return false
}

// deniedStd are standard packages leaf code may not import. debug/... is
// handled as a prefix rule.
var deniedStd = []string{"os/exec", "syscall", "unsafe", "plugin", "runtime/cgo", "debug"}

func within(p, root string) bool {
	return root != "" && (p == root || strings.HasPrefix(p, root+"/"))
}

func (p ImportPolicy) allowed(path string) bool {
	if path == "C" || !validPathSyntax(path) {
		return false
	}
	if within(path, p.Module) {
		return true
	}
	for _, d := range deniedStd {
		if within(path, d) {
			return false
		}
	}
	if hasElem(path, "internal") || hasElem(path, "vendor") {
		return false
	}
	for _, d := range p.Deps {
		if within(path, d) {
			return true
		}
	}
	return stdPackages[path]
}

// Check returns the disallowed import paths, sorted and deduplicated. The
// paths are reply-supplied text: use them as data only and never put one in an
// error, event, log or file. Use Describe for anything that will be printed.
func (p ImportPolicy) Check(imports []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range imports {
		if seen[path] || p.allowed(path) {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// AllowedList renders the allowed sets for a failure message.
func (p ImportPolicy) AllowedList() string {
	parts := []string{"standard library (except os/exec, syscall, unsafe, plugin, debug/*, runtime/cgo)"}
	if p.Module != "" {
		parts = append(parts, p.Module+"/...")
	}
	for _, d := range p.Deps {
		parts = append(parts, d+"/...")
	}
	return strings.Join(parts, ", ")
}

// Describe returns "" when every import is allowed, otherwise a fixed-kind
// sentence with a count and the index of the first offending import. It never
// contains an import path.
func (p ImportPolicy) Describe(imports []string) string {
	n, first := 0, -1
	for i, path := range imports {
		if !p.allowed(path) {
			n++
			if first < 0 {
				first = i
			}
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d disallowed imports (first is import index %d)", n, first)
}
