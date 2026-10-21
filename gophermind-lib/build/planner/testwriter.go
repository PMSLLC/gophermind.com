package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/pathsafe"
	"gophermind/gophermind-lib/build/router"
	"gophermind/gophermind-lib/build/tree"
)

// testwriterState is _state/testwriter.json: the tests of every node that is
// finished, and the hash of the test file written for it.
type testwriterState struct {
	Nodes map[string]writtenTests `json:"nodes"`
	// Pending is the node whose file is about to be written: recorded before
	// the write, so a crash between the write and the save of Nodes is
	// recognised on resume by the file's hash instead of refused as foreign.
	Pending map[string]writtenTests `json:"pending,omitempty"`
}

type writtenTests struct {
	Tests    []map[string]any `json:"tests"`
	TestFile string           `json:"test_file"` // repo-relative, forward slashes
	SHA256   string           `json:"sha256"`
}

func testwriterDone(r *run) bool { return r.status.PlannedAt != "" }

// testFuncName is the test function a node's tests live in: fn-validate-email
// gives TestValidateEmail. Node ids are unique, so the names are too.
func testFuncName(nodeID string) string {
	name := "Test"
	for _, part := range strings.Split(strings.TrimPrefix(nodeID, "fn-"), "-") {
		if part != "" {
			name += strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return name
}

// testFilePath is where a node's test file goes: beside the function's file,
// named after the node so two functions in one file never share a test file.
func testFilePath(nodeID, contractFile string) string {
	return path.Join(path.Dir(contractFile), strings.ReplaceAll(nodeID, "-", "_")+"_test.go")
}

// testCommand is the command of every test of a node. The harness writes it;
// a model never does.
func testCommand(contractFile, funcName string) string {
	dir := path.Dir(contractFile)
	if dir != "." {
		dir = "./" + dir
	}
	return "go test " + dir + " -run ^" + funcName + "$"
}

// testwriter is the Test-writer stage: for every function node, tests written
// from its contract alone and a test file placed in the target repository,
// then the finished tree and the blackboard rows. It is the first stage that
// touches the repository, and it refuses to run without a matching approval.
func (p *Planner) testwriter(ctx context.Context, r *run) error {
	if err := VerifyApproval(r.dir); err != nil {
		return err
	}

	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	classes, err := loadClasses(r)
	if err != nil {
		return err
	}
	w, err := planWaves(r.id, c, dec)
	if err != nil {
		return err
	}
	st := testwriterState{}
	if _, err := readJSON(r.path(stateTestwriter), &st); err != nil {
		return err
	}
	if st.Nodes == nil {
		st.Nodes = map[string]writtenTests{}
	}

	var drafts []map[string]any
	for _, comp := range c.Components {
		drafts = append(drafts, dec.Components[comp.ID]...)
	}
	sort.SliceStable(drafts, func(i, j int) bool {
		a, _ := drafts[i]["id"].(string)
		b, _ := drafts[j]["id"].(string)
		if w[a] != w[b] {
			return w[a] < w[b]
		}
		return a < b
	})
	for _, d := range drafts {
		id, _ := d["id"].(string)
		if _, done := st.Nodes[id]; done {
			continue
		}
		wt, err := p.writeTests(ctx, r, c, d, classes[id], &st)
		if err != nil {
			return fmt.Errorf("node %s: %w", id, err)
		}
		st.Nodes[id] = wt
		delete(st.Pending, id)
		if err := writeJSON(r.path(stateTestwriter), st); err != nil {
			return err
		}
	}
	if err := p.writeAcceptanceTests(ctx, r, c, &st); err != nil {
		return err
	}
	return p.finishTree(ctx, r, c, dec, st, w)
}

// renderTestwriter renders the Test-writer prompt. It states the module path
// and the import path of the package under test, so a test in package X_test
// does not invent a module name checkTestSource would refuse. The import path
// is the module plus the directory of the contract file ("." is the module).
func renderTestwriter(node, module, file, funcName, testFile string) (string, error) {
	return renderTestwriterWith(node, module, file, funcName, testFile, nil)
}

// renderTestwriterWith is renderTestwriter that also lists the import path of
// every package qualifier the node's signature and dependency signatures use.
// siblings maps contract package clauses to import paths (PackageImports).
func renderTestwriterWith(node, module, file, funcName, testFile string, siblings map[string]string) (string, error) {
	dir := path.Dir(file)
	importPath := ""
	if module != "" {
		importPath = module
		if dir != "." {
			importPath = module + "/" + dir
		}
	}
	mainPkg := ""
	if PackageClause(dir) == "main" {
		mainPkg = "yes"
	}
	data := map[string]string{"MainPackage": mainPkg, "Node": node, "PackageDir": dir,
		"TestFuncName": funcName, "TestFile": testFile, "Module": module, "ImportPath": importPath,
		"Imports": qualifierImports(node, siblings)}
	data["Unexported"], data["Method"], data["Clause"], data["TargetName"], data["Why"] = "", "", "", "", ""
	if name, method, why := internalTarget(nodeSignature(node)); why != whyNone && mainPkg == "" {
		data["Unexported"], data["Clause"], data["TargetName"], data["Why"] = "yes", PackageClause(dir), name, whyPrompt(why)
		if method {
			data["Method"] = "yes"
		}
	}
	return render("testwriter", data)
}

// qualifierImports lists, one per line, the import path of every package
// qualifier used in the node's signature and in its dependency signatures
// ("stats" = "csvstat/internal/stats"). A qualifier that cannot be resolved is
// left out. The result is "" when there is nothing to list.
func qualifierImports(node string, siblings map[string]string) string {
	var n struct {
		Contract struct {
			Signature string `json:"signature"`
		} `json:"contract"`
		Context struct {
			Deps json.RawMessage `json:"dependency_signatures"`
		} `json:"context"`
	}
	if json.Unmarshal([]byte(node), &n) != nil {
		return ""
	}
	texts := []string{n.Contract.Signature}
	var collect func(v any)
	collect = func(v any) {
		switch x := v.(type) {
		case string:
			texts = append(texts, x)
		case []any:
			for _, e := range x {
				collect(e)
			}
		case map[string]any:
			for _, e := range x {
				collect(e)
			}
		}
	}
	if len(n.Context.Deps) > 0 {
		var v any
		if json.Unmarshal(n.Context.Deps, &v) == nil {
			collect(v)
		}
	}
	found := map[string]string{}
	for _, t := range texts {
		f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+t, 0)
		if err != nil {
			continue
		}
		for _, q := range Qualifiers(f) {
			if p, ok := ResolveQualifier(q, t, siblings, nil); ok {
				found[q] = p
			}
		}
	}
	names := make([]string, 0, len(found))
	for q := range found {
		names = append(names, q)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, q := range names {
		fmt.Fprintf(&b, "- `%s` = `%s`\n", q, found[q])
	}
	return strings.TrimRight(b.String(), "\n")
}

// contractPackagePaths are the import paths of the packages of the contract:
// the module path plus the directory of each declaration's file. all includes
// main packages; importable leaves them out. Both are sorted and unique.
func contractPackagePaths(c *contract.Contracts) (all, importable []string) {
	seenAll, seenImp := map[string]bool{}, map[string]bool{}
	add := func(pkg, file string) {
		dir := FileDirIn(c.Module, pkg, file)
		p := c.Module
		if dir != "." {
			p = c.Module + "/" + dir
		}
		if !seenAll[p] {
			seenAll[p] = true
			all = append(all, p)
		}
		if PackageClause(dir) != "main" && !seenImp[p] {
			seenImp[p] = true
			importable = append(importable, p)
		}
	}
	for _, t := range c.Types {
		add(t.Package, t.File)
	}
	for _, f := range c.Functions {
		add(f.Package, f.File)
	}
	sort.Strings(all)
	sort.Strings(importable)
	return all, importable
}

// nodeSignature is contract.signature of a node's JSON, or "".
func nodeSignature(node string) string {
	var n struct {
		Contract struct {
			Signature string `json:"signature"`
		} `json:"contract"`
	}
	if json.Unmarshal([]byte(node), &n) != nil {
		return ""
	}
	return n.Contract.Signature
}

// Why a test of the function under test has to live in the package itself.
const (
	whyNone  = ""
	whyFunc  = "function" // the function, or its receiver type, is unexported
	whyTypes = "types"    // an exported function whose signature uses unexported types
)

// internalTarget reports whether the function under test, given by its
// signature, cannot be called from an external test package, and why: an
// unexported function, a method whose name or receiver type is unexported, or a
// signature that mentions an unexported type of the package (an unqualified,
// non-predeclared, lower-case name that is not a type parameter). name is the
// declared name; method says it has a receiver.
func internalTarget(sig string) (name string, method bool, why string) {
	f, err := parser.ParseFile(token.NewFileSet(), "d.go", "package p\n"+sig, 0)
	if err != nil {
		return "", false, whyNone
	}
	for _, d := range f.Decls {
		fd, isFn := d.(*ast.FuncDecl)
		if !isFn {
			continue
		}
		name = fd.Name.Name
		tparams := map[string]bool{}
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			method = true
			t := fd.Recv.List[0].Type
			for {
				switch x := t.(type) {
				case *ast.StarExpr:
					t = x.X
					continue
				case *ast.ParenExpr:
					t = x.X
					continue
				case *ast.IndexExpr:
					if id, ok := x.Index.(*ast.Ident); ok {
						tparams[id.Name] = true
					}
					t = x.X
					continue
				case *ast.IndexListExpr:
					for _, i := range x.Indices {
						if id, ok := i.(*ast.Ident); ok {
							tparams[id.Name] = true
						}
					}
					t = x.X
					continue
				}
				break
			}
			if id, isID := t.(*ast.Ident); isID && !ast.IsExported(id.Name) {
				return name, true, whyFunc
			}
		}
		if !ast.IsExported(name) {
			return name, method, whyFunc
		}
		if fd.Type.TypeParams != nil {
			for _, p := range fd.Type.TypeParams.List {
				for _, n := range p.Names {
					tparams[n.Name] = true
				}
			}
		}
		var exprs []ast.Expr
		for _, fl := range []*ast.FieldList{fd.Type.TypeParams, fd.Type.Params, fd.Type.Results} {
			if fl == nil {
				continue
			}
			for _, p := range fl.List {
				exprs = append(exprs, p.Type)
			}
		}
		for _, e := range exprs {
			if mentionsUnexported(e, tparams) {
				return name, method, whyTypes
			}
		}
		return name, method, whyNone
	}
	return "", false, whyNone
}

// mentionsUnexported reports whether the type expression uses an unqualified,
// lower-case name that is neither predeclared nor in skip (type parameters).
func mentionsUnexported(e ast.Expr, skip map[string]bool) bool {
	var fields func(fl *ast.FieldList) bool
	fields = func(fl *ast.FieldList) bool {
		if fl == nil {
			return false
		}
		for _, f := range fl.List {
			if mentionsUnexported(f.Type, skip) {
				return true
			}
		}
		return false
	}
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == "_" || ast.IsExported(x.Name) || skip[x.Name] {
			return false
		}
		if obj := types.Universe.Lookup(x.Name); obj != nil {
			if _, ok := obj.(*types.TypeName); ok {
				return false
			}
		}
		return true
	case *ast.StarExpr:
		return mentionsUnexported(x.X, skip)
	case *ast.ParenExpr:
		return mentionsUnexported(x.X, skip)
	case *ast.Ellipsis:
		return x.Elt != nil && mentionsUnexported(x.Elt, skip)
	case *ast.ArrayType:
		return mentionsUnexported(x.Elt, skip)
	case *ast.MapType:
		return mentionsUnexported(x.Key, skip) || mentionsUnexported(x.Value, skip)
	case *ast.ChanType:
		return mentionsUnexported(x.Value, skip)
	case *ast.FuncType:
		return fields(x.Params) || fields(x.Results)
	case *ast.StructType:
		return fields(x.Fields)
	case *ast.InterfaceType:
		return fields(x.Methods)
	case *ast.IndexExpr:
		return mentionsUnexported(x.X, skip) || mentionsUnexported(x.Index, skip)
	case *ast.IndexListExpr:
		if mentionsUnexported(x.X, skip) {
			return true
		}
		for _, i := range x.Indices {
			if mentionsUnexported(i, skip) {
				return true
			}
		}
	case *ast.BinaryExpr:
		return mentionsUnexported(x.X, skip) || mentionsUnexported(x.Y, skip)
	case *ast.UnaryExpr:
		return mentionsUnexported(x.X, skip)
	}
	return false
}

