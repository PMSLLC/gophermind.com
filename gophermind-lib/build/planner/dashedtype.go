package planner

import (
	"regexp"
	"strings"
)

var dashedTypeRE = regexp.MustCompile(`(?m)^(\s*type\s+)([A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)+)\b`)

// camelDashedTypeNames renames each type a declaration names with dashes, the
// way contract ids are written ("type slug-config struct"), to its camel-case
// Go name (slugConfig) everywhere in the declaration. Such a declaration never
// parses, and a model sent the same one on every repair. It returns the
// declaration and how many types it renamed.
func camelDashedTypeNames(decl string) (string, int) {
	ms := dashedTypeRE.FindAllStringSubmatch(decl, -1)
	for _, m := range ms {
		parts := strings.Split(m[2], "-")
		name := parts[0]
		for _, p := range parts[1:] {
			name += strings.ToUpper(p[:1]) + p[1:]
		}
		decl = regexp.MustCompile(`\b`+regexp.QuoteMeta(m[2])+`\b`).ReplaceAllString(decl, name)
	}
	return decl, len(ms)
}
