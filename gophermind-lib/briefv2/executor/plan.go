package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

// Plan is an approved plan as the executor reads it. It is read-only: every
// file it came from is hashed at load (Hashes) and must still have the same
// hash at the end of the run.
type Plan struct {
	RunID, RunDir, Repo string
	Brief               *brief.Brief
	BriefRepo           string // the brief's repo field
	Contracts           *contract.Contracts
	Leaves              []*Leaf // sorted by (Wave, ID)
	byID                map[string]*Leaf
	Requirements        []planner.Requirement
	Coverage            planner.CoverageFile
	Serve               *planner.Serve              // how to start the server for the Go acceptance tests, or nil
	AcceptTests         map[string]planner.LeafTest // acceptance requirement id -> its Go test (the planner's manifest)
	Deps                []planner.Dependency
	Classes             map[string]string
	Hashes              map[string]string // "contracts.json", "tree/<path>" -> hex sha256, taken at load
}

// Leaf is one function node.
type Leaf struct {
	ID, Title, Class, Tier, Package, File, Signature string
	FuncID                                           string // contract function id
	Wave                                             int
	DependsOn                                        []string
	Dir                                              string // slash dir of File, "." for the repo root
	StubFile                                         string // <Dir>/zz_gm_stub_<ID>.go (slash, repo relative)
	TestFile, TestFunc, TestSHA256                   string
	Constraints                                      []string
	DepSignatures                                    []string
	MaxContextTokens                                 int // 0 = brief, then settings default
	MaxRevisions                                     int // -1 = settings default
	Network                                          []NetHost
	Guidance                                         []packer.Guidance
}

// NetHost is one host of a leaf's network block.
type NetHost struct {
	Host     string
	Critical bool
}

