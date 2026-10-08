package planner

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/brief"
)

// factPackage is a package directory of the target repo and the names it
// exports. Names only: never a body, a comment or a string literal.
type factPackage struct {
	Dir     string   `json:"dir"`
	Exports []string `json:"exports,omitempty"`
}

// factsFile is _state/clarify/facts.json: what the harness established about
// the run before asking anything. The planner makes model calls only, so the
// probe runs no command and no network call; it reads go.mod, directory
// listings and the repo's Go source.
type factsFile struct {
	RepoExists  bool          `json:"repo_exists"`
	GitRepo     bool          `json:"git_repo"`
	Module      string        `json:"go_module,omitempty"`
	GoVersion   string        `json:"go_version,omitempty"`
	Packages    []factPackage `json:"packages,omitempty"`
	OS          string        `json:"os"`
	Arch        string        `json:"arch"`
	SecretNames []string      `json:"secret_names,omitempty"` // names only
	Hosts       []string      `json:"hosts,omitempty"`
	Truncated   bool          `json:"truncated,omitempty"` // a limit stopped the walk early
}

const probeMaxFileSize = 512 << 10

// Limits on the walk. They are variables so a test can lower them.
var (
	probeMaxFiles    = 400   // parsed .go files
	probeMaxEntries  = 20000 // all directory entries visited
	probeMaxPackages = 200
	probeMaxExports  = 50
)

var probeSkipDirs = map[string]bool{".git": true, ".gophermind": true, "vendor": true, "node_modules": true, "testdata": true}

func marshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// probeFacts establishes the facts. A repo that does not exist yet is a fact,
// not an error.
func probeFacts(repo string, b *brief.Brief) (factsFile, error) {
	f := factsFile{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if b != nil {
		for _, s := range b.Front.Secrets {
			f.SecretNames = append(f.SecretNames, s.Name)
		}
		for _, n := range b.Front.Network {
			f.Hosts = append(f.Hosts, n.Host)
		}
		sort.Strings(f.SecretNames)
		sort.Strings(f.Hosts)
	}
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return f, nil
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		return f, nil
	}
	f.RepoExists = true
	if gi, err := os.Lstat(filepath.Join(real, ".git")); err == nil && gi.Mode()&fs.ModeSymlink == 0 {
		f.GitRepo = true
	}
	f.Module, f.GoVersion = readGoMod(filepath.Join(real, "go.mod"))

	byDir := map[string]map[string]bool{}
	files, entries := 0, 0
	_ = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable entry is skipped
		}
		if entries++; entries > probeMaxEntries {
			f.Truncated = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			if p != real && (probeSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if files >= probeMaxFiles {
			f.Truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > probeMaxFileSize {
			return nil
		}
		files++
		file, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(real, filepath.Dir(p))
		if err != nil {
			return nil
		}
		dir := filepath.ToSlash(rel)
		names := byDir[dir]
		if names == nil {
			names = map[string]bool{}
			byDir[dir] = names
		}
		for _, n := range exportedNames(file) {
			names[n] = true
		}
		return nil
	})
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	if len(dirs) > probeMaxPackages {
		f.Truncated = true
		dirs = dirs[:probeMaxPackages]
	}
	for _, d := range dirs {
		var ex []string
		for n := range byDir[d] {
			ex = append(ex, n)
		}
		sort.Strings(ex)
		if len(ex) > probeMaxExports {
			ex = ex[:probeMaxExports]
		}
		f.Packages = append(f.Packages, factPackage{Dir: d, Exports: ex})
	}
	return f, nil
}

// exportedNames lists the exported top-level names of a file and its exported
// methods on exported types (Type.Method).
func exportedNames(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !ast.IsExported(d.Name.Name) {
				continue
			}
			if d.Recv == nil || len(d.Recv.List) == 0 {
				out = append(out, d.Name.Name)
				continue
			}
			if recv := recvTypeName(d.Recv.List[0].Type); ast.IsExported(recv) {
				out = append(out, recv+"."+d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(sp.Name.Name) {
						out = append(out, sp.Name.Name)
					}
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						if ast.IsExported(n.Name) {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

func recvTypeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return recvTypeName(x.X)
	case *ast.IndexListExpr:
		return recvTypeName(x.X)
	}
	return ""
}

// readGoMod returns the module path and the go version of a go.mod, or empty
// strings. Trailing comments are dropped; the file is read up to 64 KB.
func readGoMod(path string) (module, goVersion string) {
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		return "", ""
	}
	fh, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	read := 0
	for sc.Scan() && read < 64<<10 {
		line := sc.Text()
		read += len(line) + 1
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" && module == "" {
			module = strings.Trim(fields[1], `"`)
		}
		if len(fields) == 2 && fields[0] == "go" && goVersion == "" {
			goVersion = fields[1]
		}
	}
	return module, goVersion
}

