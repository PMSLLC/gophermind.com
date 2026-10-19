package planner

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/contract"
)

// The compile check. The executor's Wave 0 puts the contract's types and
// function stubs in the repository, adds the test files and runs the compiler.
// A test file that does not compile against those stubs fails Wave 0 with no
// way back, so the planner runs the same compile at plan time, one node at a
// time, and the model repairs the file inside the test-writer's repair loop.

const compileCheckTimeout = 60 * time.Second

// lookGo and runGo are the seams of the check: tests replace them.
var (
	lookGo = exec.LookPath
	runGo  = func(ctx context.Context, goBin, dir string, env []string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, goBin, args...)
		cmd.Dir = dir
		cmd.Env = env
		return cmd.CombinedOutput()
	}
)

// errNoGo is returned by compileCheckTestFile when the go tool is not found:
// the caller records a warning once and goes on.
var errNoGo = fmt.Errorf("the go tool was not found; the compile check of test files was skipped")

// compileCheckTestFile compiles src, the test file rel (a repo-relative slash
// path), against a scratch module holding the contract's types, a stub of every
// contract function and the test files already accepted for other nodes in the
// same directory (others). It returns nil when the file compiles, when the
// check cannot be made (an import that does not resolve offline, a timeout, a
// failure that is not in the test file) and errNoGo when there is no go tool.
// A refusal names the kind of defect and a line, never the source or the
// compiler's text.
func compileCheckTestFile(c *contract.Contracts, rel, src string, others []string) error {
	goBin, err := lookGo("go")
	if err != nil {
		return errNoGo
	}
	dir := path.Dir(rel)
	root, err := os.MkdirTemp("", "gm-compilecheck-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(root)
	if !materializeContract(root, c) {
		return nil
	}
	pkgDir := filepath.Join(root, filepath.FromSlash(dir))
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return nil
	}
	for i, o := range others {
		if err := os.WriteFile(filepath.Join(pkgDir, "zz_other"+strconv.Itoa(i)+"_test.go"), []byte(o), 0o644); err != nil {
			return nil
		}
	}
	base := path.Base(rel)
	if err := os.WriteFile(filepath.Join(pkgDir, base), []byte(src), 0o644); err != nil {
		return nil
	}
	target := "./" + dir
	if dir == "." {
		target = "."
	}
	ctx, cancel := context.WithTimeout(context.Background(), compileCheckTimeout)
	defer cancel()
	out, err := runGo(ctx, goBin, root, goCheckEnv(root), "test", "-count=1", "-run", "^$", target)
	if err == nil || ctx.Err() != nil {
		return nil
	}
	return refuseCompile(c, dir, base, src, string(out))
}

// goCheckEnv is the cleaned environment of the check: no inherited Go
// settings, no network, the local toolchain.
func goCheckEnv(root string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -buildvcs=false", "GOPROXY=off", "GOWORK=off",
		"GOSUMDB=off", "GOTELEMETRY=off", "CGO_ENABLED=0",
	}
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// materializeContract writes go.mod, the contract's type files and a stub file
// for every function under root. It reports false when a declaration cannot be
// turned into a file (an unresolvable qualifier): the check is then skipped.
func materializeContract(root string, c *contract.Contracts) bool {
	if c.Module == "" {
		return false
	}
	siblings := PackageImports(c)
	write := func(rel, body string) bool {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if os.MkdirAll(filepath.Dir(abs), 0o755) != nil {
			return false
		}
		return os.WriteFile(abs, []byte(body), 0o644) == nil
	}
	if !write("go.mod", "module "+c.Module+"\n\ngo 1.21\n") {
		return false
	}
	type group struct {
		clause string
		dir    string
		decls  []string
	}
	files := map[string]*group{}
	var order []string
	for _, t := range c.Types {
		dir := FileDirIn(c.Module, t.Package, t.File)
		g := files[t.File]
		if g == nil {
			g = &group{clause: PackageClause(t.Package), dir: dir}
			files[t.File] = g
			order = append(order, t.File)
		}
		g.decls = append(g.decls, t.Decl)
	}
	for _, file := range order {
		g := files[file]
		body, ok := assembleFile(c.Module, g.dir, g.clause, strings.Join(g.decls, "\n\n"), siblings)
		if !ok || !write(file, body) {
			return false
		}
	}
	for i, f := range c.Functions {
		dir := FileDirIn(c.Module, f.Package, f.File)
		decl := f.Signature + " {\n\tpanic(\"gm: not implemented\")\n}"
		body, ok := assembleFile(c.Module, dir, PackageClause(f.Package), decl, siblings)
		if !ok || !write(path.Join(dir, "zz_gm_stub_"+strconv.Itoa(i)+".go"), body) {
			return false
		}
	}
	return true
}

