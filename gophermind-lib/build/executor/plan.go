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
	"reflect"
	"sort"
	"strings"
	"sync"

	"gophermind/gophermind-lib/build/acceptcheck"
	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/packer"
	"gophermind/gophermind-lib/build/pathsafe"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/revlog"
	"gophermind/gophermind-lib/build/tree"
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
	// Revised maps a node id to the tip of its revision chain (sub-project E).
	// Plan.Hashes holds revision 0 for such a node; planIntact compares the node
	// file to this tip. Guarded by revMu.
	Revised map[string]RevisedTip
	keys    map[string]string // node id -> "tree/<path>"
	rootNet []NetHost
	fns     map[string]contract.Function
	revMu   sync.Mutex
	// chainMu orders the writes of a revision transaction (each one under the
	// write lock) against planIntact's re-proof of the chains (the read lock),
	// so the re-proof always sees the files between two writes.
	chainMu sync.RWMutex
}

// RevisedTip is where the revision chain of one node stands.
type RevisedTip struct {
	Key                    string // "tree/<path>"
	N                      int    // last committed revision, 0 when only an intent exists
	BodySHA256, TestSHA256 string
	Open                   *revlog.Record // an intent not yet committed or refused
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
	Revision                                         int
	Description                                      string
	Behaviour                                        []string
}

// NetHost is one host of a leaf's network block.
type NetHost struct {
	Host     string
	Critical bool
}

