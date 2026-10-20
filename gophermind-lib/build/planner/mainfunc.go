package planner

import (
	"fmt"
	"go/ast"
	"regexp"
	"sort"
)

// dirRE is a package directory safe to echo: path elements of letters, digits,
// dots, dashes and underscores.
var dirRE = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// mainDefects lists the directories of the contract's main packages (the
// module root or a directory under cmd/) that hold at least one function but
// none named main with the signature func main(). Go cannot build such a
// package: "function main is undeclared in the main package".
func mainDefects(doc map[string]any) []string {
	has := map[string]bool{}  // package has a function
	good := map[string]bool{} // package has func main()
	for _, f := range objects(doc["functions"]) {
		pkg, _ := f["package"].(string)
		if pkg == "" {
			continue
		}
		has[pkg] = true
		sig, _ := f["signature"].(string)
		if fd, err := parseSignature(sig); err == nil && isMainFunc(fd) {
			good[pkg] = true
		}
	}
	var out []string
	for pkg := range has {
		if PackageClause(pkg) == "main" && !good[pkg] {
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

// isMainFunc is true for func main() with no receiver, parameters or results.
func isMainFunc(fd *ast.FuncDecl) bool {
	return fd.Recv == nil && fd.Name.Name == "main" &&
		len(fd.Type.Params.List) == 0 && (fd.Type.Results == nil || len(fd.Type.Results.List) == 0)
}

// mainDefectText is the defect for the main package in dir, by kind: it names
// the directory and never quotes a reply.
func mainDefectText(dir string) string {
	shown := fmt.Sprintf("<%d bytes>", len(dir))
	if dirRE.MatchString(dir) && len(dir) <= maxBoundedID {
		shown = dir
	}
	return "package " + shown + " is a main package but declares no function main; add a function with id fn-main named main and signature func main() that calls the wiring functions and exits with the code of the run function"
}