// leafDoc is the part of a function node the executor reads. tree.Node keeps
// the decoded document private, so the node is marshalled and decoded again.
type leafDoc struct {
	Title     string `json:"title"`
	ModelTier string `json:"model_tier"`
	Contract  struct {
		ID        string `json:"id"` // not written by the planner today; the node id is the function id
		File      string `json:"file"`
		Signature string `json:"signature"`
		Package   string `json:"package"`
	} `json:"contract"`
	Context struct {
		DependencySignatures []string `json:"dependency_signatures"`
		Constraints          []string `json:"constraints"`
	} `json:"context"`
	Budget struct {
		MaxContextTokens int  `json:"max_context_tokens"`
		MaxRevisions     *int `json:"max_revisions"`
	} `json:"budget"`
	Network []struct {
		Host     string `json:"host"`
		Critical bool   `json:"critical"`
	} `json:"network"`
	Construction *struct {
		ApproachChosen string   `json:"approach_chosen"`
		Steps          []string `json:"steps"`
	} `json:"construction"`
	Security      json.RawMessage `json:"security"`
	Observability json.RawMessage `json:"observability"`
	Performance   json.RawMessage `json:"performance"`
	Portability   json.RawMessage `json:"portability"`
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// LoadPlan reads the plan in runDir. It verifies the approval first and
// returns that error unchanged when the plan is not the approved one: nothing
// is read on trust. It never writes.
func LoadPlan(runDir, repo string) (*Plan, error) {
	if err := planner.VerifyApproval(runDir); err != nil {
		return nil, err
	}

	briefRaw, err := readPlanFile(runDir, "brief.md")
	if err != nil {
		return nil, err
	}
	b, err := brief.Parse(briefRaw)
	if err != nil {
		return nil, errors.New("executor: brief.md is not a valid brief")
	}
	contractsRaw, err := readPlanFile(runDir, "contracts.json")
	if err != nil {
		return nil, err
	}
	c, err := contract.Load(contractsRaw)
	if err != nil {
		return nil, errors.New("executor: contracts.json is not a valid contract")
	}

	hashes := map[string]string{"contracts.json": hashHex(contractsRaw)}
	tr, err := tree.NewStore(runDir).Load()
	if err != nil {
		// A node that fails its schema is not described: the message could quote plan text.
		return nil, errors.New("executor: the task tree is not readable (a node file is invalid, misplaced or duplicated, or a dependency is unknown)")
	}
	// The remaining tree errors name node ids only.
	if err := tr.CheckStructure(); err != nil {
		return nil, fmt.Errorf("executor: the task tree is not valid: %w", err)
	}
	if err := tr.CheckWaves(); err != nil {
		return nil, fmt.Errorf("executor: the task tree is not valid: %w", err)
	}
	if err := hashNodes(runDir, tr, hashes); err != nil {
		return nil, err
	}

	tests, err := planner.ReadLeafTests(runDir)
	if err != nil {
		return nil, fmt.Errorf("executor: _state/leaf_tests.json is not readable (%s)", planner.JSONErr(err))
	}
	classes, err := planner.ReadClasses(runDir)
	if err != nil {
		return nil, fmt.Errorf("executor: _state/classes.json is not readable (%s)", planner.JSONErr(err))
	}
	reqs, err := planner.ReadRequirements(runDir)
	if err != nil {
		return nil, errors.New("executor: requirements.json is not readable")
	}
	cov, err := planner.ReadCoverage(runDir)
	if err != nil {
		return nil, errors.New("executor: coverage.json is not readable")
	}
	deps, err := planner.ReadDependencies(runDir)
	if err != nil {
		return nil, errors.New("executor: dependencies.json is not readable")
	}

	accept, err := planner.ReadAcceptanceTests(runDir)
	if err != nil {
		return nil, fmt.Errorf("executor: _state/acceptance_tests.json is not readable (%s)", planner.JSONErr(err))
	}

	p := &Plan{
		RunID: c.BriefID, RunDir: runDir, Repo: repo,
		Brief: b, BriefRepo: b.Front.Repo, Contracts: c,
		byID: map[string]*Leaf{}, Requirements: reqs, Coverage: cov, Deps: deps, Classes: classes, Hashes: hashes,
		Serve: cov.Serve, AcceptTests: accept,
	}
	if err := p.checkAcceptTests(repo); err != nil {
		return nil, err
	}
	fns := map[string]contract.Function{}
	for _, f := range c.Functions {
		fns[f.ID] = f
	}

	rootNet, err := rootNetwork(tr, c.BriefID)
	if err != nil {
		return nil, err
	}
	for _, n := range tr.Nodes {
		if n.Kind != tree.KindFunction {
			continue
		}
		l, err := p.leaf(n, rootNet, fns, tests, repo)
		if err != nil {
			return nil, err
		}
		p.Leaves = append(p.Leaves, l)
		p.byID[l.ID] = l
	}
	sort.Slice(p.Leaves, func(i, j int) bool {
		a, b := p.Leaves[i], p.Leaves[j]
		if a.Wave != b.Wave {
			return a.Wave < b.Wave
		}
		return a.ID < b.ID
	})
	for _, f := range c.Functions {
		if p.byID[f.ID] == nil {
			return nil, fmt.Errorf("executor: function %s of contracts.json has no node", f.ID)
		}
	}
	// brief.md and contracts.json were read after the first check. Checking
	// the approval again now refuses a plan that changed in between.
	if err := planner.VerifyApproval(runDir); err != nil {
		return nil, err
	}
	return p, nil
}

// leaf builds one Leaf from a function node, its recorded test and the
// contract, refusing any disagreement between them.
// A leaf's Network is its own list when it has one, otherwise the root node's
// list: the planner writes the brief's network block on the root only, and
// that block (hosts and critical flags) applies to every leaf (spec 14).
func (p *Plan) leaf(n tree.Node, rootNet []NetHost, fns map[string]contract.Function, tests map[string]planner.LeafTest, repo string) (*Leaf, error) {
	raw, err := n.Marshal()
	if err != nil {
		return nil, fmt.Errorf("executor: node %s is not readable", n.ID)
	}
	var d leafDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("executor: node %s is not readable", n.ID)
	}
	funcID := d.Contract.ID
	if funcID == "" {
		funcID = n.ID
	}
	f, ok := fns[funcID]
	if !ok || f.File != d.Contract.File || f.Signature != d.Contract.Signature || f.Package != d.Contract.Package {
		return nil, fmt.Errorf("executor: leaf %s does not match contracts.json", n.ID)
	}
	lt, ok := tests[n.ID]
	if !ok {
		return nil, fmt.Errorf("executor: leaf %s has no recorded test", n.ID)
	}
	if _, err := pathsafe.ResolveSource(repo, d.Contract.File); err != nil {
		return nil, fmt.Errorf("executor: leaf %s: its source path is not allowed", n.ID)
	}
	abs, err := pathsafe.ResolveTest(repo, lt.TestFile)
	if err != nil {
		return nil, fmt.Errorf("executor: leaf %s: its test path is not allowed", n.ID)
	}
	if fi, err := os.Lstat(abs); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("executor: leaf %s: its test file is missing from the repository", n.ID)
	}
	if lt.TestFunc == "" || lt.SHA256 == "" {
		return nil, fmt.Errorf("executor: leaf %s has an incomplete recorded test", n.ID)
	}

	l := &Leaf{
		ID: n.ID, Title: d.Title, Class: p.Classes[n.ID], Tier: d.ModelTier,
		Package: d.Contract.Package, File: d.Contract.File, Signature: d.Contract.Signature,
		FuncID: funcID, DependsOn: append([]string{}, n.DependsOn...),
		Dir:      path.Dir(d.Contract.File),
		TestFile: lt.TestFile, TestFunc: lt.TestFunc, TestSHA256: lt.SHA256,
		Constraints: d.Context.Constraints, DepSignatures: d.Context.DependencySignatures,
		MaxContextTokens: d.Budget.MaxContextTokens, MaxRevisions: -1,
		Guidance: guidanceFrom(d),
	}
	if n.Wave != nil {
		l.Wave = *n.Wave
	}
	if l.Tier == "" {
		l.Tier = "standard"
	}
	if d.Budget.MaxRevisions != nil {
		l.MaxRevisions = *d.Budget.MaxRevisions
	}
	l.StubFile = path.Join(l.Dir, "zz_gm_stub_"+l.ID+".go")
	for _, h := range d.Network {
		l.Network = append(l.Network, NetHost{Host: h.Host, Critical: h.Critical})
	}
	if len(l.Network) == 0 && len(rootNet) > 0 {
		l.Network = append([]NetHost{}, rootNet...)
	}
	return l, nil
}

