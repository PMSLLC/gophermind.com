package resume

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// discardRoot is where moved files go; the walk never enters it.
const discardRoot = "_state/discarded"

// stageTable is the run-dir-relative files and directories each planner stage
// writes. The first entry is the stage's marker: it exists once the stage has
// begun to leave output. A stage that reads a file an earlier stage wrote does
// not list it.
var stageTable = map[string][]string{
	"clarify": {
		"_state/clarify/questions.json", "_state/clarify/facts.json", "_state/clarify/rounds.jsonl",
		"_state/clarify.json", "_state/question.json", "answers.json", "QUESTIONS.md", "decisions",
	},
	"confirm": {"_state/understanding.json", "UNDERSTANDING.md"},
	// dependencies.json is written by the contract stage (contract_stage.go), not decompose.
	"contract":  {"contracts.json", "_state/contract.json", "_state/sources.json", "dependencies.json"},
	"decompose": {"_state/decomposed.json", "_state/classes.json"},
	"enrich":    {"_state/enriched.json"},
	"coverage":  {"coverage.json"},
	"approve":   {"approval.json", "APPROVAL.md"},
	"testwriter": {
		"_state/testwriter.json", "_state/test_files.json", "_state/leaf_tests.json",
		"_state/acceptance_tests.json", "_state/obligations.json",
	},
}

// stageFiles returns the static files and directories of a stage; nil for an unknown stage.
func stageFiles(stage string) []string { return append([]string(nil), stageTable[stage]...) }

// executorFiles are the executor's and project run's files at the run root. Runtime
// rows (*.runtime.json and their .lock files) and ESCALATION-*.md are found by looking.
var executorFiles = []string{
	"_state/executor.json", "_state/leaf-results.json", "_state/notes.json", "_state/escalations.json",
	"_state/revisions", "report.json", "_state/project.json",
}

// nodePlanPaths finds the node plan files decompose writes: root.json and each
// top-level folder that holds a component.json (leaf nodes live in that folder).
func nodePlanPaths(runDir string) ([]string, error) {
	var out []string
	if _, err := os.Lstat(filepath.Join(runDir, "root.json")); err == nil {
		out = append(out, "root.json")
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_state" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(runDir, e.Name(), "component.json")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// runtimePaths finds every node runtime row and its lock, and every escalation file.
func runtimePaths(runDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(runDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(runDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == discardRoot {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".runtime.json"), strings.HasSuffix(name, ".runtime.json.lock"):
			out = append(out, rel)
		case strings.HasPrefix(name, "ESCALATION-") && strings.HasSuffix(name, ".md") && !strings.Contains(rel, "/"):
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// discardDir is the folder, relative to the run dir, this resume moves files into.
func discardDir(runDir string, now time.Time) string {
	return discardRoot + "/" + now.UTC().Format("2006-01-02T15-04-05Z")
}

// inside joins rel under root and reports an error when the result leaves root.
func inside(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("resume: %q is not a path inside the run folder", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resume: %q leaves the run folder", rel)
	}
	return full, nil
}

// moveAside renames runDir/rel to runDir/dest/rel, creating parents. It reports
// false, with no error, when rel does not exist. Nothing is deleted or overwritten.
func moveAside(runDir, rel, dest string) (bool, error) {
	src, err := inside(runDir, rel)
	if err != nil {
		return false, err
	}
	dst, err := inside(runDir, dest+"/"+rel)
	if err != nil {
		return false, err
	}
	if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := os.Lstat(dst); err == nil {
		return false, fmt.Errorf("resume: %s already exists; not overwriting", filepath.ToSlash(filepath.Join(dest, rel)))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	if err := os.Rename(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

// moveAll moves rels, ancestors first so a folder goes whole, and returns those that existed, sorted.
func moveAll(runDir, dest string, rels []string) ([]string, error) {
	sort.Slice(rels, func(i, j int) bool {
		if len(rels[i]) != len(rels[j]) {
			return len(rels[i]) < len(rels[j])
		}
		return rels[i] < rels[j]
	})
	var moved []string
	for _, rel := range rels {
		ok, err := moveAside(runDir, rel, dest)
		if err != nil {
			return moved, err
		}
		if ok {
			moved = append(moved, rel)
		}
	}
	sort.Strings(moved)
	return moved, nil
}
