package packer

import (
	"sort"
	"strings"
)

// ImportPolicy is the one statement of which imports leaf code may use. Every
// caller asks this type and keeps no copy of the rules.
type ImportPolicy struct {
	Module string   // the target module path, e.g. "example.com/greeter"
	Deps   []string // module paths from dependencies.json
}

// stdTopLevel is the set of standard library top-level path elements.
// internal and vendor are deliberately absent.
var stdTopLevel = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields("archive bufio bytes cmp compress container context crypto database debug embed encoding errors expvar flag fmt go hash html image index io iter log maps math mime net os path plugin reflect regexp runtime slices sort strconv strings structs sync syscall testing text time unicode unique unsafe weak") {
		m[n] = true
	}
	return m
}()

// deniedStd are standard packages leaf code may not import. debug/... is
// handled as a prefix rule.
var deniedStd = []string{"os/exec", "syscall", "unsafe", "plugin", "runtime/cgo", "debug"}

func within(p, root string) bool {
	return root != "" && (p == root || strings.HasPrefix(p, root+"/"))
}

func (p ImportPolicy) allowed(path string) bool {
	if path == "C" || path == "" {
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
	for _, d := range p.Deps {
		if within(path, d) {
			return true
		}
	}
	elems := strings.Split(path, "/")
	if !stdTopLevel[elems[0]] {
		return false
	}
	for _, e := range elems {
		if e == "internal" || e == "vendor" || e == "" {
			return false
		}
	}
	return true
}

// Check returns the disallowed import paths, sorted and deduplicated.
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
