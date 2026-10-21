package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/build/contract"
)

// Richer compile diagnostics. A refusal may carry the types and function names
// a compiler message involves, so the model can act on it, but only tokens
// that are public knowledge: a predeclared Go type, a standard-library
// qualified name, a name the contract declares, or a composite of those. The
// text of a type is parsed and re-rendered by an allowlist printer; a
// variable name, a field name or a type the model declared makes the whole
// token unsafe and the caller falls back to the plain phrase.

// safeNames decides which identifiers may be printed for a test file in dir.
type safeNames struct {
	sameDir  map[string]bool // contract names declared in the test file's directory
	qualType map[string]bool // "pkg.Name" for every contract declaration
}

func newSafeNames(c *contract.Contracts, dir string) *safeNames {
	s := &safeNames{sameDir: map[string]bool{}, qualType: map[string]bool{}}
	if c == nil {
		return s
	}
	add := func(pkg, fileDir string, names []string) {
		clause := PackageClause(pkg)
		for _, n := range names {
			s.qualType[clause+"."+n] = true
			if fileDir == dir {
				s.sameDir[n] = true
			}
		}
	}
	for _, t := range c.Types {
		add(t.Package, FileDirIn(c.Module, t.Package, t.File), declNames(t.Decl))
	}
	for _, fn := range c.Functions {
		f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+fn.Signature, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				add(fn.Package, FileDirIn(c.Module, fn.Package, fn.File), []string{fd.Name.Name})
			}
		}
	}
	return s
}

func (s *safeNames) ident(name string) bool {
	if obj := types.Universe.Lookup(name); obj != nil {
		switch obj.(type) {
		case *types.TypeName, *types.Builtin:
			return true
		}
	}
	return s.sameDir[name]
}

func (s *safeNames) selector(pkg, name string) bool {
	if _, ok := StdQualifiers[pkg]; ok {
		return true
	}
	return s.qualType[pkg+"."+name]
}

// typeText parses text as a Go type and re-renders it. ok is false when any
// part of it is not on the allowlist.
func (s *safeNames) typeText(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	e, err := parser.ParseExpr(text)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	if !s.render(&b, e) {
		return "", false
	}
	return b.String(), true
}

// funcName is a single (possibly qualified) function name.
func (s *safeNames) funcName(text string) (string, bool) {
	e, err := parser.ParseExpr(strings.TrimSpace(text))
	if err != nil {
		return "", false
	}
	switch v := e.(type) {
	case *ast.Ident:
		if s.ident(v.Name) {
			return v.Name, true
		}
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok && s.selector(x.Name, v.Sel.Name) {
			return x.Name + "." + v.Sel.Name, true
		}
	}
	return "", false
}

// tupleText parses "(T1, T2)" as a parameter list of unnamed types.
func (s *safeNames) tupleText(text string) (string, bool) {
	e, err := parser.ParseExpr("func" + strings.TrimSpace(text))
	if err != nil {
		return "", false
	}
	ft, ok := e.(*ast.FuncType)
	if !ok || ft.Results != nil || ft.Params == nil {
		return "", false
	}
	var parts []string
	for _, f := range ft.Params.List {
		if len(f.Names) != 0 {
			return "", false
		}
		var b strings.Builder
		if !s.render(&b, f.Type) {
			return "", false
		}
		parts = append(parts, b.String())
	}
	return "(" + strings.Join(parts, ", ") + ")", true
}

