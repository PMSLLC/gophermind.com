package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// The package qualifier rule. The executor makes a stub from a function's
// signature and must turn every qualifier (the Q of Q.Name) into an import.
// The planner asks the same question of the same text with the same code, so a
// signature the planner lets through is one the stub generator can resolve.

// StdQualifiers maps the package qualifier a signature or type declaration
// commonly uses to its import path. The allow decision is not made here: the
// resolved paths go to packer.ImportPolicy.Check. os/exec, syscall and unsafe
// are listed only so that the policy, not a resolution failure, refuses them.
// "template" (text or html) and "rand" (math or crypto) are ambiguous; see
// ResolveQualifier.
var StdQualifiers = map[string]string{
	"bytes": "bytes", "context": "context", "json": "encoding/json", "errors": "errors", "fmt": "fmt",
	"io": "io", "fs": "io/fs", "log": "log", "net": "net", "http": "net/http", "url": "net/url",
	"os": "os", "path": "path", "filepath": "path/filepath", "regexp": "regexp", "sort": "sort",
	"strconv": "strconv", "strings": "strings", "sync": "sync", "atomic": "sync/atomic",
	"testing": "testing", "time": "time", "unicode": "unicode", "utf8": "unicode/utf8",
	"sql": "database/sql", "bufio": "bufio", "math": "math", "sha256": "crypto/sha256",
	"hex": "encoding/hex", "base64": "encoding/base64", "multipart": "mime/multipart",
	"signal": "os/signal", "html": "html",
	"exec": "os/exec", "syscall": "syscall", "unsafe": "unsafe",
}

var versionElemRE = regexp.MustCompile(`^v[0-9]+$`)

// ResolveQualifier finds the import path of a package qualifier: contract
// packages (siblings) first, then the modules of dependencies.json (deps), then
// the standard library table. ok is false when it cannot be told, which
// includes the ambiguous qualifiers.
//
// A qualifier is taken to be the package it names even when the declaration
// shadows it with its own identifier, and rand is math/rand unless the text
// says otherwise. A wrong guess only produces a file that does not compile,
// which the build step reports; it can never widen what is imported.
func ResolveQualifier(q, src string, siblings map[string]string, deps []string) (string, bool) {
	if p, ok := siblings[q]; ok {
		return p, true
	}
	for _, d := range deps {
		last := path.Base(d)
		if versionElemRE.MatchString(last) {
			last = path.Base(path.Dir(d))
		}
		if last == q {
			return d, true
		}
	}
	if q == "rand" {
		// math/rand and crypto/rand share the qualifier; the text cannot say which.
		if strings.Contains(src, "crypto/rand") {
			return "", false
		}
		for _, only := range []string{"rand.Reader", "rand.Prime", "rand.Text"} {
			if strings.Contains(src, only) {
				return "", false
			}
		}
		return "math/rand", true
	}
	p, ok := StdQualifiers[q]
	return p, ok
}

// Qualifiers lists, sorted, the identifiers used as the Q of a selector Q.Name
// that the file does not declare itself.
func Qualifiers(f *ast.File) []string {
	quals := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Obj == nil {
				quals[x.Name] = true
			}
		}
		return true
	})
	names := make([]string, 0, len(quals))
	for q := range quals {
		names = append(names, q)
	}
	sort.Strings(names)
	return names
}

// stripModule drops the module path from the front of a contract package path
// that carries it ("csvstat/internal/stats" in module csvstat is
// "internal/stats"; the module itself is "."). Other paths are only cleaned.
func stripModule(module, pkg string) string {
	pkg = path.Clean(pkg)
	if module == "" || pkg == "." {
		return pkg
	}
	if pkg == module {
		return "."
	}
	if rest, ok := strings.CutPrefix(pkg, module+"/"); ok {
		return rest
	}
	return pkg
}

// FileDirIn is FileDir for a contract whose package paths may carry the module
// path: the module prefix is not part of a directory in the repository.
func FileDirIn(module, pkg, file string) string {
	if strings.Contains(file, "/") {
		return path.Dir(file)
	}
	if pkg == "" {
		return "."
	}
	return stripModule(module, pkg)
}

// siblingImports maps each contract package clause to its import path, the
// module path plus the directory of its file. The first declaration in
// contract order wins when two directories share a package name.
func siblingImports(module string, decls [][2]string) map[string]string {
	out := map[string]string{}
	for _, d := range decls {
		pkg, file := d[0], d[1]
		if pkg == "" {
			continue
		}
		clause := PackageClause(pkg)
		if _, ok := out[clause]; ok {
			continue
		}
		dir := FileDirIn(module, pkg, file)
		if dir == "." {
			out[clause] = module
			continue
		}
		out[clause] = module + "/" + dir
	}
	return out
}

// PackageImports maps each contract package clause (the last element of a
// directory-valued package) to its import path.
func PackageImports(c *contract.Contracts) map[string]string {
	var decls [][2]string
	for _, t := range c.Types {
		decls = append(decls, [2]string{t.Package, t.File})
	}
	for _, f := range c.Functions {
		decls = append(decls, [2]string{f.Package, f.File})
	}
	return siblingImports(c.Module, decls)
}

// docImports is PackageImports for a contract that is still a decoded document.
func docImports(doc map[string]any) map[string]string {
	var decls [][2]string
	for _, key := range []string{"types", "functions"} {
		for _, o := range objects(doc[key]) {
			pkg, _ := o["package"].(string)
			file, _ := o["file"].(string)
			decls = append(decls, [2]string{pkg, file})
		}
	}
	module, _ := doc["module"].(string)
	return siblingImports(module, decls)
}

