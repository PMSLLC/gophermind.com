package planner

import (
	"path"
	"strings"
)

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

// moveMainToEntryPoint puts each func main() under cmd/. It moves one that is
// not into the first component whose package is under cmd/, with the file
// main.go in that folder. A model put main in the cli component and left the
// entry-point component empty; another gave the entry-point component the
// package "main", so there was no cmd/ at all. Only a main function under cmd/
// builds into a program, so no acceptance command could run one. When no
// component is under cmd/ one is made from the module's name: the main
// function's own component if it holds nothing else, else a new "entry-point".
// With main already under cmd/, nothing moves. It returns the ids it moved.
func moveMainToEntryPoint(doc map[string]any) []string {
	var mains []map[string]any
	for _, f := range objects(doc["functions"]) {
		sig, _ := f["signature"].(string)
		if strings.Join(strings.Fields(sig), " ") != "func main()" {
			continue
		}
		if p, _ := f["package"].(string); strings.HasPrefix(path.Clean(p), "cmd/") {
			continue
		}
		mains = append(mains, f)
	}
	if len(mains) == 0 {
		return nil
	}
	entryID, entryPkg := "", ""
	for _, c := range objects(doc["components"]) {
		if p, _ := c["package"].(string); strings.HasPrefix(path.Clean(p), "cmd/") {
			entryID, _ = c["id"].(string)
			entryPkg = path.Clean(p)
			break
		}
	}
	if entryID == "" {
		entryPkg = "cmd/" + programName(doc)
		entryID = entryComponentFor(doc, mains[0], entryPkg)
	}
	var moved []string
	for _, f := range mains {
		f["component"], f["package"], f["file"] = entryID, entryPkg, entryPkg+"/main.go"
		id, _ := f["id"].(string)
		moved = append(moved, id)
	}
	return moved
}

// entryComponentFor returns the id of the component that will hold the program:
// main's own when nothing else is in it, retargeted to pkg; otherwise a new one.
func entryComponentFor(doc map[string]any, main map[string]any, pkg string) string {
	own, _ := main["component"].(string)
	shared := false
	for _, f := range objects(doc["functions"]) {
		if c, _ := f["component"].(string); c == own && f["id"] != main["id"] {
			shared = true
		}
	}
	comps := objects(doc["components"])
	for _, c := range comps {
		if id, _ := c["id"].(string); id == own && own != "" && !shared {
			c["package"] = pkg
			return id
		}
	}
	id := "entry-point"
	for taken := true; taken; {
		taken = false
		for _, c := range comps {
			if c["id"] == id {
				id += "-main"
				taken = true
			}
		}
	}
	list, _ := doc["components"].([]any)
	doc["components"] = append(list, map[string]any{"id": id, "package": pkg, "exports": []any{}, "integration_tests": []any{}})
	return id
}

// programName is the name of the program built from cmd/: the last element of
// the module path, lower case with anything but letters and digits as a dash.
func programName(doc map[string]any) string {
	m, _ := doc["module"].(string)
	last := m
	if i := strings.LastIndex(m, "/"); i >= 0 {
		last = m[i+1:]
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(last) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "app"
	}
	return name
}

// contractRepairs are the ids repairContractShape changed, by repair.
type contractRepairs struct {
	Moved, Aligned, Refiled, Dropped, Exported, Qualified []string
}

// repairContractShape applies, in order, the repairs a model's contract needs
// before anything is built from it: main goes under cmd/, a function takes its
// component's package, a file goes into its package's folder, standard
// library names leave uses, a function used from another package is
// exported, and a type used bare from another package is exported and
// qualified. It runs on the contract proper and on what
// the coverage fill step adds, since both come from a model.
func repairContractShape(doc map[string]any) contractRepairs {
	return contractRepairs{
		Moved:     moveMainToEntryPoint(doc),
		Aligned:   alignFunctionsToComponents(doc),
		Refiled:   moveFilesIntoPackageDir(doc),
		Dropped:   dropStdlibUses(doc),
		Exported:  exportCrossPackageUses(doc),
		Qualified: qualifyCrossPackageTypes(doc),
	}
}
