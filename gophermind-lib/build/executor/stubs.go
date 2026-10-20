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

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/packer"
)

// StubExitStatus is the exit status of a process that ran a stub. A Go panic
// exits with 2, which is also the usual usage-error status, so an acceptance
// test that only checks for 2 would pass against a stub. 86 is distinctive.
const StubExitStatus = 86

// StubMessage is what a stub prints to stderr (and the text of its panic).
const StubMessage = "gm: not implemented"

// stubBody is a stub's body: write the message, exit with StubExitStatus, and
// a trailing panic so a function with results still compiles. The os import is
// aliased gmos so no name in the package or the signature can shadow it.
// The helper lives in the stub file itself, not in a shared support file:
// nothing globs zz_gm_stub_* at landing, and leaves run in private trees, so
// a shared file could not be removed when "the last stub of the package" goes.
// Removing the stub file removes the helper with it.
const stubBody = "\tgmos.Stderr.WriteString(\"" + StubMessage + "\\n\")\n\tgmos.Exit(86)\n\tpanic(\"" + StubMessage + "\")"

// StubSource is a leaf's stub file: the package clause, the imports it is
// given, the signature, and a body that prints StubMessage and exits with
// StubExitStatus (stubBody). The trailing panic terminates, so no returns
// follow and go vet stays quiet (spec S3).
// Method signatures are supported. An error never quotes the signature.
func StubSource(pkg, signature string, imports []string) ([]byte, error) {
	return stubSource(pkg, signature, imports, false)
}

// stubSource builds the stub. plain leaves out the exit (panic only) so the
// assembled-file policy check, which refuses process control, can run on it.
func stubSource(pkg, signature string, imports []string, plain bool) ([]byte, error) {
	if !validPackageName(pkg) {
		return nil, errors.New("stub: the package name is not a valid identifier")
	}
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	if len(imports) > 0 || !plain {
		sorted := append([]string(nil), imports...)
		sort.Strings(sorted)
		b.WriteString("import (\n")
		for _, i := range sorted {
			fmt.Fprintf(&b, "\t%q\n", i)
		}
		if !plain {
			b.WriteString("\tgmos \"os\"\n")
		}
		b.WriteString(")\n\n")
	}
	body := "\tpanic(\"" + StubMessage + "\")"
	nStmts, nImports := 1, len(imports)
	if !plain {
		body, nStmts, nImports = stubBody, 3, nImports+1
	}
	b.WriteString(signature + " {\n" + body + "\n}\n")
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
			if d.Body == nil || len(d.Body.List) != nStmts {
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
	if fns != 1 || importSpecs != nImports {
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
	plain, err := stubSource(l.Package, l.Signature, imports, true)
	if err != nil {
		return nil, err
	}
	if err := checkAssembled(plain, l.Package, pol); err != nil {
		return nil, fmt.Errorf("stub %s: the assembled file is not allowed (%w)", l.ID, err)
	}
	return StubSource(l.Package, l.Signature, imports)
}
