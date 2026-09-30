package executor

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
)

// StubSource is a leaf's stub file: the package clause, the imports it is
// given, the signature, and a body that is exactly panic("gm: not implemented").
// A panic terminates, so no returns follow and go vet stays quiet (spec S3).
// Method signatures are supported. An error never quotes the signature.
func StubSource(pkg, signature string, imports []string) ([]byte, error) {
	if !validPackageName(pkg) {
		return nil, errors.New("stub: the package name is not a valid identifier")
	}
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	if len(imports) > 0 {
		sorted := append([]string(nil), imports...)
		sort.Strings(sorted)
		b.WriteString("import (\n")
		for _, i := range sorted {
			fmt.Fprintf(&b, "\t%q\n", i)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(signature + " {\n\tpanic(\"gm: not implemented\")\n}\n")
	f, err := parser.ParseFile(token.NewFileSet(), "stub.go", b.String(), 0)
	if err != nil {
		return nil, errors.New("stub: the signature does not parse")
	}
	const notOne = "stub: the signature is not a single function signature"
	var fns, importSpecs int
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			// A signature that ends without a body (func A()\nfunc B()) leaves the earlier function bodiless.
			if d.Body == nil || len(d.Body.List) != 1 {
				return nil, errors.New(notOne)
			}
			fns++
		case *ast.GenDecl:
			if d.Tok != token.IMPORT {
				return nil, errors.New(notOne)
			}
			importSpecs += len(d.Specs)
		default:
			return nil, errors.New(notOne)
		}
	}
	if fns != 1 || importSpecs != len(imports) {
		return nil, errors.New(notOne)
	}
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, errors.New("stub: the stub does not format")
	}
	return src, nil
}

// StubPath is the repo-relative (slash) path of a leaf's stub file.
func StubPath(dir, id string) string { return path.Join(dir, "zz_gm_stub_"+id+".go") }

// stubFor derives the imports a leaf's signature needs and builds its stub.
func stubFor(c *contract.Contracts, pol packer.ImportPolicy, l *Leaf) ([]byte, error) {
	imports, err := deriveImports(l.ID, l.Signature, packageImports(c), pol)
	if err != nil {
		return nil, err
	}
	src, err := StubSource(l.Package, l.Signature, imports)
	if err != nil {
		return nil, err
	}
	if err := checkAssembled(src, l.Package, pol); err != nil {
		return nil, fmt.Errorf("stub %s: the assembled file is not allowed (%w)", l.ID, err)
	}
	return src, nil
}
