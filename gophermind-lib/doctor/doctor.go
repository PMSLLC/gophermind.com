// Package doctor runs quick environment/config diagnostics for gophermind so a
// user can spot setup problems (unreachable endpoint, missing rg/git) fast.
package doctor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result is one diagnostic check outcome.
type Result struct {
	Name   string
	OK     bool
	Detail string
}

// Params carries the inputs the checks need. HTTPGet and lookPath are injectable
// for testing; when nil, real implementations are used.
type Params struct {
	BaseURL string
	Model   string
	Root    string

	HTTPGet  func(url string) (int, error)     // returns HTTP status code
	lookPath func(name string) (string, error) // resolves a binary on PATH
	GitInit  func(dir string) error            // runs `git init`; injectable for --fix
}

// Checks runs all diagnostics and returns their results in a stable order.
func Checks(p Params) []Result {
	if p.HTTPGet == nil {
		p.HTTPGet = defaultHTTPGet
	}
	if p.lookPath == nil {
		p.lookPath = exec.LookPath
	}

	var out []Result

	// Endpoint configured.
	if p.BaseURL == "" {
		out = append(out, Result{"endpoint configured", false, "no BaseURL set (run `gophermind config` or set GOPHERMIND_BASE_URL)"})
	} else {
		out = append(out, Result{"endpoint configured", true, p.BaseURL})
	}

	// Endpoint reachable (only meaningful if configured).
	if p.BaseURL == "" {
		out = append(out, Result{"endpoint reachable", false, "skipped: no endpoint configured"})
	} else {
		url := strings.TrimRight(p.BaseURL, "/") + "/models"
		code, err := p.HTTPGet(url)
		switch {
		case err != nil:
			out = append(out, Result{"endpoint reachable", false, fmt.Sprintf("%s: %v", url, err)})
		case code >= 500:
			out = append(out, Result{"endpoint reachable", false, fmt.Sprintf("%s returned %d", url, code)})
		default:
			out = append(out, Result{"endpoint reachable", true, fmt.Sprintf("%s → %d", url, code)})
		}
	}

	// Model.
	if p.Model == "" {
		out = append(out, Result{"model", true, "auto-discover from endpoint"})
	} else {
		out = append(out, Result{"model", true, "set: " + p.Model})
	}

	// External binaries.
	out = append(out, binaryCheck(p.lookPath, "ripgrep (rg)", "rg"))
	out = append(out, binaryCheck(p.lookPath, "git", "git"))

	// Repository root is a git repo.
	out = append(out, gitRepoCheck(p.Root))

	return out
}

func binaryCheck(lookPath func(string) (string, error), label, bin string) Result {
	path, err := lookPath(bin)
	if err != nil || path == "" {
		return Result{label, false, "not found on PATH"}
	}
	return Result{label, true, path}
}

// gitRepoCheck walks up from root looking for a .git directory.
func gitRepoCheck(root string) Result {
	if root == "" {
		root = "."
	}
	dir, err := filepath.Abs(root)
	if err != nil {
		return Result{"git repository", false, err.Error()}
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return Result{"git repository", true, dir}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Result{"git repository", false, "not inside a git repository"}
		}
		dir = parent
	}
}

func defaultHTTPGet(url string) (int, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Report writes a human-readable summary and returns true iff every check
// passed.
func Report(w io.Writer, results []Result) bool {
	allOK := true
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
			allOK = false
		}
		fmt.Fprintf(w, "%s %-20s %s\n", mark, r.Name, r.Detail)
	}
	if allOK {
		fmt.Fprintln(w, "\nAll checks passed.")
	} else {
		fmt.Fprintln(w, "\nSome checks failed — see above.")
	}
	return allOK
}
