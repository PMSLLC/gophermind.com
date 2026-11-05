package planner

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
)

// fixedIface is an interface the brief's Architecture section fixes: a
// function of the package in Dir with exactly this name and signature.
type fixedIface struct {
	Dir, Name, Signature string
}

var (
	backtickSpanRE = regexp.MustCompile("`([^`]+)`")
	fixedSpanRE    = regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)\((.*)\)\s*(.*)$`)
	methodSigRE    = regexp.MustCompile(`^\s*func\s*\(`)
)

// archBulletDir is the directory an Architecture bullet starts with, the way
// archDirs reads it; "" for a line that is not such a bullet.
func archBulletDir(line string) string {
	b := archBulletRE.FindStringSubmatch(line)
	if b == nil || strings.HasSuffix(b[1], ".go") || strings.ContainsAny(b[1], " ()") {
		return ""
	}
	return path.Clean(b[1])
}

// fixedInterfaces lists the interfaces the Architecture section fixes: every
// backtick span of a directory bullet that reads as an exported function
// signature (`Make(s string, max int) string`), and func main() for a
// directory under cmd/. Order: bullet order, then span order.
func fixedInterfaces(arch string) []fixedIface {
	var out []fixedIface
	for _, line := range strings.Split(arch, "\n") {
		dir := archBulletDir(line)
		if dir == "" {
			continue
		}
		for _, s := range backtickSpanRE.FindAllStringSubmatch(line, -1) {
			if m := fixedSpanRE.FindStringSubmatch(s[1]); m != nil {
				out = append(out, fixedIface{Dir: dir, Name: m[1], Signature: "func " + s[1]})
			}
		}
		if isEntryPackage(dir) {
			out = append(out, fixedIface{Dir: dir, Name: "main", Signature: "func main()"})
		}
	}
	return out
}

// entryCall is the call the entry point makes, as the first cmd/ bullet of the
// Architecture section names it: its first pkg.Func ("cli", "Run"). Both are
// empty when there is none.
func entryCall(arch string) (pkg, name string) {
	for _, line := range strings.Split(arch, "\n") {
		if !isEntryPackage(archBulletDir(line)) {
			continue
		}
		if m := archQualRE.FindStringSubmatch(line); m != nil {
			return m[1], m[2]
		}
		return "", ""
	}
	return "", ""
}

// fixedInterfacesText is one line per interface: "<Dir>: <Signature>".
func fixedInterfacesText(ifs []fixedIface) string {
	lines := make([]string, 0, len(ifs))
	for _, f := range ifs {
		lines = append(lines, f.Dir+": "+f.Signature)
	}
	return strings.Join(lines, "\n")
}

// applyFixedInterfaces holds the functions of component, whose package is dir,
// to the interfaces fixed for dir. Methods never match. A function whose
// signature name equals a fixed name exactly matches it; with no exact match,
// the one function whose name equals it ignoring case does, and several such
// functions match none. A matched function gets the fixed signature, and with
// it the fixed name; each change is returned as "<id>: <old sig> -> <new sig>".
// A fixed interface no function of the component matches is missing.
func applyFixedInterfaces(doc map[string]any, component, dir string, ifs []fixedIface) (applied []string, missing []fixedIface) {
	for _, fi := range ifs {
		if fi.Dir != dir {
			continue
		}
		var exact, folded []map[string]any
		for _, f := range objects(doc["functions"]) {
			if f["component"] != component {
				continue
			}
			sig, _ := f["signature"].(string)
			m := funcSigNameRE.FindStringSubmatch(sig)
			if m == nil || methodSigRE.MatchString(sig) {
				continue
			}
			switch {
			case m[1] == fi.Name:
				exact = append(exact, f)
			case strings.EqualFold(m[1], fi.Name):
				folded = append(folded, f)
			}
		}
		var f map[string]any
		switch {
		case len(exact) > 0:
			f = exact[0]
		case len(folded) == 1:
			f = folded[0]
		default:
			missing = append(missing, fi)
			continue
		}
		if sig, _ := f["signature"].(string); sig != fi.Signature {
			f["signature"] = fi.Signature
			id, _ := f["id"].(string)
			applied = append(applied, id+": "+sig+" -> "+fi.Signature)
		}
	}
	return applied, missing
}

// fixedDefectText asks for the fixed interfaces a component left out.
func fixedDefectText(missing []fixedIface) string {
	sigs := make([]string, 0, len(missing))
	for _, f := range missing {
		sigs = append(sigs, f.Signature)
	}
	return "the brief's Architecture section fixes these interfaces for this package and the component does not declare them: " +
		strings.Join(sigs, "; ") + ". Write each with exactly that name and signature"
}

// ownPackage is true when pkg, as a function of a component reply writes it,
// is the component's package compPkg: the same path, its last element, the
// same path once the module is stripped, or the package clause of the
// directory (main for cmd/).
func ownPackage(pkg, compPkg, module string) bool {
	dir := stripModule(module, compPkg)
	return pkg == compPkg || pkg == path.Base(compPkg) || stripModule(module, pkg) == dir || pkg == PackageClause(dir)
}

// dropForeignFunctions takes out of a component reply the functions declared
// in another package, and a func main() of a component outside cmd/: the
// model wrote another component's functions, and merged here they would take
// that component's ids. It returns the reply and "<id> (package <pkg>)" for
// each function dropped. A reply that does not parse is returned as it is;
// the merge reports it.
func dropForeignFunctions(text, compPkg, module string) (string, []string) {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil || raw["functions"] == nil {
		return text, nil
	}
	var fns []any
	if json.Unmarshal(raw["functions"], &fns) != nil {
		return text, nil
	}
	entry := isEntryPackage(stripModule(module, compPkg))
	var kept []any
	var dropped []string
	for _, x := range fns {
		f, ok := x.(map[string]any)
		if !ok {
			kept = append(kept, x)
			continue
		}
		pkg, _ := f["package"].(string)
		sig, _ := f["signature"].(string)
		isMain := strings.Join(strings.Fields(sig), " ") == "func main()"
		if (pkg != "" && !ownPackage(pkg, compPkg, module)) || (isMain && !entry) {
			id, _ := f["id"].(string)
			dropped = append(dropped, id+" (package "+pkg+")")
			continue
		}
		kept = append(kept, x)
	}
	if len(dropped) == 0 {
		return text, nil
	}
	if kept == nil {
		kept = []any{}
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return text, nil
	}
	raw["functions"] = b
	out, err := json.Marshal(raw)
	if err != nil {
		return text, nil
	}
	return string(out), dropped
}

// foreignDroppedText is the warning for the functions dropped from one reply.
func foreignDroppedText(component string, dropped []string) string {
	return "component_foreign_function_dropped: " + component + ": " + strings.Join(dropped, ", ")
}