func (s *safeNames) render(b *strings.Builder, e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		if v.Name == "number" || s.ident(v.Name) {
			b.WriteString(v.Name)
			return true
		}
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok && s.selector(x.Name, v.Sel.Name) {
			b.WriteString(x.Name + "." + v.Sel.Name)
			return true
		}
	case *ast.StarExpr:
		b.WriteString("*")
		return s.render(b, v.X)
	case *ast.ParenExpr:
		return s.render(b, v.X)
	case *ast.Ellipsis:
		b.WriteString("...")
		return v.Elt != nil && s.render(b, v.Elt)
	case *ast.ArrayType:
		switch l := v.Len.(type) {
		case nil:
			b.WriteString("[]")
		case *ast.BasicLit:
			if l.Kind != token.INT {
				return false
			}
			b.WriteString("[" + l.Value + "]")
		default:
			return false
		}
		return s.render(b, v.Elt)
	case *ast.MapType:
		b.WriteString("map[")
		if !s.render(b, v.Key) {
			return false
		}
		b.WriteString("]")
		return s.render(b, v.Value)
	case *ast.ChanType:
		switch v.Dir {
		case ast.SEND:
			b.WriteString("chan<- ")
		case ast.RECV:
			b.WriteString("<-chan ")
		default:
			b.WriteString("chan ")
		}
		return s.render(b, v.Value)
	case *ast.InterfaceType:
		if v.Methods == nil || len(v.Methods.List) == 0 {
			b.WriteString("interface{}")
			return true
		}
	case *ast.StructType:
		if v.Fields == nil || len(v.Fields.List) == 0 {
			b.WriteString("struct{}")
			return true
		}
	case *ast.FuncType:
		b.WriteString("func(")
		if !s.fields(b, v.Params) {
			return false
		}
		b.WriteString(")")
		if v.Results != nil && len(v.Results.List) > 0 {
			b.WriteString(" ")
			paren := len(v.Results.List) > 1
			if paren {
				b.WriteString("(")
			}
			if !s.fields(b, v.Results) {
				return false
			}
			if paren {
				b.WriteString(")")
			}
		}
		return true
	}
	return false
}

// fields renders unnamed parameter or result types; a named one is unsafe.
func (s *safeNames) fields(b *strings.Builder, fl *ast.FieldList) bool {
	if fl == nil {
		return true
	}
	for i, f := range fl.List {
		if len(f.Names) != 0 {
			return false
		}
		if i > 0 {
			b.WriteString(", ")
		}
		if !s.render(b, f.Type) {
			return false
		}
	}
	return true
}

var anyTypeRE = regexp.MustCompile(`interface\{\}|\bany\b`)

var (
	cannotUseRE = regexp.MustCompile(`^cannot use .* \((?:[^()]*? )?of type (.+?)\) as (.+?) value in (argument to ([^\s:]+)|assignment|return statement|struct literal|field value|variable declaration)`)
	argCountRE  = regexp.MustCompile(`(?s)^(not enough|too many) arguments in call to ([^\s:]+)(?:\s*\n\s*have (\(.*?\))\s*\n\s*want (\(.*?\)))?`)
	noFieldRE   = regexp.MustCompile(`^\S+ undefined \(type (.+?) has no field or method `)
	// "unknown field values in struct literal of type columnState"; older
	// compilers quote the field and leave the type out. go1.27 appends a
	// hint when a declared field differs only by case or is unexported:
	// "..., but does have Count", "..., but does have unexported count". The
	// hint is matched so the type capture stops before it, and discarded: the
	// name it offers is already in the declared-fields list of the message.
	unknownFieldLitRE = regexp.MustCompile(`^unknown field '?([A-Za-z_][A-Za-z0-9_]*)'? in struct literal(?: of type (.+?))?(?:, but does have (?:unexported )?[A-Za-z_][A-Za-z0-9_]*)?$`)
)

// mismatchWords is the phrase for a value of type t offered where u is
// required at the place the compiler names.
func mismatchWords(place, t, u string, call string) string {
	switch {
	case strings.HasPrefix(place, "argument to"):
		s := fmt.Sprintf("a value of type %s is passed where %s is required", t, u)
		if call != "" {
			s += " in the call to " + call
		}
		return s
	case place == "assignment":
		return fmt.Sprintf("a value of type %s is assigned where %s is required", t, u)
	case place == "return statement":
		return fmt.Sprintf("a value of type %s is returned where %s is required", t, u)
	case place == "struct literal" || place == "field value":
		return fmt.Sprintf("a value of type %s is given to a struct field that requires %s", t, u)
	}
	return fmt.Sprintf("a value of type %s is used in a variable declaration that requires %s", t, u)
}