// writeTests makes the model call for one node and writes its test file.
func (p *Planner) writeTests(ctx context.Context, r *run, c *contract.Contracts, d map[string]any, class string, st *testwriterState) (writtenTests, error) {
	id, _ := d["id"].(string)
	ct, _ := d["contract"].(map[string]any)
	file, _ := ct["file"].(string)
	pkg, _ := ct["package"].(string)
	funcName := testFuncName(id)
	rel := testFilePath(id, file)
	abs, err := pathsafe.ResolveTest(r.repo, rel)
	if err != nil {
		return writtenTests{}, err
	}
	if _, err := os.Lstat(abs); err == nil {
		// Only a file this run recorded, byte for byte, is accepted.
		if pend, ok := st.Pending[id]; ok {
			if raw, err := os.ReadFile(abs); err == nil && hashHex(raw) == pend.SHA256 {
				return pend, nil
			}
		}
		return writtenTests{}, fmt.Errorf("test file %s already exists and this run did not write it; move it away, then resume", path.Base(rel))
	}
	prompt, err := renderTestwriterWith(mustJSON(testwriterView(d)), c.Module, file, funcName, rel, PackageImports(c))
	if err != nil {
		return writtenTests{}, err
	}
	var tests []map[string]any
	var source string
	renamedHelpers := false
	var droppedImports []string
	var droppedComments int
	cs := callSpec{stage: "testwrite:" + id, taskType: "testwrite", nodeID: id, nodeClass: class, tier: p.tierFor(strField(d, "model_tier")), scope: router.ScopeNode, maxTokens: maxTokensTestwrite}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		ts, src, droppedC, err := parseTestwriteCounted(StripReply(text), ct, pkg, funcName, c.Module, hookList(d), packageSet(c))
		if err != nil {
			return err
		}
		others := otherTestSources(r.repo, path.Dir(rel), id, st)
		src, renamed, err := resolveSharedNames(src, strings.TrimPrefix(funcName, "Test"), others, stubSources(c, path.Dir(file)))
		if err == errDeclaredName {
			err = richDeclaredName(src, c, path.Dir(rel), err)
		}
		if err != nil {
			return err
		}
		src, dropped := dropUnusedImports(src)
		if err := compileCheckTestFile(c, rel, src, others); err != nil {
			if errors.Is(err, errNoGo) {
				p.noGoWarn.Do(func() { p.emit(events.KindWarning, cs.stage, id, err.Error()) })
				tests, source, renamedHelpers, droppedImports, droppedComments = ts, src, renamed, dropped, droppedC
				return nil
			}
			return err
		}
		tests, source, renamedHelpers, droppedImports, droppedComments = ts, src, renamed, dropped, droppedC
		return nil
	})
	if err != nil {
		return writtenTests{}, err
	}
	if renamedHelpers {
		p.emit(events.KindWarning, cs.stage, id, "test_helper_renamed: helpers in the test file of node "+id+" collided with another test file of the package and were renamed with the function name")
	}
	if len(droppedImports) > 0 {
		p.emit(events.KindWarning, cs.stage, id, fmt.Sprintf(
			"test_import_normalized: %d unused imports were removed from the test file of node %s",
			len(droppedImports), id))
	}
	if droppedComments > 0 {
		p.emit(events.KindWarning, cs.stage, id, fmt.Sprintf(
			"test_comment_normalized: %d long comment blocks were removed from the test file of node %s",
			droppedComments, id))
	}
	wt := writtenTests{Tests: tests, TestFile: rel, SHA256: hashHex([]byte(source))}
	if st.Pending == nil {
		st.Pending = map[string]writtenTests{}
	}
	st.Pending[id] = wt
	if err := writeJSON(r.path(stateTestwriter), *st); err != nil {
		return writtenTests{}, err
	}
	if err := placeTestFile(r, abs, rel, source); err != nil {
		return writtenTests{}, err
	}
	return wt, nil
}

