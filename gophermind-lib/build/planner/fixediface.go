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
// backtick span of a directory bullet that is an exported function signature
// (`Make(s string, max int) string`), and func main() for a directory under
// cmd/. A span that does not parse as a signature with every parameter named
// is a call example (`Make(s, 0)`) and fixes nothing. Order: bullet order,
// then span order.
func fixedInterfaces(arch string) []fixedIface {
	var out []fixedIface
	for _, line := range strings.Split(arch, "\n") {
		dir := archBulletDir(line)
		if dir == "" {
			continue
		}
		for _, s := range backtickSpanRE.FindAllStringSubmatch(line, -1) {
			if m := fixedSpanRE.FindStringSubmatch(s[1]); m != nil && namedSignature("func "+s[1]) {
				out = append(out, fixedIface{Dir: dir, Name: m[1], Signature: "func " + s[1]})
			}
		}
		if isEntryPackage(dir) {
			out = append(out, fixedIface{Dir: dir, Name: "main", Signature: "func main()"})
		}
	}
	return out
}

// namedSignature is true when sig parses as a function signature whose every
// parameter has a name.
func namedSignature(sig string) bool {
	fd, err := parseSignature(sig)
	if err != nil {
		return false
	}
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			return false
		}
	}
	return true
}

// entryCall is the call the entry point makes, as the first cmd/ bullet of the
// Architecture section names it: its first pkg.Func whose pkg is the last
// element of an Architecture directory outside cmd/ ("cli", "Run"), so a
// standard library qualifier (`os.Args`) is skipped. Both are empty when
// there is none.
func entryCall(arch string) (pkg, name string) {
	pkgs := map[string]bool{}
	for _, d := range archDirs(arch) {
		if !isEntryPackage(d) {
			pkgs[path.Base(d)] = true
		}
	}
	for _, line := range strings.Split(arch, "\n") {
		if !isEntryPackage(archBulletDir(line)) {
			continue
		}
		for _, m := range archQualRE.FindAllStringSubmatch(line, -1) {
			if pkgs[m[1]] {
				return m[1], m[2]
			}
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

// componentDirs maps each component id of doc to its package directory, the
// module stripped.
func componentDirs(doc map[string]any) map[string]string {
	module, _ := doc["module"].(string)
	dirs := map[string]string{}
	for _, c := range objects(doc["components"]) {
		id, _ := c["id"].(string)
		if pkg, _ := c["package"].(string); id != "" && pkg != "" {
			dirs[id] = stripModule(module, pkg)
		}
	}
	return dirs
}

// applyFixedInterfaces holds the functions of the components whose package is
// dir (module stripped) to the interfaces fixed for dir. Methods never match.
// A function whose signature name equals a fixed name exactly matches it; with
// no exact match, the one function whose name equals it ignoring case does,
// and several such functions match none. A matched function gets the fixed
// signature, and with it the fixed name; each change is returned as
// "<id>: <old sig> -> <new sig>". A fixed interface no function in dir
// matches is missing.
func applyFixedInterfaces(doc map[string]any, dir string, ifs []fixedIface) (applied []string, missing []fixedIface) {
	module, _ := doc["module"].(string)
	compDirs := componentDirs(doc)
	for _, fi := range ifs {
		if stripModule(module, fi.Dir) != dir {
			continue
		}
		var exact, folded []map[string]any
		for _, f := range objects(doc["functions"]) {
			if c, _ := f["component"].(string); compDirs[c] != dir {
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

// fixedMissingNow applies the fixed interfaces of dir after the pass of
// component id (see applyFixedInterfaces) and returns the ones that are
// missing and must be asked of id now. While another component of dir comes
// later in the outline and is not done, nothing is asked: the sibling may
// declare it, and the check runs again after its pass. Only the last
// component of a directory, or a lone one, is asked.
func fixedMissingNow(doc map[string]any, id, dir string, ifs []fixedIface, done []string) (applied []string, missing []fixedIface) {
	applied, missing = applyFixedInterfaces(doc, dir, ifs)
	if len(missing) == 0 {
		return applied, nil
	}
	compDirs := componentDirs(doc)
	seen := false
	for _, c := range objects(doc["components"]) {
		cid, _ := c["id"].(string)
		switch {
		case cid == id:
			seen = true
		case seen && compDirs[cid] == dir && !contains(done, cid):
			return applied, nil
		}
	}
	return applied, missing
}

// unownedFixedInterfaces lists the fixed interfaces whose directory no
// component of doc has: nobody is asked to write them.
func unownedFixedInterfaces(doc map[string]any, ifs []fixedIface, module string) []fixedIface {
	owned := map[string]bool{}
	for _, d := range componentDirs(doc) {
		owned[d] = true
	}
	var out []fixedIface
	for _, fi := range ifs {
		if !owned[stripModule(module, fi.Dir)] {
			out = append(out, fi)
		}
	}
	return out
}

// unownedText is the warning for a fixed interface no component owns.
func unownedText(fi fixedIface) string {
	return "fixed_interface_unowned: " + fi.Dir + ": " + fi.Signature
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
// is the component's package compPkg: the same path, the same path once the
// module is stripped, the last element of one equal to the other in either
// direction (cli and internal/cli), or the package clause of the directory
// (main for cmd/).
func ownPackage(pkg, compPkg, module string) bool {
	dir := stripModule(module, compPkg)
	fnDir := stripModule(module, pkg)
	return pkg == compPkg || fnDir == dir || pkg == path.Base(dir) || path.Base(fnDir) == dir || pkg == PackageClause(dir)
}

// dropForeignFunctions takes out of a component reply the functions declared
// in another package, and a func main() of a component whose package is not a
// main package (a directory under cmd/ or the module root): the model wrote
// another component's functions, and merged here they would take that
// component's ids. It returns the reply and "<id> (package <pkg>)" for
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
	entry := PackageClause(stripModule(module, compPkg)) == "main"
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
