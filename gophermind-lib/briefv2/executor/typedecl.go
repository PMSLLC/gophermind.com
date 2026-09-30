package executor

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/pathsafe"
)

// stdQualifiers maps the package qualifier a signature or type declaration
// commonly uses to its import path. The allow decision is not made here: the
// resolved paths go to packer.ImportPolicy.Check. os/exec, syscall and unsafe
// are listed only so that the policy, not a resolution failure, refuses them.
// "template" (text or html) and "rand" (math or crypto) are ambiguous; see
// resolveQualifier.
var stdQualifiers = map[string]string{
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

// packageImports maps each contract package name to its import path, the
// module path plus the directory of its file. The first declaration in
// contract order wins when two directories share a package name.
func packageImports(c *contract.Contracts) map[string]string {
	out := map[string]string{}
	add := func(pkg, file string) {
		if pkg == "" {
			return
		}
		if _, ok := out[pkg]; ok {
			return
		}
		dir := path.Dir(file)
		if dir == "." {
			out[pkg] = c.Module
			return
		}
		out[pkg] = c.Module + "/" + dir
	}
	for _, t := range c.Types {
		add(t.Package, t.File)
	}
	for _, f := range c.Functions {
		add(f.Package, f.File)
	}
	return out
}

// parseErrAt is the kind of parse failure as "line:col", never the message,
// which can quote source text. prefix is the number of lines put before the
// source.
func parseErrAt(err error, prefix int) (line, col int) {
	var list scanner.ErrorList
	if el, ok := err.(scanner.ErrorList); ok {
		list = el
	}
	if len(list) == 0 {
		return 0, 0
	}
	p := list[0].Pos
	l := p.Line - prefix
	if l < 1 {
		l = 1
	}
	return l, p.Column
}

// resolveQualifier finds the import path of a package qualifier: contract
// packages first, then the modules of dependencies.json, then the standard
// library table. ok is false when it cannot be told, which includes the
// ambiguous qualifiers.
func resolveQualifier(q, src string, siblings map[string]string, pol packer.ImportPolicy) (string, bool) {
	if p, ok := siblings[q]; ok {
		return p, true
	}
	for _, d := range pol.Deps {
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
	p, ok := stdQualifiers[q]
	return p, ok
}

// deriveImports finds the import paths a declaration or signature needs. It
// only resolves qualifiers; pol.Check makes the allow decision. Errors never
// quote source text.
func deriveImports(id, src string, siblings map[string]string, pol packer.ImportPolicy) ([]string, error) {
	const head = "package p\n"
	f, err := parser.ParseFile(token.NewFileSet(), "decl.go", head+src, 0)
	if err != nil {
		line, col := parseErrAt(err, 1)
		return nil, fmt.Errorf("type %s: declaration does not parse (%d:%d)", id, line, col)
	}
	if len(f.Imports) > 0 {
		return nil, fmt.Errorf("type %s: declaration does not parse (1:1)", id)
	}
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
	seen := map[string]bool{}
	var imports []string
	for _, q := range names {
		p, ok := resolveQualifier(q, src, siblings, pol)
		if !ok {
			return nil, fmt.Errorf("type %s: cannot resolve package qualifier (%d bytes)", id, len(src))
		}
		if !seen[p] {
			seen[p] = true
			imports = append(imports, p)
		}
	}
	if len(pol.Check(imports)) > 0 {
		return nil, fmt.Errorf("type %s: import not allowed (%d bytes)", id, len(src))
	}
	sort.Strings(imports)
	return imports, nil
}

type typeFile struct {
	pkg     string
	first   string // id of the first type, for messages
	decls   []string
	imports map[string]bool
}

// WriteTypes writes each contract Type's file. pol is Plan.Policy(): every
// import a declaration needs is checked with pol.Check, so the executor keeps
// no copy of the import rules. Every type is validated before anything is
// written, so a failing type leaves no file behind.
func WriteTypes(repo string, c *contract.Contracts, pol packer.ImportPolicy) ([]string, error) {
	siblings := packageImports(c)
	files := map[string]*typeFile{}
	var order []string
	for _, t := range c.Types {
		if _, err := pathsafe.ResolveSource(repo, t.File); err != nil {
			return nil, fmt.Errorf("type %s: %w", t.ID, err)
		}
		imps, err := deriveImports(t.ID, t.Decl, siblings, pol)
		if err != nil {
			return nil, err
		}
		tf := files[t.File]
		if tf == nil {
			tf = &typeFile{pkg: t.Package, first: t.ID, imports: map[string]bool{}}
			files[t.File] = tf
			order = append(order, t.File)
		}
		if tf.pkg != t.Package {
			return nil, fmt.Errorf("type %s: its package differs from the other types of its file", t.ID)
		}
		tf.decls = append(tf.decls, t.Decl)
		for _, i := range imps {
			tf.imports[i] = true
		}
	}
	out := map[string][]byte{}
	for _, file := range order {
		tf := files[file]
		var b strings.Builder
		b.WriteString("package " + tf.pkg + "\n\n")
		imps := make([]string, 0, len(tf.imports))
		for i := range tf.imports {
			imps = append(imps, i)
		}
		sort.Strings(imps)
		if len(imps) > 0 {
			b.WriteString("import (\n")
			for _, i := range imps {
				fmt.Fprintf(&b, "\t%q\n", i)
			}
			b.WriteString(")\n\n")
		}
		b.WriteString(strings.Join(tf.decls, "\n\n"))
		b.WriteString("\n")
		src, err := format.Source([]byte(b.String()))
		if err != nil {
			line, col := parseErrAt(err, 0)
			return nil, fmt.Errorf("type %s: declaration does not parse (%d:%d)", tf.first, line, col)
		}
		out[file] = src
	}
	paths := make([]string, 0, len(order))
	for _, file := range order {
		if err := pathsafe.Replace(repo, file, out[file]); err != nil {
			return nil, fmt.Errorf("type %s: %w", files[file].first, err)
		}
		paths = append(paths, file)
	}
	sort.Strings(paths)
	return paths, nil
}
