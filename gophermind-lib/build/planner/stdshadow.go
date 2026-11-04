package planner

import (
	"fmt"
	"path"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// stdShadow refuses a contract package whose last path element is the name of
// a standard library package. ResolveQualifier prefers a contract package over
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
		if std, ok := StdQualifiers[base]; ok {
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
