package planner

import "regexp"

// newModuleRE is a Go module path as `go mod init` accepts it for a local
// module: slash separated elements of letters, digits and . _ ~ -, none empty,
// the first character of each a letter or digit (so no "..", no leading "/",
// "./" or "~/").
var newModuleRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*(/[A-Za-z0-9][A-Za-z0-9._~-]*)*$`)

const maxModulePath = 200

// validModulePath reports whether s can be the module of a new repository.
// A model once wrote the repository's own file path, which go mod init refuses
// after the whole plan is already made.
func validModulePath(s string) bool {
	return len(s) > 0 && len(s) <= maxModulePath && newModuleRE.MatchString(s)
}