// placeTestFile writes a test file the model produced, after checking the path
// again: the first check was made before a model call that can take minutes.
// It refuses an existing entry, a link and a folder that leads out of the repo.
func placeTestFile(r *run, abs, rel, source string) error {
	// The path was checked before the model call, which can take minutes.
	// Check it again before each step that touches the disk.
	beforeTestWrite()
	recheck := func() error {
		if _, err := pathsafe.ResolveTest(r.repo, rel); err != nil {
			return err
		}
		if _, err := os.Lstat(abs); err == nil {
			return fmt.Errorf("test file %s already exists and this run did not write it; move it away, then resume", path.Base(rel))
		}
		return nil
	}
	if err := recheck(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("creating the folder for %s: %s", path.Base(rel), osReason(err))
	}
	// O_EXCL fails on any existing entry, a symbolic link included, and never
	// follows one at the final component.
	if err := recheck(); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL|pathsafe.NoFollow, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	// A directory swapped for a link after the last check would have been
	// followed by the open: undo that write.
	if err := pathsafe.InsideRepo(r.repo, filepath.Dir(abs)); err != nil {
		f.Close()
		os.Remove(abs)
		return err
	}
	_, werr := f.WriteString(source)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(werr))
	}
	return nil
}

// scalarText is a decoded JSON string, boolean or number as plain text. A model
// writing a test for a function that returns a bool or a number puts the bare
// value there. A non-empty object or array (the argument values of a function
// with several arguments) is kept as compact JSON text; the executable test is
// test_file, so the list is only read by people. Null, an empty object or
// array are not text.
func scalarText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case json.Number:
		return x.String(), true
	case map[string]any:
		if len(x) == 0 {
			return "", false
		}
	case []any:
		if len(x) == 0 {
			return "", false
		}
	default:
		return "", false
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSuffix(b.String(), "\n"), true
}

