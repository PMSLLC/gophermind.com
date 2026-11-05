package planner

import (
	"encoding/json"
	"path"
	"strings"
)

// archDirs is the directory of every Architecture bullet, in order, without
// duplicates.
func archDirs(arch string) []string {
	var dirs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(arch, "\n") {
		b := archBulletRE.FindStringSubmatch(line)
		if b == nil {
			continue
		}
		d := path.Clean(b[1])
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// mapComponentPackages turns the component packages a model wrote as Go
// package names ("slug", "main") into the directories the brief's Architecture
// section names. In order: a module prefix is stripped; a name equal to the
// last element of exactly one Architecture directory becomes that directory;
// main becomes the one Architecture directory under cmd/. A package that
// already is one of the directories, or matches none or several, is left
// alone. It returns "<id>: <old> -> <new>" for each change.
func mapComponentPackages(doc map[string]any, arch, module string) []string {
	dirs := archDirs(arch)
	isDir := map[string]bool{}
	var cmdDirs []string
	for _, d := range dirs {
		isDir[d] = true
		if strings.HasPrefix(d, "cmd/") {
			cmdDirs = append(cmdDirs, d)
		}
	}
	var changes []string
	for _, comp := range objects(doc["components"]) {
		id, _ := comp["id"].(string)
		old, _ := comp["package"].(string)
		if old == "" {
			continue
		}
		pkg := stripModule(module, old)
		if !isDir[pkg] {
			var match []string
			for _, d := range dirs {
				if path.Base(d) == pkg {
					match = append(match, d)
				}
			}
			switch {
			case len(match) == 1:
				pkg = match[0]
			case pkg == "main" && len(cmdDirs) == 1:
				pkg = cmdDirs[0]
			}
		}
		if pkg != old {
			comp["package"] = pkg
			changes = append(changes, id+": "+old+" -> "+pkg)
		}
	}
	return changes
}

// mapOutlinePackages applies mapComponentPackages to the components of an
// outline reply before it is merged, so the merge, its duplicate check and the
// contract validation all see directories. A reply that does not parse is
// returned as it is; the merge reports it.
func mapOutlinePackages(doc map[string]any, text, arch string) (string, []string) {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil || raw["components"] == nil {
		return text, nil
	}
	var comps []any
	if json.Unmarshal(raw["components"], &comps) != nil {
		return text, nil
	}
	module, _ := doc["module"].(string)
	if doc == nil {
		var m struct {
			Module string `json:"module"`
		}
		_ = json.Unmarshal([]byte(text), &m)
		module = m.Module
	}
	changes := mapComponentPackages(map[string]any{"components": comps}, arch, module)
	if len(changes) == 0 {
		return text, nil
	}
	b, err := json.Marshal(comps)
	if err != nil {
		return text, nil
	}
	raw["components"] = b
	out, err := json.Marshal(raw)
	if err != nil {
		return text, nil
	}
	return string(out), changes
}
