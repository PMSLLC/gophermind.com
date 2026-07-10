package doctor

import (
	"fmt"
	"os/exec"
)

// FixResult reports the outcome of attempting to remediate one failing check.
type FixResult struct {
	Name   string
	Fixed  bool
	Detail string
}

// fixHints maps a failing check to advice when it cannot be auto-remediated.
var fixHints = map[string]string{
	"endpoint configured": "run `gophermind config` or set GOPHERMIND_BASE_URL",
	"endpoint reachable":  "check the endpoint URL, network, and that the server is up",
	"model":               "set -model or GOPHERMIND_MODEL",
	"ripgrep (rg)":        "install ripgrep (e.g. `brew install ripgrep` / `apt install ripgrep`)",
	"git":                 "install git",
}

// AutoFix attempts to remediate the fixable failing checks in results and
// returns an outcome per failing check. The only safely auto-fixable case is a
// missing git repository (via git init); everything else is reported with a
// hint. GitInit is injectable for testing.
func AutoFix(p Params, results []Result) []FixResult {
	gitInit := p.GitInit
	if gitInit == nil {
		gitInit = defaultGitInit
	}

	var out []FixResult
	for _, r := range results {
		if r.OK {
			continue
		}
		switch r.Name {
		case "git repository":
			root := p.Root
			if root == "" {
				root = "."
			}
			if err := gitInit(root); err != nil {
				out = append(out, FixResult{r.Name, false, "git init failed: " + err.Error()})
			} else {
				out = append(out, FixResult{r.Name, true, "ran git init in " + root})
			}
		default:
			hint := fixHints[r.Name]
			if hint == "" {
				hint = "no automatic fix available"
			}
			out = append(out, FixResult{r.Name, false, hint})
		}
	}
	return out
}

// defaultGitInit runs `git init` in dir.
func defaultGitInit(dir string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}