// declNames are the type names a Go declaration declares.
func declNames(decl string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+decl+"\n", parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok {
			for _, s := range gd.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					out = append(out, ts.Name.Name)
				}
			}
		}
	}
	return out
}

// normalizeSignatureQualifiers rewrites, in every function signature of doc,
// Q.Name to Name when Q is the module name (the last element of the module path)
// or the last element of the function's own package, and Name is a type the
// contract declares in that same package. A same-package type never takes a
// qualifier, so the rewrite is unambiguous. A standard library name is never
// taken for a qualifier of the own package. It returns how many qualifiers were
// removed and at most maxDefaultedIDsShown examples (function id only).
func normalizeSignatureQualifiers(doc map[string]any) (int, []string) {
	module, _ := doc["module"].(string)
	modName := path.Base(module)
	same := map[string]map[string]bool{} // stripped package path -> type names
	for _, t := range objects(doc["types"]) {
		pkg, _ := t["package"].(string)
		decl, _ := t["decl"].(string)
		key := stripModule(module, pkg)
		if same[key] == nil {
			same[key] = map[string]bool{}
		}
		for _, n := range declNames(decl) {
			same[key][n] = true
		}
	}
	total := 0
	var ex []string
	for _, f := range objects(doc["functions"]) {
		sig, _ := f["signature"].(string)
		pkg, _ := f["package"].(string)
		id, _ := f["id"].(string)
		key := stripModule(module, pkg)
		own := path.Base(key)
		if own == "." {
			own = ""
		}
		const head = "package p\n"
		file, err := parser.ParseFile(token.NewFileSet(), "", head+sig, 0)
		if err != nil {
			continue
		}
		var cuts []int // offsets in sig of the "Q." to remove
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Obj != nil || !(x.Name == modName || x.Name == own) {
				return true
			}
			if _, std := StdQualifiers[x.Name]; std {
				return true
			}
			if same[key][sel.Sel.Name] {
				cuts = append(cuts, int(x.Pos())-1-len(head))
			}
			return true
		})
		if len(cuts) == 0 {
			continue
		}
		sort.Sort(sort.Reverse(sort.IntSlice(cuts)))
		out := sig
		for _, at := range cuts {
			dot := strings.IndexByte(out[at:], '.')
			out = out[:at] + out[at+dot+1:]
		}
		f["signature"] = out
		total += len(cuts)
		if len(ex) < maxDefaultedIDsShown {
			ex = append(ex, boundedID(id))
		}
	}
	return total, ex
}

// qualifierIssues lists the functions whose signature has a package qualifier
// the stub generator cannot resolve: not a package of this plan, not a module of
// dependencies.json, not a standard library package. The issue names the
// function and says "qualifier"; it never quotes the signature.
func qualifierIssues(doc map[string]any, deps []string) []schemaIssue {
	siblings := docImports(doc)
	var out []schemaIssue
	for i, f := range objects(doc["functions"]) {
		sig, _ := f["signature"].(string)
		file, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+sig, 0)
		if err != nil {
			continue
		}
		for _, q := range Qualifiers(file) {
			if _, ok := ResolveQualifier(q, sig, siblings, deps); !ok {
				id, _ := f["id"].(string)
				out = append(out, schemaIssue{list: "functions", index: i, id: id, field: "signature", kind: "qualifier"})
				break
			}
		}
	}
	return out
}

// qualifierText is what a prompt says about a node with such an issue.
const qualifierText = "has a package qualifier in its signature that is not a package of this plan, not a dependency and not a standard library package; a type of the function's own package is written without any qualifier. Write the signature again"

// RepoFile is file made repo-relative: a bare file name takes the directory of
// its package (FileDirIn); a name that already has a directory is left alone.
// It is idempotent.
func RepoFile(module, pkg, file string) string {
	if file == "" || strings.Contains(file, "/") {
		return file
	}
	return path.Join(FileDirIn(module, pkg, file), file)
}

// NormalizeContractFiles makes the file of every type and function of c
// repo-relative (see RepoFile) and returns how many it changed.
func NormalizeContractFiles(c *contract.Contracts) int {
	n := 0
	for i := range c.Types {
		t := &c.Types[i]
		if f := RepoFile(c.Module, t.Package, t.File); f != t.File {
			t.File, n = f, n+1
		}
	}
	for i := range c.Functions {
		fn := &c.Functions[i]
		if f := RepoFile(c.Module, fn.Package, fn.File); f != fn.File {
			fn.File, n = f, n+1
		}
	}
	return n
}

// normalizeFiles makes the file of every type and function of the decoded
// contract doc repo-relative. It returns the count and a few examples.
func normalizeFiles(doc map[string]any) (int, []string) {
	module, _ := doc["module"].(string)
	n := 0
	var ex []string
	for _, key := range []string{"types", "functions"} {
		for _, o := range objects(doc[key]) {
			pkg, _ := o["package"].(string)
			file, _ := o["file"].(string)
			f := RepoFile(module, pkg, file)
			if f == file {
				continue
			}
			o["file"] = f
			n++
			if id, _ := o["id"].(string); len(ex) < 3 {
				ex = append(ex, fmt.Sprintf("%s: %s -> %s", id, file, f))
			}
		}
	}
	return n, ex
}
