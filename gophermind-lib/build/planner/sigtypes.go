package planner

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"regexp"
	"strings"
	"unicode"

	"gophermind/gophermind-lib/build/contract"
)

var goIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// sigType is a Go type name that function signatures use and no type of the
// contract declares. Such a function cannot compile, so a test of it cannot
// either: the planner asks for the type while the contract is still being
// written, not at the test-writer an hour later.
type sigType struct {
	Name       string   // the Go name, as written in the signature
	Users      []string // ids of the functions whose signature uses it
	Components []string // the components of those functions
}

// undeclaredSignatureTypes lists, in order of first use, the type names that
// signatures (parameters, results and receivers) use and the types of doc
// never declare. A name from the standard library, a package of the plan, a
// builtin or a type parameter is not listed.
func undeclaredSignatureTypes(doc map[string]any) []sigType {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	var c contract.Contracts
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	declared, pkgs := contractTypeNames(&c), planPackageNames(&c)
	var out []sigType
	at := map[string]int{}
	for _, f := range c.Functions {
		for _, name := range signatureUnresolved(f.Signature, &c, declared, pkgs) {
			i, ok := at[name]
			if !ok {
				i = len(out)
				at[name] = i
				out = append(out, sigType{Name: name})
			}
			out[i].Users = appendOnce(out[i].Users, f.ID)
			out[i].Components = appendOnce(out[i].Components, f.Component)
		}
	}
	return out
}

// signatureUnresolved is the type names sig uses that nothing declares: the
// ones deriveGoTypes finds in the parameters and results, plus the receiver's.
func signatureUnresolved(sig string, c *contract.Contracts, declared, pkgs map[string]bool) []string {
	g, err := deriveGoTypes(sig, c)
	if err != nil {
		return nil
	}
	out := append([]string(nil), g.Unresolved...)
	fd, err := parseSignature(sig)
	if err != nil {
		return nil
	}
	if fd.Recv != nil {
		for _, f := range fd.Recv.List {
			for _, name := range unresolvedTypeNames(f.Type, declared, pkgs) {
				out = appendOnce(out, name)
			}
		}
	}
	// A package qualifier (zz.Thing) is not a type of this plan: it has its own
	// repair by kind, so it is left out here.
	qualifiers := map[string]bool{}
	ast.Inspect(fd, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				qualifiers[id.Name] = true
			}
		}
		return true
	})
	var types []string
	for _, name := range out {
		if !qualifiers[name] {
			types = append(types, name)
		}
	}
	return types
}

func appendOnce(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// typeIDFor is the id suggested for a type: the Go name in lower case with
// dashes, greetState to greet-state.
func typeIDFor(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sigTypesText is the repair prompt's list of undeclared signature types, one
// per line, with the functions that use them. It quotes names that came from a
// parsed signature only, and bounds the list.
func sigTypesText(list []sigType) string {
	var lines []string
	for _, s := range list {
		if len(lines) == maxUnresolvedInPrompt {
			break
		}
		if !goIdentRE.MatchString(s.Name) || len(s.Name) > maxBoundedID {
			continue
		}
		var users []string
		for _, u := range s.Users {
			if idSyntaxRE.MatchString(u) && len(u) <= maxBoundedID && len(users) < maxUnresolvedInError {
				users = append(users, u)
			}
		}
		lines = append(lines, fmt.Sprintf("%s (used by %s): write a type with id %s whose decl declares %s",
			s.Name, strings.Join(users, ", "), typeIDFor(s.Name), s.Name))
	}
	return strings.Join(lines, "\n")
}

// boundedName is a type name for a warning: the name when it is a Go identifier
// of sane length, else its length.
func boundedName(name string) string {
	if goIdentRE.MatchString(name) && len(name) <= maxBoundedID {
		return name
	}
	return fmt.Sprintf("(a name of %d bytes)", len(name))
}

// boundedIDs is up to n of ids that pass the id syntax.
func boundedIDs(ids []string, n int) []string {
	var out []string
	for _, id := range ids {
		if idSyntaxRE.MatchString(id) && len(id) <= maxBoundedID && len(out) < n {
			out = append(out, id)
		}
	}
	return out
}
