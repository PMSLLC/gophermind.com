package planner

import (
	"regexp"
	"strings"
)

var (
	archBulletRE  = regexp.MustCompile("^\\s*[-*]\\s+`([^`]+)`")
	archQualRE    = regexp.MustCompile(`\b([a-z][a-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)\b`)
	funcSigNameRE = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
)

// architectureUses adds the calls the brief's Architecture section states to
// the uses of the contract functions. A bullet that starts with a package of
// the contract (`internal/cli`), names one of that package's functions (`Run`)
// and calls a function of another contract package (`slug.Make`) makes the
// named function use it. A model left such a call out, so the implementer was
// never told the function existed, invented one, and the build failed in every
// attempt. With exactly one function in the package, it is the one meant. It
// returns the ids that gained a use.
func architectureUses(doc map[string]any, arch string) []string {
	module, _ := doc["module"].(string)
	fns := objects(doc["functions"])
	type fnRef struct {
		id, name, dir string
		f             map[string]any
	}
	var refs []fnRef
	for _, f := range fns {
		id, _ := f["id"].(string)
		pkg, _ := f["package"].(string)
		sig, _ := f["signature"].(string)
		m := funcSigNameRE.FindStringSubmatch(sig)
		if id == "" || pkg == "" || m == nil {
			continue
		}
		refs = append(refs, fnRef{id, m[1], stripModule(module, pkg), f})
	}
	var added []string
	for _, line := range strings.Split(arch, "\n") {
		b := archBulletRE.FindStringSubmatch(line)
		if b == nil {
			continue
		}
		dir := stripModule(module, b[1])
		var own []fnRef
		for _, r := range refs {
			if r.dir == dir {
				own = append(own, r)
			}
		}
		if len(own) == 0 {
			continue
		}
		var targets []string
		for _, q := range archQualRE.FindAllStringSubmatch(line, -1) {
			for _, r := range refs {
				if r.dir != dir && r.name == q[2] && PackageClause(r.dir) == q[1] {
					targets = append(targets, r.id)
				}
			}
		}
		if len(targets) == 0 {
			continue
		}
		var sources []fnRef
		if len(own) == 1 {
			sources = own
		} else {
			for _, r := range own {
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(r.name) + `\b`).MatchString(line) {
					sources = append(sources, r)
				}
			}
		}
		for _, s := range sources {
			uses := strList(s.f["uses"])
			grew := false
			for _, t := range targets {
				if !contains(uses, t) {
					uses = append(uses, t)
					grew = true
				}
			}
			if grew {
				s.f["uses"] = anyStrings(uses)
				added = append(added, s.id)
			}
		}
	}
	return added
}
