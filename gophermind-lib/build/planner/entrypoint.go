package planner

import (
	"path"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/build/brief"
)

// A brief that describes something to be run (a command-line tool or a server)
// needs a main package under cmd/<binary-name>: acceptance tests call the
// binary the harness builds from it, so without one no behavior can be proven.

var (
	codeSpanRE = regexp.MustCompile("`([^`\n]+)`")
	cmdWordRE  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// runKeywordRE are phrases of the overview and architecture that say the
	// product is run rather than imported.
	runKeywordRE = regexp.MustCompile(`(?i)command[- ]line|\bcli\b|\bhttp server\b|\bweb server\b|\bdaemon\b|\blistens on\b|\blistening on\b`)
)

// notBinaries are command words of a code span that are tools the checks use,
// not the product.
var notBinaries = map[string]bool{
	"go": true, "gofmt": true, "goimports": true, "test": true, "grep": true, "egrep": true, "sed": true,
	"awk": true, "cat": true, "echo": true, "ls": true, "find": true, "git": true, "make": true, "sh": true,
	"bash": true, "curl": true, "wget": true, "diff": true, "cmp": true, "jq": true, "python": true,
	"python3": true, "node": true, "npm": true, "docker": true, "true": true, "false": true, "mkdir": true,
	"rm": true, "cp": true, "mv": true, "head": true, "tail": true, "sort": true, "wc": true, "printf": true,
	"env": true, "cd": true, "staticcheck": true, "golangci-lint": true, "vet": true,
}

var nonKebabRE = regexp.MustCompile(`[^a-z0-9]+`)

func kebab(s string) string {
	return strings.Trim(nonKebabRE.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// RunnableBinary says whether the brief describes something to be run, and the
// name of its binary. The name is the command an acceptance criterion or
// feature runs (a code span such as `csvstat data.csv`: the command is the
// brief's title, or is followed by a flag or a file name), else the title in
// kebab case, else the brief id. It is a deliberate guess in one direction: a
// library brief that names no command and uses none of the phrases of
// overview (command-line, CLI, HTTP server, daemon, listens on) is not runnable.
func RunnableBinary(reqs []Requirement, f brief.Frontmatter, overview string) (string, bool) {
	fallback := kebab(f.Title)
	if fallback == "" {
		fallback = f.ID
	}
	for _, q := range reqs {
		if q.Kind == ReqConstraint {
			continue
		}
		for _, m := range codeSpanRE.FindAllStringSubmatch(q.Text, -1) {
			words := strings.Fields(m[1])
			if len(words) == 0 || !cmdWordRE.MatchString(words[0]) || notBinaries[words[0]] {
				continue
			}
			if words[0] == kebab(f.Title) || hasPathOrFlagArg(words[1:]) {
				return words[0], true
			}
		}
	}
	if runKeywordRE.MatchString(overview) {
		return fallback, true
	}
	return "", false
}

// hasPathOrFlagArg is true for an argument that looks like a flag or a file.
func hasPathOrFlagArg(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") || strings.ContainsAny(a, "./") {
			return true
		}
	}
	return false
}

// hasEntryPoint is true when a component of the contract document has a
// package under cmd/.
func hasEntryPoint(doc map[string]any) bool {
	for _, c := range objects(doc["components"]) {
		pkg, _ := c["package"].(string)
		if rest, ok := strings.CutPrefix(path.Clean(pkg), "cmd/"); ok && rest != "" {
			return true
		}
	}
	return false
}