// testField reads the given or expect of test n. A key that is absent is
// reported as missing; a present empty or blank string is kept as written (an
// empty input or an empty expected output is a real test).
func testField(t map[string]any, key string, n int) (string, error) {
	v, present := t[key]
	if !present {
		return "", fmt.Errorf("test %d: %s is missing", n, key)
	}
	text, ok := scalarText(v)
	if !ok {
		return "", fmt.Errorf("test %d: %s must be text", n, key)
	}
	return text, nil
}

// parseTestwrite checks a Test-writer reply: enough tests, each described,
// and a Go test file that parses, holds the expected test function, and
// imports nothing outside the standard library and the module. Level and
// command are set here, whatever the model wrote.
func parseTestwrite(text string, ct map[string]any, pkg, funcName, module string, hooks []string) ([]map[string]any, string, error) {
	return parseTestwriteIn(text, ct, pkg, funcName, module, hooks, importSet{})
}

// parseTestwriteIn is parseTestwrite that also knows the packages of the
// contract, so an import of this module can be checked and a refusal can name
// the valid ones.
func parseTestwriteIn(text string, ct map[string]any, pkg, funcName, module string, hooks []string, pkgs importSet) ([]map[string]any, string, error) {
	tests, src, _, err := parseTestwriteCounted(text, ct, pkg, funcName, module, hooks, pkgs)
	return tests, src, err
}

