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
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/tree"
)

// testwriterState is _state/testwriter.json: the tests of every node that is
// finished, and the hash of the test file written for it.
type testwriterState struct {
	Nodes map[string]writtenTests `json:"nodes"`
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
	var ap approval
	found, err := readJSON(r.path(fileApproval), &ap)
	if err != nil {
		return err
	}
	_, hash, err := RenderPlan(r.dir)
	if err != nil {
		return err
	}
	if !found || ap.PlanHash != hash {
		return errors.New("approval.json does not match the plan as it stands; remove it and resume to approve the plan again")
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
		wt, err := p.writeTests(ctx, r, c, d, classes[id])
		if err != nil {
			return fmt.Errorf("node %s: %w", id, err)
		}
		st.Nodes[id] = wt
		if err := writeJSON(r.path(stateTestwriter), st); err != nil {
			return err
		}
	}
	return p.finishTree(ctx, r, c, dec, st, w)
}

// writeTests makes the model call for one node and writes its test file.
func (p *Planner) writeTests(ctx context.Context, r *run, c *contract.Contracts, d map[string]any, class string) (writtenTests, error) {
	id, _ := d["id"].(string)
	ct, _ := d["contract"].(map[string]any)
	file, _ := ct["file"].(string)
	pkg, _ := ct["package"].(string)
	funcName := testFuncName(id)
	rel := testFilePath(id, file)
	abs, err := safeTestPath(r.repo, rel)
	if err != nil {
		return writtenTests{}, err
	}
	if exists(abs) {
		return writtenTests{}, fmt.Errorf("test file %s already exists and this run did not write it; move it away, then resume", path.Base(rel))
	}
	prompt, err := render("testwriter", map[string]string{
		"Node": mustJSON(d), "PackageDir": path.Dir(file), "TestFuncName": funcName, "TestFile": rel})
	if err != nil {
		return writtenTests{}, err
	}
	var tests []map[string]any
	var source string
	cs := callSpec{stage: "testwrite:" + id, taskType: "testwrite", nodeID: id, nodeClass: class, scope: router.ScopeNode, maxTokens: maxTokensTestwrite}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		ts, src, err := parseTestwrite(StripReply(text), ct, pkg, funcName, c.Module)
		if err != nil {
			return err
		}
		tests, source = ts, src
		return nil
	})
	if err != nil {
		return writtenTests{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return writtenTests{}, fmt.Errorf("creating the folder for %s: %s", path.Base(rel), osReason(err))
	}
	if err := os.WriteFile(abs, []byte(source), 0o644); err != nil {
		return writtenTests{}, fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	sum := sha256.Sum256([]byte(source))
	return writtenTests{Tests: tests, TestFile: rel, SHA256: hex.EncodeToString(sum[:])}, nil
}

// parseTestwrite checks a Test-writer reply: enough tests, each described,
// and a Go test file that parses, holds the expected test function, and
// imports nothing outside the standard library and the module. Level and
// command are set here, whatever the model wrote.
func parseTestwrite(text string, ct map[string]any, pkg, funcName, module string) ([]map[string]any, string, error) {
	var reply struct {
		Tests    []map[string]any `json:"tests"`
		TestFile string           `json:"test_file"`
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, "", fmt.Errorf("test-writer reply is not a JSON object (%s)", jsonErr(err))
	}
	need := len(objects(ct["errors"])) + 1
	if len(reply.Tests) < need {
		return nil, "", fmt.Errorf("test-writer reply has %d tests; the contract lists %d error conditions, so at least %d are needed (the happy path and one per condition)",
			len(reply.Tests), need-1, need)
	}
	file, _ := ct["file"].(string)
	command := testCommand(file, funcName)
	tests := make([]map[string]any, 0, len(reply.Tests))
	for i, t := range reply.Tests {
		name, _ := t["name"].(string)
		given, okGiven := t["given"].(string)
		expect, okExpect := t["expect"].(string)
		if strings.TrimSpace(name) == "" || !okGiven || !okExpect || strings.TrimSpace(expect) == "" {
			return nil, "", fmt.Errorf("test %d needs a name, a given and an expect", i+1)
		}
		tests = append(tests, map[string]any{"name": name, "level": "unit", "given": given, "expect": expect, "command": command})
	}
	if err := checkTestSource(reply.TestFile, pkg, funcName, module); err != nil {
		return nil, "", err
	}
	return tests, reply.TestFile, nil
}

// checkTestSource refuses a test file that is not the one asked for.
func checkTestSource(src, pkg, funcName, module string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("test file (%d bytes) is not valid Go: %s", len(src), syntaxErr(err))
	}
	if f.Name.Name != pkg && f.Name.Name != pkg+"_test" {
		return fmt.Errorf("test file is not in package %s or %s_test", pkg, pkg)
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return errors.New("test file has an unreadable import")
		}
		first, _, _ := strings.Cut(p, "/")
		standard := !strings.Contains(first, ".")
		own := module != "" && (p == module || strings.HasPrefix(p, module+"/"))
		if !standard && !own {
			return fmt.Errorf("test file imports a package (%d bytes) outside the standard library and this module; only the standard library and this module (%s) are allowed", len(p), module)
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

// safeTestPath returns where rel lands inside repo, refusing anything that is
// not a test file strictly inside the repository, including a path that would
// pass through a symbolic link pointing out of it.
func safeTestPath(repo, rel string) (string, error) {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("test file path (%d bytes) is not a clean path inside the repository", len(rel))
	}
	if !strings.HasSuffix(rel, "_test.go") {
		return "", fmt.Errorf("test file path (%d bytes) does not end in _test.go", len(rel))
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	// The deepest directory on the way that already exists decides where the
	// file would really be written.
	dir := filepath.Dir(abs)
	for !exists(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("test file path (%d bytes) resolves outside the repository", len(rel))
	}
	return abs, nil
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
	docs := append([]map[string]any{root}, comps...)
	files := []string{}
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
			docs = append(docs, doc)
			files = append(files, wt.TestFile)
		}
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
	if err := writeJSON(r.path(stateTestFiles), files); err != nil {
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

// osReason is the reason of a file system error without the path it names,
// which may have come from a model.
func osReason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "file system error"
}
