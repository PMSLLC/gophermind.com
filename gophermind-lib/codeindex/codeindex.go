// Package codeindex builds a static map of a Go tree's symbols — every
// top-level func, method, type, const and var with its location, signature and
// doc summary — and renders it to INDEX.md.
//
// It is deliberately AST-only: no LLM call, so it is fast enough to regenerate
// after every task in an autonomous run, and it cannot drift from the source
// the way written summaries do. The render carries no timestamp, so rebuilding
// an unchanged tree produces a byte-identical file and shows up in a diff only
// when the code really changed.
package codeindex

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the artifact written at the repo root.
const FileName = "INDEX.md"

// Symbol kinds recorded in the index.
const (
	KindFunc      = "func"
	KindMethod    = "method"
	KindType      = "type"
	KindInterface = "interface"
	KindConst     = "const"
	KindVar       = "var"
)

// skipDirs are trees that are never source of interest: dependencies, build
// output, fixtures, and the workflow's own scratch space.
var skipDirs = map[string]bool{
	"vendor": true, "dist": true, "testdata": true,
	".git": true, "node_modules": true, ".planning": true, "tmp": true,
}

// Entry is one indexed symbol.
type Entry struct {
	Package   string
	File      string // repo-relative, slash-separated by filepath rules
	Line      int
	Kind      string
	Name      string
	Recv      string // receiver type for methods, empty otherwise
	Signature string
	Doc       string // first sentence of the doc comment
}

// Index is the built symbol map for a tree.
type Index struct {
	Root    string
	Entries []Entry
}

// Build walks root and indexes every non-test Go file outside skipDirs.
//
// A file that fails to parse is skipped rather than failing the whole build: a
// half-written file mid-task must not break the index the agent is relying on.
func Build(root string) (*Index, error) {
	idx := &Index{Root: root}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil // unparseable right now; skip it
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		idx.Entries = append(idx.Entries, entriesFor(fset, file, rel)...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(idx.Entries, func(i, j int) bool {
		a, b := idx.Entries[i], idx.Entries[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return idx, nil
}

// entriesFor extracts every top-level declaration in one file.
func entriesFor(fset *token.FileSet, file *ast.File, rel string) []Entry {
	pkg := file.Name.Name
	var out []Entry

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			e := Entry{
				Package: pkg, File: rel, Line: fset.Position(d.Pos()).Line,
				Kind: KindFunc, Name: d.Name.Name,
				Signature: render(fset, d.Type), Doc: firstSentence(d.Doc.Text()),
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				e.Kind = KindMethod
				e.Recv = render(fset, d.Recv.List[0].Type)
			}
			out = append(out, e)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := KindType
					if _, ok := s.Type.(*ast.InterfaceType); ok {
						kind = KindInterface
					}
					doc := firstSentence(s.Doc.Text())
					if doc == "" {
						doc = firstSentence(d.Doc.Text())
					}
					out = append(out, Entry{
						Package: pkg, File: rel, Line: fset.Position(s.Pos()).Line,
						Kind: kind, Name: s.Name.Name,
						Signature: typeSignature(fset, s), Doc: doc,
					})

				case *ast.ValueSpec:
					kind := KindVar
					if d.Tok == token.CONST {
						kind = KindConst
					}
					doc := firstSentence(s.Doc.Text())
					if doc == "" {
						doc = firstSentence(d.Doc.Text())
					}
					for _, name := range s.Names {
						sig := kind
						if s.Type != nil {
							sig = kind + " " + render(fset, s.Type)
						}
						out = append(out, Entry{
							Package: pkg, File: rel, Line: fset.Position(name.Pos()).Line,
							Kind: kind, Name: name.Name, Signature: sig, Doc: doc,
						})
					}
				}
			}
		}
	}
	return out
}

// typeSignature summarizes a type without inlining its whole body, which would
// make the index unreadable for large structs.
func typeSignature(fset *token.FileSet, s *ast.TypeSpec) string {
	switch t := s.Type.(type) {
	case *ast.StructType:
		return fmt.Sprintf("type %s struct (%d fields)", s.Name.Name, len(t.Fields.List))
	case *ast.InterfaceType:
		return fmt.Sprintf("type %s interface (%d methods)", s.Name.Name, len(t.Methods.List))
	default:
		return "type " + s.Name.Name + " " + render(fset, s.Type)
	}
}

// render prints an AST node back to source form on one line.
func render(fset *token.FileSet, node any) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// firstSentence trims a doc comment to its first sentence so each symbol
// occupies exactly one row.
func firstSentence(doc string) string {
	doc = strings.TrimSpace(strings.ReplaceAll(doc, "\n", " "))
	if doc == "" {
		return ""
	}
	if i := strings.Index(doc, ". "); i >= 0 {
		return strings.TrimSpace(doc[:i+1])
	}
	return strings.TrimSuffix(doc, ".") + "."
}

// Render writes the index as Markdown grouped by package. Ordering is stable
// and no timestamp is emitted, so an unchanged tree renders identically.
func (idx *Index) Render() string {
	var b strings.Builder
	b.WriteString("# Code index\n\n")
	b.WriteString("Generated by `gophermind index`. Do not edit by hand — it is rebuilt\n")
	b.WriteString("after every completed task. Non-test Go declarations only.\n\n")
	fmt.Fprintf(&b, "%d symbols.\n", len(idx.Entries))

	byPkg := map[string][]Entry{}
	for _, e := range idx.Entries {
		byPkg[e.Package] = append(byPkg[e.Package], e)
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, p := range pkgs {
		fmt.Fprintf(&b, "\n## package %s\n\n", p)
		b.WriteString("| Symbol | Kind | Location | Summary |\n|---|---|---|---|\n")
		for _, e := range byPkg[p] {
			name := e.Name
			if e.Recv != "" {
				name = "(" + e.Recv + ")." + e.Name
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s:%d | %s |\n",
				name, e.Kind, e.File, e.Line, escapePipes(e.Doc))
		}
	}
	return b.String()
}

// escapePipes keeps a doc sentence from breaking the Markdown table.
func escapePipes(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

// Write renders the index to root/INDEX.md via a temp file and rename, so an
// interrupted write cannot leave a truncated index behind.
func (idx *Index) Write(root string) error {
	path := filepath.Join(root, FileName)
	tmp, err := os.CreateTemp(root, ".index-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(idx.Render()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Lookup returns entries whose name (or receiver-qualified name) contains query,
// case-insensitively. An empty query matches everything; kind, when non-empty,
// restricts the result. limit caps the result so a broad query cannot flood the
// model's context.
func (idx *Index) Lookup(query, kind string, limit int) []Entry {
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(query)
	var out []Entry
	for _, e := range idx.Entries {
		if kind != "" && e.Kind != kind {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) &&
			!strings.Contains(strings.ToLower(e.Recv), q) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out
}

// BuildAndWrite is the one-call form used by the CLI and the executor hook.
func BuildAndWrite(root string) (int, error) {
	idx, err := Build(root)
	if err != nil {
		return 0, err
	}
	if err := idx.Write(root); err != nil {
		return 0, err
	}
	return len(idx.Entries), nil
}