// parseTestwriteCounted is parseTestwriteIn that also reports how many long
// comments it removed from the test file before the validator ran.
func parseTestwriteCounted(text string, ct map[string]any, pkg, funcName, module string, hooks []string, pkgs importSet) ([]map[string]any, string, int, error) {
	var reply struct {
		Tests    []map[string]any `json:"tests"`
		TestFile string           `json:"test_file"`
	}
	decode := func(src string) error {
		dec := json.NewDecoder(strings.NewReader(src))
		dec.UseNumber() // a number keeps the digits the model wrote
		return dec.Decode(&reply)
	}
	if err := decode(text); err != nil {
		// Fallback only: an unclosed string on a pretty-printed line is
		// closed in place. The repaired text is kept only if it decodes,
		// otherwise the original error is the one reported.
		var syn *json.SyntaxError
		fixed, changed := closeStrayStrings(text)
		if errors.As(err, &syn) && strings.Contains(syn.Error(), "in string") && changed {
			reply.Tests, reply.TestFile = nil, ""
			if decode(fixed) == nil {
				err = nil
			}
		}
		if err != nil {
			return nil, "", 0, fmt.Errorf("test-writer reply is not a JSON object (%s)", jsonErrIn(err, text))
		}
	}
	need := len(objects(ct["errors"])) + 1
	if len(reply.Tests) < need {
		return nil, "", 0, fmt.Errorf("test-writer reply has %d tests; the contract lists %d error conditions, so at least %d are needed (the happy path and one per condition)",
			len(reply.Tests), need-1, need)
	}
	file, _ := ct["file"].(string)
	command := testCommand(file, funcName)
	tests := make([]map[string]any, 0, len(reply.Tests))
	for i, t := range reply.Tests {
		name, _ := t["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, "", 0, fmt.Errorf("test %d needs a name", i+1)
		}
		given, err := testField(t, "given", i+1)
		if err != nil {
			return nil, "", 0, err
		}
		expect, err := testField(t, "expect", i+1)
		if err != nil {
			return nil, "", 0, err
		}
		pol, _ := t["polarity"].(string)
		cov, _ := t["covers"].(string)
		tests = append(tests, map[string]any{"name": name, "level": "unit", "given": given, "expect": expect, "command": command, "polarity": pol, "covers": cov})
	}
	if err := checkPolarity(tests, len(objects(ct["errors"]))); err != nil {
		return nil, "", 0, err
	}
	sig, _ := ct["signature"].(string)
	_, _, why := internalTarget(sig)
	var dropped int
	reply.TestFile, dropped = dropLongComments(reply.TestFile)
	if err := checkTestSourceIn(reply.TestFile, pkg, funcName, module, why, pkgs); err != nil {
		return nil, "", 0, err
	}
	if contains(hooks, "bench") && !hasBenchmark(reply.TestFile, funcName) {
		return nil, "", 0, fmt.Errorf("%s: the profile hooks ask for a benchmark, but the test file has no Benchmark%s function", grpHookBench, strings.TrimPrefix(funcName, "Test"))
	}
	return tests, reply.TestFile, dropped, nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stdTopLevel is the set of standard library top-level path elements. A path
// whose first element is not in it is standard only if it is the module's own.
// internal and vendor are deliberately absent.
var stdTopLevel = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields("archive bufio bytes cmp compress container context crypto database debug embed encoding errors expvar flag fmt go hash html image index io iter log maps math mime net os path plugin reflect regexp runtime slices sort strconv strings structs sync syscall testing text time unicode unique unsafe weak") {
		m[n] = true
	}
	return m
}()