// Leaf returns the leaf with that node id, or nil.
func (p *Plan) Leaf(id string) *Leaf { return p.byID[id] }

// View is what the packer may know about a leaf. testSource is the leaf's
// test file as it is on disk now; the packer never reads a file.
func (p *Plan) View(l *Leaf, testSource string) packer.NodeView {
	return packer.NodeView{
		ID: l.ID, FuncID: l.FuncID, Package: l.Package, File: l.File, Signature: l.Signature,
		DependencySignatures: l.DepSignatures, Constraints: l.Constraints,
		TestFile: l.TestFile, TestSource: testSource, MaxContextTokens: l.MaxContextTokens,
		Guidance: l.Guidance,
	}
}

// Policy is the import policy of every reply: the target module and the
// modules of dependencies.json.
func (p *Plan) Policy() packer.ImportPolicy {
	deps := []string{}
	for _, d := range p.Deps {
		deps = append(deps, d.Module)
	}
	return packer.ImportPolicy{Module: p.Contracts.Module, Deps: deps}
}

// readPlanFile reads one file of the run folder. An error names the file and
// never its content.
func readPlanFile(runDir, rel string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("executor: %s is not readable", rel)
	}
	return raw, nil
}

// hashNodes records the SHA-256 of every node file under "tree/<path>". Each
// file is read once here and parsed again from those bytes: the node that was
// loaded must be the node that was hashed, so a file that changes between the
// two reads is refused instead of half used.
func hashNodes(runDir string, tr *tree.Tree, hashes map[string]string) error {
	for _, n := range tr.Nodes {
		rel := n.Path()
		raw, err := readPlanFile(runDir, rel)
		if err != nil {
			return err
		}
		again, err := tree.ParseNode(raw)
		if err != nil {
			return errors.New("executor: a plan file changed while the plan was being loaded")
		}
		want, err1 := n.Marshal()
		got, err2 := again.Marshal()
		if err1 != nil || err2 != nil || string(want) != string(got) {
			return errors.New("executor: a plan file changed while the plan was being loaded")
		}
		hashes["tree/"+rel] = hashHex(raw)
	}
	return nil
}

