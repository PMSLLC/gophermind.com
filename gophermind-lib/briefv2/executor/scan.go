package executor

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/briefv2/pathsafe"
)

// Finding is one forbidden construct. It carries a fixed kind, a repo
// relative file and a line, never source text.
//
// Kinds: replace, toolchain, godebug, exclude (go.mod); generate, linkname,
// embed, cgo (directives and import "C"); exit (a call or reference to an
// exit, fatal, panic-log or Goexit function, or a recover call, where the
// process could be ended or a panic swallowed); recover; testing (import of
// testing in a non-test file); flagexit (flag.ExitOnError or ContinueOnError
// outside package main); goflags (GOFLAGS); symlink, toolarge, parse (a file
// the scan cannot read as plain Go).
//
// Reason for the exit rules: a model-written init() could print the lines
// go test turns into a pass and exit 0, and the runner cannot tell that from
// a real pass. The acceptance run against the built binary is the backstop;
// this scan is the mitigation.
type Finding struct {
	Kind string
	File string
	Line int
}

// ParseError is a source file that does not parse. Its message names the file
// and the line only; the parser's own text can quote the source.
type ParseError struct {
	File string
	Line int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("scan: %s does not parse (line %d)", e.File, e.Line)
}

const maxScanBytes = 4 << 20

var modForbidden = map[string]bool{"replace": true, "toolchain": true, "godebug": true, "exclude": true}

var modBlockDirective = map[string]bool{"require": true, "replace": true, "exclude": true, "retract": true, "godebug": true, "tool": true, "ignore": true}

// ScanGoMod rejects replace, toolchain, godebug and exclude directives in
// every form: one line, block, and a block opened with the parenthesis glued
// to the keyword. A block entry is a finding of its own, so a block cannot
// hide anything.
func ScanGoMod(src []byte) []Finding {
	var out []Finding
	inBlock := ""
	for i, raw := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		line := raw
		if j := strings.Index(line, "//"); j >= 0 {
			line = line[:j]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if inBlock != "" {
			if f[0] == ")" {
				inBlock = ""
				continue
			}
			if modForbidden[inBlock] {
				out = append(out, Finding{Kind: inBlock, File: "go.mod", Line: i + 1})
			}
			continue
		}
		head, opens := f[0], len(f) >= 2 && f[1] == "("
		if j := strings.Index(head, "("); j > 0 {
			head, opens = head[:j], true
		}
		if modForbidden[head] {
			out = append(out, Finding{Kind: head, File: "go.mod", Line: i + 1})
		}
		if opens && modBlockDirective[head] {
			inBlock = head
		}
	}
	return out
}

// ScanGoFlags rejects -toolexec and -overlay tokens, with one or two leading
// dashes and an optional =value.
func ScanGoFlags(goflags string) []Finding {
	var out []Finding
	for _, tok := range strings.Fields(goflags) {
		name, _, _ := strings.Cut(strings.TrimLeft(tok, "-"), "=")
		if name == "toolexec" || name == "overlay" {
			out = append(out, Finding{Kind: "goflags", File: "GOFLAGS"})
		}
	}
	return out
}

// exitFuncs are the package level functions that end the process, end the
// goroutine, or turn a log line into an exit.
var exitFuncs = map[string]map[string]bool{
	"os":      {"Exit": true},
	"syscall": {"Exit": true},
	"log":     {"Fatal": true, "Fatalf": true, "Fatalln": true, "Panic": true, "Panicf": true, "Panicln": true},
	"runtime": {"Goexit": true},
}

var loggerMethods = map[string]bool{"Fatal": true, "Fatalf": true, "Fatalln": true, "Panic": true, "Panicf": true, "Panicln": true}

var flagExit = map[string]bool{"ExitOnError": true, "ContinueOnError": true}

// importName is the name an import binds: its alias, else the last element of
// the path (skipping a major version suffix).
func importName(spec *ast.ImportSpec, p string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	base := path.Base(p)
	if len(base) > 1 && base[0] == 'v' {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			base = path.Base(path.Dir(p))
		}
	}
	return base
}