// PackageClause is the Go package clause of a contract package. The contract
// gives a directory path ("internal/stats"), so the clause is its last
// element; the module root and a directory under cmd/ hold package main. A
// plain name ("greet") is its own clause.
func PackageClause(pkg string) string {
	if first, _, _ := strings.Cut(pkg, "/"); path.Clean(pkg) == "." || first == "cmd" {
		return "main"
	}
	return path.Base(pkg)
}

// packageClauseNames are the package clauses a test file for contract package
// pkg may carry, plain or with the _test suffix. A main package can only be
// tested from package main: Go cannot import it, and a second clause in its
// directory breaks the build.
func packageClauseNames(pkg string) []string {
	if PackageClause(pkg) == "main" {
		return []string{"main"}
	}
	var names []string
	if base := path.Base(pkg); base != "." && base != "/" {
		names = append(names, base, base+"_test")
	}
	return names
}

// mainRefusal is the refusal for a test file of a main package.
const mainRefusal = "test file is not in package main: a test file for a main package must declare package main and must not import it"

// maxCommentRun is the longest run of consecutive comment lines a test file may
// carry; maxCommentShare is the share of the file's lines comments may fill.
const (
	maxCommentRun   = 8
	maxCommentShare = 0.4
)

// checkCommentVolume refuses a test file that is mostly deliberation in
// comments. The messages never quote the file.
func checkCommentVolume(fset *token.FileSet, f *ast.File, src string) error {
	total := 0
	for _, g := range f.Comments {
		first := fset.Position(g.Pos()).Line
		n := fset.Position(g.End()).Line - first + 1
		if n > maxCommentRun {
			return fmt.Errorf("test file has a long comment block at line %d; a test file is code only: write the test, no deliberation or questions in comments", first)
		}
		total += n
	}
	if lines := strings.Count(src, "\n") + 1; lines > 0 && float64(total) > maxCommentShare*float64(lines) {
		return fmt.Errorf("test file is %d%% comments (%d of %d lines); a test file is code only: write the test, no deliberation or questions in comments", total*100/lines, total, lines)
	}
	return nil
}

// checkTestSource refuses a test file that is not the one asked for.
func checkTestSource(src, pkg, funcName, module string) error {
	return checkTestSourceFor(src, pkg, funcName, module, whyNone)
}

// whyPrompt is the opening clause of the prompt's internal-test block.
func whyPrompt(why string) string {
	if why == whyTypes {
		return "The signature of the function under test uses unexported types, which only code in the same package can build"
	}
	return "The function under test is unexported"
}

// unexportedRefusal is the refusal for an external test package around a
// function under test that only the package itself can call or build
// arguments for. It quotes nothing from the reply.
func unexportedRefusal(clause, why string) string {
	reason := "the function under test is unexported"
	if why == whyTypes {
		reason = "the signature of the function under test uses unexported types, which only code in the same package can build"
	}
	return fmt.Sprintf("%s, so the test file must declare package %s (not %s_test) and call it without a qualifier", reason, clause, clause)
}

// checkTestSourceFor is checkTestSource that also knows why (whyNone, whyFunc,
// whyTypes) the test has to be internal. Then only the plain package clause is
// accepted.
func checkTestSourceFor(src, pkg, funcName, module, why string) error {
	return checkTestSourceIn(src, pkg, funcName, module, why, importSet{})
}

// importSet is what the contract says about the packages of this module. A zero
// value means unknown: any path under the module is accepted.
type importSet struct {
	all        []string // every package path, main packages included
	importable []string // the ones a test may import
}

const maxListedPackages = 12

