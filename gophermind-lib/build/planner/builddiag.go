package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// BuildHints turns the compiler output of a failed build of an implementation
// into short, actionable lines for the next attempt: the declared fields of a
// contract struct the code got wrong, the declared signature of a contract
// function called with the wrong arguments, and the imports to delete. The
// planner's refusals of a test file say the same things (compilediag.go); here
// the output is the executor's own check of the model's code, which the model
// already sees whole, so names are printed as the compiler wrote them. A line
// is added only when the contract answers it; at most maxBuildHints lines come
// back, none when nothing in out is recognised.
func BuildHints(c *contract.Contracts, out string) []string {
	if c == nil {
		return nil
	}
	var hints []string
	seen := map[string]bool{}
	add := func(h string) {
		if h != "" && !seen[h] && len(hints) < maxBuildHints {
			seen[h] = true
			hints = append(hints, h)
		}
	}
	var unused []string
	for _, loc := range diagRE.FindAllStringSubmatchIndex(out, -1) {
		diag := out[loc[8]:loc[9]] + diagContinuation(out[loc[1]:])
		if m := unusedImportRE.FindStringSubmatch(diag); m != nil {
			if !contains(unused, m[1]) {
				unused = append(unused, m[1])
			}
			continue
		}
		if m := unknownFieldLitRE.FindStringSubmatch(diag); m != nil {
			add(structHint(c, m[2], m[1]))
			continue
		}
		if m := noFieldRE.FindStringSubmatch(diag); m != nil {
			add(structHint(c, m[1], noFieldName(diag)))
			continue
		}
		if m := argCountRE.FindStringSubmatch(diag); m != nil {
			add(callHint(c, m[2], m[4]))
		}
	}
	if len(unused) > 0 {
		quoted := make([]string, len(unused))
		for i, u := range unused {
			quoted[i] = `"` + u + `"`
		}
		add("Delete these imports, nothing in the file uses them: " + strings.Join(quoted, ", ") + ". Do not add a use just to keep an import.")
	}
	return hints
}

const maxBuildHints = 6

var (
	unusedImportRE = regexp.MustCompile(`^"([^"]+)" imported(?: as \S+)? and not used`)
	noFieldNameRE  = regexp.MustCompile(`^\S+ undefined \(type .+? has no field or method (\w+)`)
)

func noFieldName(diag string) string {
	if m := noFieldNameRE.FindStringSubmatch(diag); m != nil {
		return m[1]
	}
	return ""
}

// structHint names the declared fields of the contract struct that typ (T,
// *T or pkg.T as the compiler spells it) denotes, or "" when the contract has
// no such struct. field is the name the code used.
func structHint(c *contract.Contracts, typ, field string) string {
	if typ == "" {
		return ""
	}
	si, ok := contractStruct(c, "", typ)
	if !ok || len(si.fields) == 0 {
		return ""
	}
	h := fmt.Sprintf("%s declares only these fields: %s.", si.name, strings.Join(si.fields, ", "))
	if field != "" {
		h = fmt.Sprintf("%s has no field %s. ", si.name, field) + h
	}
	h += " Use those names; do not add or rename fields."
	if si.embedded {
		h += " The type also embeds other types."
	}
	return h
}

// callHint gives the declared signature of the contract function callee when
// the call passed the wrong number of arguments. A callee the contract does
// not declare (a helper the code wrote itself) gets the parameter list the
// compiler wants, when it printed one.
func callHint(c *contract.Contracts, callee, want string) string {
	name := callee
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	for _, fn := range c.Functions {
		f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+fn.Signature, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return fmt.Sprintf("The contract declares %s as: %s. Call it with exactly those parameters.", name, strings.TrimSpace(fn.Signature))
			}
		}
	}
	if want != "" {
		return fmt.Sprintf("%s takes %s: change the call to pass exactly those, or change the declaration of %s so the two agree.", name, want, name)
	}
	return ""
}