// ScanGoSource scans one Go file. rel is its repo relative path (a name ending
// in _test.go is a test file). A parse error is described by file and line.
func ScanGoSource(rel string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		line := 0
		var el scanner.ErrorList
		if errors.As(err, &el) && len(el) > 0 {
			line = el[0].Pos.Line
		}
		return nil, &ParseError{File: rel, Line: line}
	}
	var out []Finding
	add := func(kind string, pos token.Pos) {
		out = append(out, Finding{Kind: kind, File: rel, Line: fset.Position(pos).Line})
	}
	isTest := strings.HasSuffix(rel, "_test.go")
	isMain := f.Name.Name == "main"

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			for _, d := range []struct{ prefix, kind string }{
				{"//go:generate", "generate"}, {"//go:linkname", "linkname"}, {"//go:embed", "embed"},
			} {
				if strings.HasPrefix(c.Text, d.prefix) {
					add(d.kind, c.Pos())
				}
			}
		}
	}

	pkgOf := map[string]string{} // local name -> import path
	dotted := map[string]map[string]bool{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case p == "C":
			add("cgo", spec.Pos())
		case p == "testing" && !isTest:
			add("testing", spec.Pos())
		}
		name := importName(spec, p)
		switch name {
		case "_":
		case ".":
			set := map[string]bool{}
			for n := range exitFuncs[p] {
				set[n] = true
			}
			if p == "flag" && !isMain {
				for n := range flagExit {
					set[n] = true
				}
			}
			if len(set) > 0 {
				dotted[p] = set
			}
		default:
			pkgOf[name] = p
		}
	}

	for _, decl := range f.Decls {
		if isTest {
			break // harness-written tests are not scanned for exits (they may use TestMain and t.Fatal)
		}
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "main" && isMain {
			continue // the one place a process may end itself
		}
		sels := map[*ast.Ident]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				sels[x.Sel] = true
				flagged := false
				if id, ok := x.X.(*ast.Ident); ok && id.Obj == nil {
					if p, ok := pkgOf[id.Name]; ok {
						if exitFuncs[p][x.Sel.Name] {
							add("exit", x.Pos())
							flagged = true
						}
						if p == "flag" && !isMain && flagExit[x.Sel.Name] {
							add("flagexit", x.Pos())
						}
					}
				}
				if !flagged && loggerMethods[x.Sel.Name] {
					// Any receiver or package: a *log.Logger can reach a function through a variable, a
					// parameter or an interface, and a logging package may exit on Fatal. A method named
					// Fatal on a user type is flagged too (a documented false positive).
					add("exit", x.Pos())
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "recover" && id.Obj == nil {
					add("recover", id.Pos())
				}
			case *ast.Ident:
				if x.Obj != nil || sels[x] {
					return true
				}
				for p, set := range dotted {
					if !set[x.Name] {
						continue
					}
					if p == "flag" {
						add("flagexit", x.Pos())
					} else {
						add("exit", x.Pos())
					}
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

func cleanRel(rel string) bool {
	return rel != "" && !filepath.IsAbs(rel) && path.Clean(filepath.ToSlash(rel)) == filepath.ToSlash(rel) && !strings.HasPrefix(rel, "../") && rel != ".."
}

// ScanFiles scans the named repo relative files: go.mod files and .go files.
// Other names are ignored, a missing go.mod is not an error, and a symbolic
// link is a finding and is never followed. A file that does not parse is a
// finding of kind parse.
func ScanFiles(repo string, rels []string) ([]Finding, error) {
	var out []Finding
	for _, rel := range rels {
		rel = filepath.ToSlash(rel)
		if !cleanRel(rel) {
			return nil, errors.New("scan: a path is not a clean repository path")
		}
		base := path.Base(rel)
		isMod, isGo := base == "go.mod", strings.HasSuffix(base, ".go")
		if !isMod && !isGo {
			continue
		}
		abs := filepath.Join(repo, filepath.FromSlash(rel))
		fi, err := os.Lstat(abs)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.New("scan: a file is not readable")
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			out = append(out, Finding{Kind: "symlink", File: rel})
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		if fi.Size() > maxScanBytes {
			out = append(out, Finding{Kind: "toolarge", File: rel})
			continue
		}
		raw, err := readNoFollow(abs)
		if err != nil {
			return nil, errors.New("scan: a file is not readable")
		}
		if isMod {
			for _, f := range ScanGoMod(raw) {
				f.File = rel
				out = append(out, f)
			}
			continue
		}
		fs, err := ScanGoSource(rel, raw)
		var pe *ParseError
		if errors.As(err, &pe) {
			out = append(out, Finding{Kind: "parse", File: rel, Line: pe.Line})
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, fs...)
	}
	return out, nil
}

func readNoFollow(abs string) ([]byte, error) {
	f, err := os.OpenFile(abs, os.O_RDONLY|pathsafe.NoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxScanBytes+1))
}

// skipDir: the tool ignores these too (testdata, names that start with . or _).
func skipDir(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// ScanRepo is go.mod plus every .go file of the repository, skipping .git,
// .gophermind and the directories the go tool ignores.
func ScanRepo(repo string) ([]Finding, error) {
	rels := []string{}
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("scan: the repository is not readable")
		}
		if d.IsDir() {
			if p != repo && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" || strings.HasSuffix(d.Name(), ".go") {
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return errors.New("scan: the repository is not readable")
			}
			rels = append(rels, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rels, func(i, j int) bool {
		if (rels[i] == "go.mod") != (rels[j] == "go.mod") {
			return rels[i] == "go.mod"
		}
		return rels[i] < rels[j]
	})
	return ScanFiles(repo, rels)
}

// describeFindings is the fixed-text message of a scan failure. It names the
// kind, the node that owns the file (or go.mod, GOFLAGS, or "other file") and
// the line: never source text and never a file name the model could choose.
func describeFindings(p *Plan, fs []Finding) string {
	owner := map[string]string{}
	for _, l := range p.Leaves {
		for _, f := range []string{l.File, l.TestFile, l.StubFile} {
			owner[f] = l.ID
		}
	}
	const max = 5
	var parts []string
	for i, f := range fs {
		if i == max {
			parts = append(parts, fmt.Sprintf("and %d more", len(fs)-max))
			break
		}
		who := owner[f.File]
		switch {
		case f.File == "go.mod" || f.File == "GOFLAGS":
			who = f.File
		case who == "":
			who = "other file"
		}
		parts = append(parts, fmt.Sprintf("%s in %s:%d", f.Kind, who, f.Line))
	}
	return "forbidden construct(s): " + strings.Join(parts, ", ")
}