// importRefusal is the refusal for an import that is neither standard library
// nor a package of this module. It quotes nothing from the reply: the module
// path and the package list come from the contract.
func importRefusal(module string, pkgs importSet) string {
	msg := "test file imports a package outside the standard library and this module"
	if module == "" {
		return msg
	}
	list := pkgs.importable
	if len(list) == 0 {
		return fmt.Sprintf("%s; the module path is %s", msg, module)
	}
	if len(list) > maxListedPackages {
		list = list[:maxListedPackages]
	}
	return fmt.Sprintf("%s; the module path is %s and the packages of this module are: %s; import only those, spelled exactly", msg, module, strings.Join(list, ", "))
}

// checkTestSourceIn is checkTestSourceFor that also checks the imports under
// the module path against the contract's packages when pkgs is known.
func checkTestSourceIn(src, pkg, funcName, module, why string, pkgs importSet) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("test file (%d bytes) is not valid Go: %s", len(src), syntaxErr(err))
	}
	if err := checkCommentVolume(fset, f, src); err != nil {
		return err
	}
	if why != whyNone && PackageClause(pkg) != "main" {
		if clause := path.Base(pkg); f.Name.Name == clause+"_test" {
			return errors.New(unexportedRefusal(clause, why))
		}
	}
	if names := packageClauseNames(pkg); !slices.Contains(names, f.Name.Name) {
		if PackageClause(pkg) == "main" {
			return errors.New(mainRefusal)
		}
		return fmt.Errorf("test file is not in package %s", strings.Join(names, " or "))
	}
	self := ""
	if module != "" && PackageClause(pkg) == "main" {
		self = module
		if d := path.Clean(pkg); d != "." {
			self = module + "/" + d
		}
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return errors.New("test file has an unreadable import")
		}
		if self != "" && p == self {
			return errors.New(mainRefusal)
		}
		first, _, _ := strings.Cut(p, "/")
		standard := stdTopLevel[first]
		own := module != "" && (p == module || strings.HasPrefix(p, module+"/"))
		if own && len(pkgs.all) > 0 && !slices.Contains(pkgs.all, p) {
			own = false
		}
		if !standard && !own {
			return errors.New(importRefusal(module, pkgs))
		}
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != funcName {
			continue
		}
		if ps := fd.Type.Params; ps != nil && len(ps.List) == 1 {
			if star, ok := ps.List[0].Type.(*ast.StarExpr); ok {
				if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "T" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "testing" {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("test file declares %s but not as func %s(t *testing.T)", funcName, funcName)
	}
	return fmt.Errorf("test file has no func %s(t *testing.T)", funcName)
}

// finishTree writes the complete tree, checks it, records the test files the
// executor must never let an implementer edit, and creates the blackboard rows.
func (p *Planner) finishTree(ctx context.Context, r *run, c *contract.Contracts, dec decomposed, st testwriterState, w map[string]int) error {
	cov, err := ReadCoverage(r.dir)
	if err != nil {
		return err
	}
	root, comps, err := buildSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, cov.RootTests)
	if err != nil {
		return err
	}
	root["requirement_ids"] = toAnySlice(allRequirementIDs(cov))
	for _, doc := range comps {
		cid, _ := doc["id"].(string)
		ids := map[string]bool{}
		for _, d := range dec.Components[cid] {
			fid, _ := d["id"].(string)
			for _, q := range requirementIDsFor(cov, fid, cid) {
				ids[q] = true
			}
		}
		for _, q := range requirementIDsFor(cov, cid, "") {
			ids[q] = true
		}
		doc["requirement_ids"] = toAnySlice(sortedKeys(ids))
	}
	docs := append([]map[string]any{root}, comps...)
	files := []string{}
	leafTests := map[string]LeafTest{}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			wt, ok := st.Nodes[id]
			if !ok {
				return fmt.Errorf("node %s has no tests", id)
			}
			doc, err := copyDoc(d)
			if err != nil {
				return err
			}
			doc["tests"], doc["wave"] = wt.Tests, w[id]
			if ct, ok := doc["contract"].(map[string]any); ok {
				if err := linkErrorTests(ct, wt.Tests); err != nil {
					return fmt.Errorf("node %s: %w", id, err)
				}
			}
			doc["requirement_ids"] = toAnySlice(requirementIDsFor(cov, id, comp.ID))
			docs = append(docs, doc)
			files = append(files, wt.TestFile)
			leafTests[id] = LeafTest{TestFile: wt.TestFile, TestFunc: testFuncName(id), SHA256: wt.SHA256}
		}
	}
	qs, err := loadQStore(r)
	if err != nil {
		return err
	}
	settled := map[string]bool{}
	for _, q := range qs.Questions {
		if q.Status == qSettled {
			settled[q.ID] = true
		}
	}
	for _, doc := range docs {
		for _, did := range strList(doc["decision_ids"]) {
			if !settled[did] {
				return fmt.Errorf("node %v cites decision %s, which is not a settled question", doc["id"], boundedID(did))
			}
		}
	}
	embedDecisions(qs, nil, docs[1+len(comps):])
	if bad := finishGroupDefects(docs, qs); len(bad) > 0 {
		return fmt.Errorf("the finished tree is inconsistent: %s", strings.Join(bad[:min(len(bad), 5)], "; "))
	}
	nodes := make([]tree.Node, 0, len(docs))
	for _, doc := range docs {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		n, err := tree.ParseNode(raw)
		if err != nil {
			var ve *jsonschema.ValidationError
			if errors.As(err, &ve) {
				return fmt.Errorf("node %v: %s", doc["id"], schemaErr(ve))
			}
			return fmt.Errorf("node %v is not a valid tree node", doc["id"])
		}
		nodes = append(nodes, n)
	}
	t, err := tree.NewTree(nodes)
	if err != nil {
		return err
	}
	store := tree.NewStore(r.dir)
	if err := store.WriteAll(t); err != nil {
		return err
	}
	loaded, err := store.Load()
	if err != nil {
		return err
	}
	if err := loaded.CheckStructure(); err != nil {
		return err
	}
	if err := loaded.CheckWaves(); err != nil {
		return err
	}
	computed, err := loaded.ComputeWaves()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(computed))
	for id, wave := range computed {
		if w[id] != wave {
			return fmt.Errorf("node %s: the plan showed wave %d but the tree computes %d", id, w[id], wave)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sort.Strings(files)
	accept := map[string]LeafTest{}
	for _, b := range goBullets(r.reqs, cov.Serve) {
		wt, ok := st.Nodes[acceptKey(b)]
		if !ok {
			return fmt.Errorf("acceptance requirement %s has no test", b.Requirement)
		}
		files = append(files, wt.TestFile)
		accept[b.Requirement] = LeafTest{TestFile: wt.TestFile, TestFunc: b.Func, SHA256: wt.SHA256}
	}
	sort.Strings(files)
	if err := writeJSON(r.path(stateAcceptTests), accept); err != nil {
		return err
	}
	if err := writeJSON(r.path(stateTestFiles), files); err != nil {
		return err
	}
	if err := writeJSON(r.path(stateLeafTests), leafTests); err != nil {
		return err
	}
	if p.d.Board != nil {
		if err := p.d.Board.InitRun(ctx, r.id, ids, computed); err != nil {
			return err
		}
	}
	r.status.PlannedAt = p.d.Now().UTC().Format("2006-01-02T15:04:05Z")
	return r.saveStatus()
}

// beforeTestWrite is a seam for tests: it runs between the model call and the
// last checks before the write.
var beforeTestWrite = func() {}

// osReason is the reason of a file system error without the path it names,
// which may have come from a model.
func osReason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "file system error"
}

