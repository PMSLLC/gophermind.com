package planner

import (
	"fmt"
	"regexp"
	"sort"
)

// Dependency is one third-party Go module the planned code needs.
type Dependency struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Purpose string `json:"purpose"`
}

// fileDependencies is dependencies.json, written by the contract stage and
// covered by the approval hash.
const fileDependencies = "dependencies.json"

var (
	// A dotted host first, so a standard library path (fmt) or an internal
	// path (internal/x) is refused.
	depModuleRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z][a-z0-9]*(/[A-Za-z0-9._~-]+)*$`)
	depVersionRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

// ParseDependencies validates the dependencies of an outline reply and
// returns them sorted by module. An error names an index and a kind, never
// the value, because the value came from a model.
func ParseDependencies(raw []map[string]any) ([]Dependency, error) {
	out := make([]Dependency, 0, len(raw))
	seen := map[string]bool{}
	for i, o := range raw {
		mod, _ := o["module"].(string)
		ver, _ := o["version"].(string)
		purpose, _ := o["purpose"].(string)
		switch {
		case !depModuleRE.MatchString(mod):
			return nil, fmt.Errorf("dependencies[%d]: module path (%d bytes) is not plausible", i, len(mod))
		case !depVersionRE.MatchString(ver):
			return nil, fmt.Errorf("dependencies[%d]: version (%d bytes) is not a pinned semver", i, len(ver))
		case purpose == "":
			return nil, fmt.Errorf("dependencies[%d]: purpose is empty", i)
		case seen[mod]:
			return nil, fmt.Errorf("dependencies[%d]: module (%d bytes) is listed twice", i, len(mod))
		}
		seen[mod] = true
		out = append(out, Dependency{Module: mod, Version: ver, Purpose: purpose})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Module < out[b].Module })
	return out, nil
}
