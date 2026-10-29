package planner

import (
	"regexp"
	"sort"
	"unicode"
)

// qualifyCrossPackageTypes repairs a signature that uses a contract type of
// another package by its bare name: parseFlags in internal/cli returned
// slugConfig, declared unexported in internal/slug, so neither the function
// nor its test could compile. The type is exported (renamed in its declaration
// and in every signature of its own package), the foreign signature gets the
// package qualifier (slug.SlugConfig), and the type's id joins that function's
// uses so its import is known. It returns the ids of the functions it
// qualified.
func qualifyCrossPackageTypes(doc map[string]any) []string {
	module, _ := doc["module"].(string)
	type typeRef struct {
		dir string
		t   map[string]any
	}
	byName := map[string]typeRef{}
	ownNames := map[string]map[string]bool{} // dir -> type names declared there
	for _, t := range objects(doc["types"]) {
		pkg, _ := t["package"].(string)
		dir := stripModule(module, pkg)
		for _, n := range declNames(strField(t, "decl")) {
			byName[n] = typeRef{dir, t}
			if ownNames[dir] == nil {
				ownNames[dir] = map[string]bool{}
			}
			ownNames[dir][n] = true
		}
	}
	var changed []string
	for _, f := range objects(doc["functions"]) {
		pkg, _ := f["package"].(string)
		dir := stripModule(module, pkg)
		sig := strField(f, "signature")
		grew := false
		names := make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			ref := byName[name]
			if ref.dir == dir || ownNames[dir][name] || !bareIdentRE(name).MatchString(sig) {
				continue
			}
			exported := exportName(name)
			if exported != name {
				renameTypeEverywhere(doc, module, ref.dir, ref.t, name, exported)
				delete(byName, name)
				byName[exported] = ref
			}
			sig = bareIdentRE(name).ReplaceAllString(sig, "${1}"+PackageClause(ref.dir)+"."+exported)
			f["signature"] = sig
			if id, _ := ref.t["id"].(string); id != "" {
				uses := strList(f["uses"])
				if !contains(uses, id) {
					f["uses"] = anyStrings(append(uses, id))
				}
			}
			grew = true
		}
		if grew {
			id, _ := f["id"].(string)
			changed = append(changed, id)
		}
	}
	return changed
}

// bareIdentRE matches name as a whole identifier that no package qualifier
// precedes; the first group is the character before it.
func bareIdentRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(name) + `\b`)
}

func exportName(name string) string {
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// renameTypeEverywhere renames a type in its declaration and in the bare uses
// of every signature of its own package.
func renameTypeEverywhere(doc map[string]any, module, dir string, t map[string]any, from, to string) {
	re := bareIdentRE(from)
	t["decl"] = re.ReplaceAllString(strField(t, "decl"), "${1}"+to)
	for _, f := range objects(doc["functions"]) {
		pkg, _ := f["package"].(string)
		if stripModule(module, pkg) == dir {
			f["signature"] = re.ReplaceAllString(strField(f, "signature"), "${1}"+to)
		}
	}
}
