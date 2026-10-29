package planner

import (
	"regexp"
	"strings"
	"unicode"
)

// funcNameRE splits a function signature into what comes before its name
// ("func " or "func (s *Store) ") and the name.
var funcNameRE = regexp.MustCompile(`^(func\s+(?:\([^)]*\)\s*)?)([A-Za-z_][A-Za-z0-9_]*)`)

// exportCrossPackageUses capitalises the name of every function, or method, that
// another package's function uses, because Go cannot call an unexported name
// from another package. A model wrote main (package main) calling run in
// internal/cli; the program and the test of main could never compile. A name is
// left alone when the capitalised one already exists in its package. It returns
// the ids it renamed, in the order found.
func exportCrossPackageUses(doc map[string]any) []string {
	fns := objects(doc["functions"])
	byID := map[string]map[string]any{}
	taken := map[string]map[string]bool{} // package -> function names in use
	for _, f := range fns {
		id, _ := f["id"].(string)
		byID[id] = f
		pkg, _ := f["package"].(string)
		if m := funcNameRE.FindStringSubmatch(strField(f, "signature")); m != nil {
			if taken[pkg] == nil {
				taken[pkg] = map[string]bool{}
			}
			taken[pkg][m[2]] = true
		}
	}
	var renamed []string
	done := map[string]bool{}
	for _, f := range fns {
		fpkg, _ := f["package"].(string)
		for _, u := range strList(f["uses"]) {
			g := byID[u]
			if g == nil || done[u] {
				continue
			}
			if gpkg, _ := g["package"].(string); gpkg == fpkg {
				continue
			}
			sig := strField(g, "signature")
			loc := funcNameRE.FindStringSubmatchIndex(sig)
			if loc == nil {
				continue
			}
			name := sig[loc[4]:loc[5]]
			if first := []rune(name)[0]; unicode.IsUpper(first) || !unicode.IsLetter(first) {
				continue
			}
			up := strings.ToUpper(name[:1]) + name[1:]
			gpkg, _ := g["package"].(string)
			if taken[gpkg][up] {
				continue
			}
			g["signature"] = sig[:loc[4]] + up + sig[loc[5]:]
			taken[gpkg][up] = true
			done[u] = true
			renamed = append(renamed, u)
		}
	}
	return renamed
}
