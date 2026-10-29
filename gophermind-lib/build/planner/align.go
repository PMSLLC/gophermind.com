package planner

import "path"

// alignFunctionsToComponents gives a function the package of the component that
// owns it, and moves its file into that package's folder (the base name is
// kept), when the two packages differ. A model gave an entry-point component the
// package cmd/slugify and its function the package "main" at main/main.go, so no
// program was built from cmd/ and every acceptance command that runs it looked
// vacuous. A function whose package already matches is left alone, including a
// file in the wrong folder, which other checks report. A function with no
// component, or a component with no package, is left alone. It returns the ids
// it changed, in order.
func alignFunctionsToComponents(doc map[string]any) []string {
	pkgOf := map[string]string{}
	for _, c := range objects(doc["components"]) {
		id, _ := c["id"].(string)
		if p, _ := c["package"].(string); id != "" && p != "" {
			pkgOf[id] = p
		}
	}
	var changed []string
	for _, f := range objects(doc["functions"]) {
		comp, _ := f["component"].(string)
		want, ok := pkgOf[comp]
		if !ok {
			continue
		}
		pkg, _ := f["package"].(string)
		if pkg == want {
			continue
		}
		f["package"] = want
		if file, _ := f["file"].(string); file != "" {
			f["file"] = path.Join(want, path.Base(file))
		}
		id, _ := f["id"].(string)
		changed = append(changed, id)
	}
	return changed
}