// factText answers a fact question from its key. ok is false when the facts
// cannot answer it.
func factText(f factsFile, key string) (string, bool) {
	switch key {
	case "go_module":
		return f.Module, f.Module != ""
	case "go_version":
		return f.GoVersion, f.GoVersion != ""
	case "repo_is_git":
		if !f.RepoExists {
			return "", false
		}
		if f.GitRepo {
			return "yes", true
		}
		return "no", true
	case "os":
		return f.OS, f.OS != ""
	case "arch":
		return f.Arch, f.Arch != ""
	case "packages":
		if len(f.Packages) == 0 {
			return "", false
		}
		dirs := make([]string, len(f.Packages))
		for i, p := range f.Packages {
			dirs[i] = p.Dir
		}
		return strings.Join(dirs, ", "), true
	case "secret_names":
		if len(f.SecretNames) == 0 {
			return "none declared", true
		}
		return strings.Join(f.SecretNames, ", "), true
	case "hosts":
		if len(f.Hosts) == 0 {
			return "none declared", true
		}
		return strings.Join(f.Hosts, ", "), true
	}
	return "", false
}

// factsPrompt renders the facts for a prompt and for UNDERSTANDING.md.
// It is deterministic.
func factsPrompt(f factsFile) string {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	var b strings.Builder
	b.WriteString("- target repository exists: " + yn(f.RepoExists) + "; is a git repository: " + yn(f.GitRepo) + "\n")
	if f.Module != "" {
		b.WriteString("- Go module: " + f.Module + "\n")
	}
	if f.GoVersion != "" {
		b.WriteString("- Go version in go.mod: " + f.GoVersion + "\n")
	}
	b.WriteString("- host operating system and architecture: " + f.OS + "/" + f.Arch + "\n")
	if len(f.SecretNames) > 0 {
		b.WriteString("- declared secrets (names only): " + strings.Join(f.SecretNames, ", ") + "\n")
	}
	if len(f.Hosts) > 0 {
		b.WriteString("- declared network hosts: " + strings.Join(f.Hosts, ", ") + "\n")
	}
	if len(f.Packages) > 0 {
		b.WriteString("- packages already in the repository and the names they export:\n")
		for _, p := range f.Packages {
			b.WriteString("  - " + p.Dir + ": " + strings.Join(p.Exports, ", ") + "\n")
		}
	}
	if f.Truncated {
		b.WriteString("- the repository scan was truncated by a size limit; the package list may be incomplete\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// answerFacts settles every open fact question the facts can answer and whose
// prerequisites are settled. It returns the ids it settled.
func (s *qstore) answerFacts(f factsFile, at time.Time) []string {
	settled := map[string]bool{}
	for _, q := range s.Questions {
		if q.Status == qSettled {
			settled[q.ID] = true
		}
	}
	var done []string
	for i := range s.Questions {
		q := &s.Questions[i]
		if q.Status != qOpen || q.Kind != "fact" {
			continue
		}
		ready := true
		for _, d := range q.DependsOn {
			if !settled[d] {
				ready = false
			}
		}
		if !ready {
			continue
		}
		if text, ok := factText(f, q.FactKey); ok {
			s.settle(q.ID, text, byProbe, at)
			settled[q.ID] = true
			done = append(done, q.ID)
		}
	}
	return done
}

func loadFacts(r *run) (factsFile, error) {
	var f factsFile
	_, err := readJSON(r.path(fileFacts), &f)
	return f, err
}

// ensureFacts reads facts.json, or probes and writes it. The probe runs once
// per run: a resume reads what the first run established.
func (p *Planner) ensureFacts(r *run) (factsFile, error) {
	var f factsFile
	found, err := readJSON(r.path(fileFacts), &f)
	if err != nil || found {
		return f, err
	}
	f, err = probeFacts(r.repo, r.brief)
	if err != nil {
		return f, err
	}
	return f, writeJSON(r.path(fileFacts), f)
}
