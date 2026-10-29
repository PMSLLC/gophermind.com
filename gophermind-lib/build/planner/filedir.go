package planner

import (
	"path"
	"strings"
)

// moveFilesIntoPackageDir puts the file of each type and function whose
// package is a folder path ("internal/slug") into that folder when the model
// named another one ("slug/make.go"). Go takes a package's import path from its
// folder, so such a file built a different package and every caller that
// imported the declared one failed to build. A package without a slash ("main",
// "slug") names no folder and is left alone, as is a bare file name (RepoFile).
// It returns the ids it moved.
func moveFilesIntoPackageDir(doc map[string]any) []string {
	module, _ := doc["module"].(string)
	var moved []string
	for _, key := range []string{"functions", "types"} {
		for _, o := range objects(doc[key]) {
			pkg, _ := o["package"].(string)
			file, _ := o["file"].(string)
			if pkg == "" || !strings.Contains(file, "/") {
				continue
			}
			dir := stripModule(module, pkg)
			if !strings.Contains(dir, "/") || strings.Contains(dir, ".") || path.Dir(file) == dir {
				continue
			}
			o["file"] = path.Join(dir, path.Base(file))
			id, _ := o["id"].(string)
			moved = append(moved, id)
		}
	}
	return moved
}