// richMismatch is the detailed phrase for a "cannot use" diagnostic, or "".
func richMismatch(s *safeNames, n int, diag string) string {
	m := cannotUseRE.FindStringSubmatch(diag)
	if m == nil {
		return ""
	}
	t, ok := s.typeText(m[1])
	if !ok {
		return ""
	}
	u, ok := s.typeText(m[2])
	if !ok {
		return ""
	}
	call := ""
	if m[4] != "" {
		call, _ = s.funcName(m[4])
	}
	out := fmt.Sprintf("type mismatch at line %d: %s", n, mismatchWords(m[3], t, u, call))
	if anyTypeRE.MatchString(t) {
		out += " (declare the field with the exact type the function takes)"
	}
	return out
}

// richArgCount is the detailed phrase for a wrong argument count, or "".
func richArgCount(s *safeNames, n int, diag string) string {
	m := argCountRE.FindStringSubmatch(diag)
	if m == nil {
		return ""
	}
	out := fmt.Sprintf("wrong number of arguments at line %d", n)
	if f, ok := s.funcName(m[2]); ok {
		out += " in the call to " + f
	}
	if m[3] != "" && m[4] != "" {
		have, ok1 := s.tupleText(m[3])
		want, ok2 := s.tupleText(m[4])
		if ok1 && ok2 {
			out += fmt.Sprintf(": it takes %s and the call passes %s", want, have)
		}
	}
	return out
}

// richNoField is the phrase for "x.y undefined (type T has no field or
// method y)", with T only when it is safe.
func richNoField(s *safeNames, n int, diag string) string {
	m := noFieldRE.FindStringSubmatch(diag)
	if m == nil {
		return ""
	}
	if t, ok := s.typeText(m[1]); ok {
		return fmt.Sprintf("unknown field or method at line %d on a value of type %s", n, t)
	}
	return fmt.Sprintf("unknown field or method at line %d", n)
}

// richUnknownField is the phrase for a keyed composite literal that names a
// field its struct does not declare: the line, the type when it is safe, and
// the contract's declared fields as the valid values. The field name from the
// diagnostic is the model's and is printed only when it is a wrong-case
// spelling of a declared field. "" when diag is another diagnostic.
func richUnknownField(s *safeNames, c *contract.Contracts, dir string, n int, diag string) string {
	m := unknownFieldLitRE.FindStringSubmatch(diag)
	if m == nil {
		return ""
	}
	out := fmt.Sprintf("unknown field at line %d in a composite literal", n)
	t, ok := s.typeText(m[2])
	if !ok {
		return out
	}
	si, ok := contractStruct(c, dir, t)
	if !ok {
		return out + " of type " + t
	}
	for _, f := range si.fields {
		if strings.EqualFold(f, m[1]) {
			out = fmt.Sprintf("unknown field %s at line %d in a composite literal", m[1], n)
			break
		}
	}
	out += " of type " + t + "; declared fields: " + strings.Join(si.fields, ", ")
	if si.embedded {
		out += "; the type embeds other types"
	}
	return out
}

// contractStruct is the contract struct a safe type text names (T, pkg.T or
// *T). When several packages declare the name, the one in dir is preferred.
func contractStruct(c *contract.Contracts, dir, text string) (structInfo, bool) {
	e, err := parser.ParseExpr(text)
	if err != nil {
		return structInfo{}, false
	}
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	name := ""
	switch v := e.(type) {
	case *ast.Ident:
		name = v.Name
	case *ast.SelectorExpr:
		name = v.Sel.Name
	}
	pkg := ""
	for _, t := range c.Types {
		if contains(declNames(t.Decl), name) && (pkg == "" || FileDirIn(c.Module, t.Package, t.File) == dir) {
			pkg = t.Package
		}
	}
	return lookupStruct(name, pkg, c)
}