var (
	memberOpenRe  = regexp.MustCompile(`^\s*"(?:[^"\\]|\\.)*"\s*:\s*"`)
	elementOpenRe = regexp.MustCompile(`^\s*"`)
)

// closeStrayStrings closes a string that a pretty-printed line leaves open.
// A line that is a member (indent "key": "value...) or an array element
// (indent "value...), optionally ending in a comma, with an odd number of
// unescaped quotes gets one quote added before the trailing comma, or at the
// end of the line. A quote after an odd run of backslashes is escaped. A
// string cannot span lines, so lines are judged on their own. It reports
// whether anything changed.
func closeStrayStrings(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	changed := false
	for i, ln := range lines {
		if !memberOpenRe.MatchString(ln) && !elementOpenRe.MatchString(ln) {
			continue
		}
		if unescapedQuotes(ln)%2 == 0 {
			continue
		}
		body := strings.TrimRight(ln, " \t\r")
		tail := ln[len(body):]
		if strings.HasSuffix(body, ",") {
			body = strings.TrimSuffix(body, ",") + `",`
		} else {
			body += `"`
		}
		lines[i] = body + tail
		changed = true
	}
	return strings.Join(lines, "\n"), changed
}

func unescapedQuotes(s string) int {
	n, slashes := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			slashes++
			continue
		case '"':
			if slashes%2 == 0 {
				n++
			}
		}
		slashes = 0
	}
	return n
}

// packageSet is the importSet of a contract.
func packageSet(c *contract.Contracts) importSet {
	all, imp := contractPackagePaths(c)
	return importSet{all: all, importable: imp}
}