// leafDoc is the part of a function node the executor reads. tree.Node keeps
// the decoded document private, so the node is marshalled and decoded again.
type leafDoc struct {
	Title       string `json:"title"`
	ModelTier   string `json:"model_tier"`
	Description string `json:"description"`
	Revision    int    `json:"revision"`
	Contract    struct {
		ID          string                                     `json:"id"` // not written by the planner today; the node id is the function id
		File        string                                     `json:"file"`
		Signature   string                                     `json:"signature"`
		Package     string                                     `json:"package"`
		Inputs      []struct{ Name, Type, Description string } `json:"inputs"`
		Outputs     []struct{ Name, Type, Description string } `json:"outputs"`
		Errors      []struct{ When, Returns string }           `json:"errors"`
		SideEffects []string                                   `json:"side_effects"`
	} `json:"contract"`
	Context struct {
		DependencySignatures []string `json:"dependency_signatures"`
		Constraints          []string `json:"constraints"`
		Notes                []string `json:"notes"`
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

	// Caller contract: hashes[key] for a revised node is the hash of its staged
	// body-0, anchored to the approved tree only where the caller compares it with
	// State.PlanHashes (the resume and retryWave0 DeepEqual). LoadPlan alone proves
	// the chain is self-consistent and matches the files on disk, nothing more;
	// any other caller gets no anchor to the approved plan.
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
	revised, err := verifyRevisions(runDir, repo, functionPaths(tr), hashes, tests)
	if err != nil {
		return nil, err
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

	fns := map[string]contract.Function{}
	for _, f := range c.Functions {
		fns[f.ID] = f
	}
	rootNet, err := rootNetwork(tr, c.BriefID)
	if err != nil {
		return nil, err
	}

	p := &Plan{
		RunID: c.BriefID, RunDir: runDir, Repo: repo,
		Brief: b, BriefRepo: b.Front.Repo, Contracts: c,
		byID: map[string]*Leaf{}, Requirements: reqs, Coverage: cov, Deps: deps, Classes: classes, Hashes: hashes,
		Serve: cov.Serve, AcceptTests: accept,
		Revised: revised, keys: map[string]string{}, rootNet: rootNet, fns: fns,
	}
	if err := p.checkAcceptTests(repo); err != nil {
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
		p.keys[l.ID] = "tree/" + n.Path()
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

	guidance, err := guidanceFrom(d)
	if err != nil {
		return nil, fmt.Errorf("executor: leaf %s: %v", n.ID, err)
	}
	l := &Leaf{
		ID: n.ID, Title: d.Title, Class: p.Classes[n.ID], Tier: d.ModelTier,
		Package: d.Contract.Package, File: d.Contract.File, Signature: d.Contract.Signature,
		FuncID: funcID, DependsOn: append([]string{}, n.DependsOn...),
		Dir:      path.Dir(d.Contract.File),
		TestFile: lt.TestFile, TestFunc: lt.TestFunc, TestSHA256: lt.SHA256,
		Constraints: d.Context.Constraints, DepSignatures: d.Context.DependencySignatures,
		MaxContextTokens: d.Budget.MaxContextTokens, MaxRevisions: -1,
		Guidance: guidance, Revision: d.Revision,
	}
	if l.Revision >= 1 {
		l.Description, l.Behaviour = d.Description, behaviourFrom(d)
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
		Guidance: l.Guidance, Description: l.Description, Behaviour: l.Behaviour,
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

// behaviourFrom is the detail lines of a revised node's contract text.
func behaviourFrom(d leafDoc) []string {
	var out []string
	for _, in := range d.Contract.Inputs {
		out = append(out, fmt.Sprintf("Input %s (%s): %s", in.Name, in.Type, in.Description))
	}
	for _, o := range d.Contract.Outputs {
		out = append(out, fmt.Sprintf("Output %s (%s): %s", o.Name, o.Type, o.Description))
	}
	for _, e := range d.Contract.Errors {
		out = append(out, fmt.Sprintf("Error when %s: returns %s", e.When, e.Returns))
	}
	for _, s := range d.Contract.SideEffects {
		out = append(out, "Side effect: "+s)
	}
	for _, n := range d.Context.Notes {
		out = append(out, "Note: "+n)
	}
	return out
}

// errPlanChanged is the stop for any plan file, chain or record that is not
// what the run was started with.
func errPlanChanged() *stopError {
	return &stopError{Status: "failed", Reason: "plan_changed", Message: "executor: a plan file changed while the run was in progress"}
}

func (p *Plan) nodeKey(id string) string { return p.keys[id] }

func (p *Plan) tip(id string) (RevisedTip, bool) {
	p.revMu.Lock()
	defer p.revMu.Unlock()
	t, ok := p.Revised[id]
	return t, ok
}

func (p *Plan) setTip(id string, t RevisedTip) {
	p.revMu.Lock()
	defer p.revMu.Unlock()
	if p.Revised == nil {
		p.Revised = map[string]RevisedTip{}
	}
	p.Revised[id] = t
}

// accepts says whether hash is an allowed content for the node file at key:
// the expected one, or for a node whose intent is open (staged, not yet
// committed) the open revision's body as well, for that node only.
func (p *Plan) accepts(key, hash string) bool {
	p.revMu.Lock()
	defer p.revMu.Unlock()
	for _, t := range p.Revised {
		if t.Key == key && t.Open != nil && hash == t.Open.BodySHA256 {
			return true
		}
	}
	return false
}

// expected is Plan.Hashes with every revised node at its chain tip: what planIntact holds the files to.
func (p *Plan) expected() map[string]string {
	p.revMu.Lock()
	defer p.revMu.Unlock()
	out := make(map[string]string, len(p.Hashes))
	for k, v := range p.Hashes {
		out[k] = v
	}
	for _, t := range p.Revised {
		out[t.Key] = t.BodySHA256
	}
	return out
}

// nodeDisk reads what is on disk now for one function node: the node file, its
// entry in leaf_tests.json (given) and its test file.
func nodeDisk(runDir, repo, rel string, lt planner.LeafTest) (revlog.Disk, error) {
	node, err := readPlanFile(runDir, rel)
	if err != nil {
		return revlog.Disk{}, err
	}
	abs, err := pathsafe.ResolveTest(repo, lt.TestFile)
	if err != nil {
		return revlog.Disk{}, errPlanChanged()
	}
	test, err := readNoFollow(abs)
	if err != nil {
		return revlog.Disk{}, errPlanChanged()
	}
	return revlog.Disk{NodeSHA256: revlog.Hash(node), LeafTestSHA256: lt.SHA256, TestFileSHA256: revlog.Hash(test)}, nil
}

// nodeRevision is the revision field of a node file (0 when absent).
func nodeRevision(runDir, rel string) (int, error) {
	raw, err := readPlanFile(runDir, rel)
	if err != nil {
		return 0, err
	}
	var d struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return 0, err
	}
	return d.Revision, nil
}

// functionPaths maps every function node id of the tree to its node file path.
func functionPaths(tr *tree.Tree) map[string]string {
	out := map[string]string{}
	for _, n := range tr.Nodes {
		if n.Kind == tree.KindFunction {
			out[n.ID] = n.Path()
		}
	}
	return out
}

// chainWrite runs one write of a revision transaction under the chain lock.
func (p *Plan) chainWrite(f func() error) error {
	p.chainMu.Lock()
	defer p.chainMu.Unlock()
	return f()
}

// chainsIntact proves every revision chain again with verifyRevisions, as
// LoadPlan does (hashes and the frozen-field check, no test run): body-0 must
// still be the revision 0 the plan was loaded with, and each chain's committed
// revision must be the one this run holds, or the open intent's (a commit
// record written, the tip not yet moved). The read lock is taken before the
// memory is copied: a writer writes the files under the write lock and moves
// the memory after, so under the read lock the memory can lag the files and
// never lead them. Lock order: chainMu, then revMu.
func (p *Plan) chainsIntact() bool {
	p.chainMu.RLock()
	defer p.chainMu.RUnlock()
	p.revMu.Lock()
	mem := make(map[string]RevisedTip, len(p.Revised))
	for id, t := range p.Revised {
		mem[id] = t
	}
	p.revMu.Unlock()
	paths := make(map[string]string, len(p.keys))
	for id, key := range p.keys {
		paths[id] = strings.TrimPrefix(key, "tree/")
	}
	tests, err := planner.ReadLeafTests(p.RunDir)
	if err != nil {
		return false
	}
	base := map[string]string{}
	disk, err := verifyRevisions(p.RunDir, p.Repo, paths, base, tests)
	if err != nil {
		return false
	}
	for key, h := range base {
		if p.Hashes[key] != h {
			return false
		}
	}
	for id, d := range disk {
		m := mem[id]
		if d.N != m.N && (m.Open == nil || m.Open.N != d.N) {
			return false
		}
	}
	for id, m := range mem {
		if _, ok := disk[id]; !ok && m.N > 0 {
			return false
		}
	}
	return true
}

// verifyRevisions proves the revision chain of every function node (id to node
// file path) that has a folder in _state/revisions, and points hashes at
// revision 0 for it. A node with no folder is unrevised. It never writes.
func verifyRevisions(runDir, repo string, fnPaths map[string]string, hashes map[string]string, tests map[string]planner.LeafTest) (map[string]RevisedTip, error) {
	out := map[string]RevisedTip{}
	for id, path := range fnPaths {
		dir, err := revlog.NodeDir(runDir, id)
		if err != nil {
			return nil, errPlanChanged()
		}
		// A node file's own revision field must agree with its chain: a node that
		// says revision 1 with no folder, or a folder deleted under a revised
		// node, is not the plan that was approved.
		nodeRev, err := nodeRevision(runDir, path)
		if err != nil {
			return nil, errPlanChanged()
		}
		if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
			if nodeRev != 0 {
				return nil, errPlanChanged()
			}
			continue
		}
		lt, ok := tests[id]
		if !ok {
			return nil, errPlanChanged()
		}
		key := "tree/" + path
		disk, err := nodeDisk(runDir, repo, path, lt)
		if err != nil {
			return nil, errPlanChanged()
		}
		tip, err := revlog.Verify(dir, disk)
		if err != nil {
			return nil, errPlanChanged()
		}
		if tip.N == 0 && tip.Open == nil {
			// revlog reads a missing log as an empty one: a folder with files but
			// no record at all is a deleted or emptied log, not an unrevised node.
			recs, err := revlog.Read(dir)
			entries, err2 := os.ReadDir(dir)
			if err != nil || err2 != nil || (len(recs) == 0 && len(entries) > 0 && !stagedOnly(dir, entries, disk)) || nodeRev != 0 {
				return nil, errPlanChanged()
			}
			continue
		}
		if nodeRev != tip.N && (tip.Open == nil || nodeRev != tip.Open.N) {
			return nil, errPlanChanged()
		}
		body0, _, err := revlog.ReadStaged(dir, 0)
		if err != nil {
			return nil, errPlanChanged()
		}
		hashes[key] = revlog.Hash(body0)
		out[id] = RevisedTip{Key: key, N: tip.N, BodySHA256: tip.BodySHA256, TestSHA256: tip.TestSHA256, Open: tip.Open}
	}
	return out, nil
}

// ReloadLeaf re-reads the node file and leaf_tests.json of one leaf and updates
// the Leaf in place: the pointer every holder has stays valid. Only the parts
// a revision may change are copied; a changed frozen part is refused.
func (p *Plan) ReloadLeaf(id string) error {
	old := p.byID[id]
	if old == nil || p.keys[id] == "" {
		return fmt.Errorf("executor: leaf %s is not in the plan", id)
	}
	raw, err := readPlanFile(p.RunDir, strings.TrimPrefix(p.keys[id], "tree/"))
	if err != nil {
		return err
	}
	n, err := tree.ParseNode(raw)
	if err != nil {
		return errPlanChanged()
	}
	tests, err := planner.ReadLeafTests(p.RunDir)
	if err != nil {
		return fmt.Errorf("executor: _state/leaf_tests.json is not readable (%s)", planner.JSONErr(err))
	}
	fresh, err := p.leaf(n, p.rootNet, p.fns, tests, p.Repo)
	if err != nil {
		return err
	}
	if fresh.Package != old.Package || fresh.File != old.File || fresh.Signature != old.Signature || fresh.Tier != old.Tier ||
		fresh.Wave != old.Wave || fresh.TestFile != old.TestFile || fresh.TestFunc != old.TestFunc || !reflect.DeepEqual(fresh.DependsOn, old.DependsOn) {
		return errPlanChanged()
	}
	old.Revision, old.Description, old.Behaviour = fresh.Revision, fresh.Description, fresh.Behaviour
	old.Constraints, old.TestSHA256, old.Guidance = fresh.Constraints, fresh.TestSHA256, fresh.Guidance
	return nil
}

// stagedOnly says a folder with no log line is the residue of a kill between
// staging (step 2) and the intent record (step 3): it holds only the staged
// files of revisions 0 and 1, and the node and its test are still what
// revision 0 staged, and a staged revision 1 changes no frozen field. Anything
// else with no record is a deleted or emptied log. It compares with the STAGED
// revision 0, not with the approved hash (LoadPlan has no run state): a
// rewritten node-and-body-0 pair is caught downstream, where resume compares
// State.PlanHashes with Plan.Hashes and the approval is verified. A lone
// body-0.json (a kill between the two writes of staging revision 0) is not yet
// staged when it still equals the node file and the test is as planned; the
// next revision stages the pair again (revlog.Stage).
func stagedOnly(dir string, entries []os.DirEntry, d revlog.Disk) bool {
	allowed := map[string]bool{revlog.BodyFile(0): true, revlog.TestFile(0): true, revlog.BodyFile(1): true, revlog.TestFile(1): true}
	for _, e := range entries {
		if !allowed[e.Name()] || !e.Type().IsRegular() {
			return false
		}
	}
	if len(entries) == 1 && entries[0].Name() == revlog.BodyFile(0) {
		body0, err := os.ReadFile(filepath.Join(dir, revlog.BodyFile(0)))
		return err == nil && d.NodeSHA256 == revlog.Hash(body0) && d.LeafTestSHA256 == d.TestFileSHA256
	}
	body0, test0, err := revlog.ReadStaged(dir, 0)
	if err != nil {
		return false
	}
	if body1, rerr := os.ReadFile(filepath.Join(dir, revlog.BodyFile(1))); rerr == nil && revlog.CheckFrozen(body0, body1) != nil {
		return false
	}
	return d.NodeSHA256 == revlog.Hash(body0) && d.LeafTestSHA256 == revlog.Hash(test0) && d.TestFileSHA256 == revlog.Hash(test0)
}
