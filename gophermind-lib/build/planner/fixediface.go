package planner

import (
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
// to the interfaces fixed for dir. A function whose signature name equals a
// fixed name (ignoring case) gets the fixed signature, and with it the fixed
// name; each change is returned as "<id>: <old sig> -> <new sig>". A fixed
// interface no function of the component matches is missing.
func applyFixedInterfaces(doc map[string]any, component, dir string, ifs []fixedIface) (applied []string, missing []fixedIface) {
	for _, fi := range ifs {
		if fi.Dir != dir {
			continue
		}
		found := false
		for _, f := range objects(doc["functions"]) {
			if f["component"] != component {
				continue
			}
			sig, _ := f["signature"].(string)
			m := funcSigNameRE.FindStringSubmatch(sig)
			if m == nil || !strings.EqualFold(m[1], fi.Name) {
				continue
			}
			found = true
			if sig != fi.Signature {
				f["signature"] = fi.Signature
				id, _ := f["id"].(string)
				applied = append(applied, id+": "+sig+" -> "+fi.Signature)
			}
			break
		}
		if !found {
			missing = append(missing, fi)
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