// rootNetwork is the network block of the root node (the brief's block).
func rootNetwork(tr *tree.Tree, rootID string) ([]NetHost, error) {
	n, ok := tr.Nodes[rootID]
	if !ok {
		return nil, errors.New("executor: the task tree has no root node")
	}
	raw, err := n.Marshal()
	if err != nil {
		return nil, errors.New("executor: the root node is not readable")
	}
	var d struct {
		Network []struct {
			Host     string `json:"host"`
			Critical bool   `json:"critical"`
		} `json:"network"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errors.New("executor: the root node is not readable")
	}
	var out []NetHost
	for _, h := range d.Network {
		out = append(out, NetHost{Host: h.Host, Critical: h.Critical})
	}
	return out, nil
}

// binNames are the directory names of the main packages under cmd/ that the
// plan's leaves write: the binaries the acceptance run will build.
func (p *Plan) binNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range p.Leaves {
		if l.Package != "main" {
			continue
		}
		rest, ok := strings.CutPrefix(l.Dir, "cmd/")
		if !ok {
			continue
		}
		name := strings.SplitN(rest, "/", 2)[0]
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// checkAcceptTests holds the Go acceptance tests the planner wrote to the same
// rules as a leaf's test: a path the sandbox allows, a regular file in the
// repository, a recorded hash, and every acceptance package root test names only
// tests of the manifest.
func (p *Plan) checkAcceptTests(repo string) error {
	byFunc := map[string]bool{}
	for id, at := range p.AcceptTests {
		abs, err := pathsafe.ResolveTest(repo, at.TestFile)
		if err != nil {
			return fmt.Errorf("executor: the acceptance test of %s: its path is not allowed", id)
		}
		if fi, err := os.Lstat(abs); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("executor: the acceptance test of %s is missing from the repository", id)
		}
		if at.TestFunc == "" || at.SHA256 == "" {
			return fmt.Errorf("executor: the acceptance test of %s is not fully recorded", id)
		}
		byFunc[at.TestFunc] = true
	}
	for _, t := range p.Coverage.RootTests {
		gt, ok := acceptcheck.ParseGoTest(t.Command)
		if !ok || !isAcceptancePkg(gt.Pkg) {
			continue
		}
		for _, fn := range gt.Funcs {
			if !byFunc[fn] {
				return fmt.Errorf("executor: the root test of %s runs an acceptance test the planner did not record", t.Requirement)
			}
		}
	}
	return nil
}

// isAcceptancePkg says a go test package argument is the acceptance package.
func isAcceptancePkg(pkg string) bool {
	return pkg == "./"+planner.AcceptanceDir || strings.HasPrefix(pkg, "./"+planner.AcceptanceDir+"/")
}

// acceptTestFiles is the repo-relative path of every acceptance test, sorted.
func (p *Plan) acceptTestFiles() []string {
	var out []string
	for _, at := range p.AcceptTests {
		out = append(out, at.TestFile)
	}
	sort.Strings(out)
	return out
}
