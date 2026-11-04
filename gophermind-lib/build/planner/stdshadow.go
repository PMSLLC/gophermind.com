package planner

import (
	"fmt"
	"path"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// deepStdShadows are the qualifiers of standard library packages below the top
// level that a model means as the standard library when it writes them. Other
// deep names (user, list, mail, ring, heap) more often mean the contract's own
// package, so refusing them would cost the planner a retry for nothing.
var deepStdShadows = map[string]bool{
	"http": true, "url": true, "json": true, "filepath": true, "httptest": true,
	"httputil": true, "utf8": true, "atomic": true, "exec": true, "slog": true,
	"sql": true, "csv": true, "xml": true, "base64": true, "hex": true, "rand": true,
}

// stdShadow refuses a contract package whose last path element is the name of
// a top-level standard library package, or one of the deep packages listed in
// deepStdShadows. ResolveQualifier prefers a contract package over
// the standard library, so a test that follows the prompt imports the module's
// package under that name and finds none of the standard library's names in it.
func stdShadow(c *contract.Contracts) error {
	check := func(pkg, id string) error {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" || pkg == "." {
			return nil
		}
		base := path.Base(pkg)
		// base is a StdQualifiers key here, so it is safe to print unquoted.
		if std, ok := StdQualifiers[base]; ok && (!strings.Contains(std, "/") || deepStdShadows[base]) {
			return fmt.Errorf("contract: package %s is named like the standard library package %s, which tests import by that qualifier (declared by %s); rename the directory and its package to a name no standard library package uses", base, std, boundedID(id))
		}
		return nil
	}
	for _, x := range c.Components {
		if err := check(x.Package, x.ID); err != nil {
			return err
		}
	}
	for _, x := range c.Types {
		if err := check(x.Package, x.ID); err != nil {
			return err
		}
	}
	for _, x := range c.Functions {
		if err := check(x.Package, x.ID); err != nil {
			return err
		}
	}
	return nil
}
