package planner

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
)

// errTypeDeclShape is the fixed text of a refused contract type declaration. It
// never quotes the declaration, so it is safe to hand back to a model as a
// repair hint.
var errTypeDeclShape = errors.New("a declaration may hold type declarations and methods only (no const, var, import or plain function); write an enum as a named type and let the functions that need its values use literals")

// TypeDeclShape accepts a contract type Decl only when it is one or more type
// declarations plus methods with bodies and no import. The executor writes the
// declaration verbatim into the repository and refuses anything else at Wave 0,
// so the planner applies the same rule when the contract is written and the
// model can still repair it.
func TypeDeclShape(decl string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "decl.go", "package p\n"+decl, parser.ParseComments)
	if err != nil {
		return errors.New("does not parse")
	}
	if len(f.Imports) > 0 {
		return errTypeDeclShape
	}
	types := 0
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				return errTypeDeclShape
			}
			types++
		case *ast.FuncDecl:
			if d.Recv == nil || d.Body == nil {
				return errTypeDeclShape
			}
		default:
			return errTypeDeclShape
		}
	}
	if types == 0 {
		return errTypeDeclShape
	}
	return nil
}