// assembleFile is a package clause, the imports the declarations need and the
// declarations. ok is false when a qualifier cannot be resolved or the text
// does not parse. An import of the package itself is left out.
func assembleFile(module, dir, clause, decls string, siblings map[string]string) (string, bool) {
	f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+decls, 0)
	if err != nil {
		return "", false
	}
	self := module
	if dir != "." {
		self = module + "/" + dir
	}
	seen := map[string]bool{}
	var imports []string
	for _, q := range Qualifiers(f) {
		p, ok := ResolveQualifier(q, decls, siblings, nil)
		if !ok {
			return "", false
		}
		if p != self && !seen[p] {
			seen[p] = true
			imports = append(imports, p)
		}
	}
	sort.Strings(imports)
	var b strings.Builder
	b.WriteString("package " + clause + "\n\n")
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, i := range imports {
			fmt.Fprintf(&b, "\t%q\n", i)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(decls + "\n")
	return b.String(), true
}

var diagRE = regexp.MustCompile(`(?m)^(?:vet: )?(?:\S*/)?([^/\s:]+):(\d+):(\d+): (.*)$`)

// refuseCompile turns the compiler's output into a refusal of the test file
// base. Only diagnostics located in that file count. Each is reduced to a
// fixed phrase and a line number.
func refuseCompile(c *contract.Contracts, dir, base, src, out string) error {
	if strings.Contains(out, "no required module provides") || strings.Contains(out, "cannot find module") ||
		strings.Contains(out, "module lookup disabled") || strings.Contains(out, "no matching versions") {
		return nil
	}
	lines := strings.Split(src, "\n")
	seen := map[int]bool{}
	var msgs []string
	for _, m := range diagRE.FindAllStringSubmatch(out, -1) {
		if m[1] != base {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if seen[n] {
			continue
		}
		seen[n] = true
		text := ""
		if n >= 1 && n <= len(lines) {
			text = lines[n-1]
		}
		msgs = append(msgs, compileMessage(c, dir, src, text, n, m[4]))
		if len(msgs) == 3 {
			break
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("test file does not compile against the contract: %s", strings.Join(msgs, "; "))
}

// compileMessage is the fixed phrase for one diagnostic. diag is read for its
// class only and never copied.
func compileMessage(c *contract.Contracts, dir, src, line string, n int, diag string) string {
	switch {
	case strings.Contains(diag, "imported and not used") || strings.Contains(diag, "imported as") && strings.Contains(diag, "not used"):
		return fmt.Sprintf("unused import at line %d", n)
	case strings.Contains(diag, "undefined") || strings.Contains(diag, "not exported") || strings.Contains(diag, "unexported"):
		clause := testClause(src)
		if name := unexportedCalled(c, dir, line); name != "" && strings.HasSuffix(clause, "_test") {
			return fmt.Sprintf("undefined name at line %d: %s is unexported and an external test package cannot call it; the test file must be in package %s (not %s) for an unexported function", n, name, strings.TrimSuffix(clause, "_test"), clause)
		}
		return fmt.Sprintf("undefined name at line %d: the function under test is called as the package provides it (an external test package cannot call an unexported name; use package %s for unexported functions)", n, strings.TrimSuffix(clause, "_test"))
	case strings.Contains(diag, "cannot use") || strings.Contains(diag, "mismatched types") ||
		strings.Contains(diag, "invalid composite literal") || strings.Contains(diag, "cannot convert") ||
		strings.Contains(diag, "does not satisfy") || strings.Contains(diag, "incompatible"):
		return fmt.Sprintf("type mismatch at line %d", n)
	}
	return fmt.Sprintf("does not compile at line %d", n)
}

func testClause(src string) string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly)
	if err != nil || f.Name == nil {
		return ""
	}
	return f.Name.Name
}

// unexportedCalled is the name of an unexported contract function of dir that
// the source line selects (pkg.name), or "". Only declared contract names are
// ever returned.
func unexportedCalled(c *contract.Contracts, dir, line string) string {
	for _, fn := range c.Functions {
		if FileDirIn(c.Module, fn.Package, fn.File) != dir {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+fn.Signature, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && !ast.IsExported(fd.Name.Name) {
				if strings.Contains(line, "."+fd.Name.Name) {
					return fd.Name.Name
				}
			}
		}
	}
	return ""
}
