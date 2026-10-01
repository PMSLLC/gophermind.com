package planner

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

// DebugDumpEnv names a directory that receives the raw prompt and the raw reply
// of every planner model call. It is a developer tool for finding out what a
// model really sent. It is never set by an orchestrator, never reaches a store,
// an event or a report, and is ignored when the directory is inside the target
// repository, the run folder or the config folder.
const DebugDumpEnv = "GOPHERMIND_DEBUG_DUMP_DIR"

// debugOut is where the notices about the dump go.
var debugOut io.Writer = os.Stderr

type dumpState struct {
	once sync.Once
	dir  string // empty when off or refused

	mu  sync.Mutex
	seq map[string]int
}

// dumpDir resolves the dump directory once per Planner: "" when the variable is
// unset, names no existing directory, or points somewhere it must not.
func (p *Planner) dumpDir(r *run) string {
	p.dump.once.Do(func() {
		dir := strings.TrimSpace(os.Getenv(DebugDumpEnv))
		if dir == "" {
			return
		}
		real, err := realDir(dir)
		if err != nil {
			fmt.Fprintf(debugOut, "debug dump ignored: %s is not an existing directory\n", DebugDumpEnv)
			return
		}
		forbidden := []string{r.dir, r.repo}
		if cp, err := settings.Path(); err == nil {
			forbidden = append(forbidden, filepath.Dir(cp))
		}
		for _, f := range forbidden {
			if f == "" {
				continue
			}
			if rf, err := realDir(f); err == nil && within(real, rf) {
				fmt.Fprintf(debugOut, "debug dump ignored: %s is inside the repository, the run folder or the config folder\n", DebugDumpEnv)
				return
			}
		}
		fmt.Fprintln(debugOut, "debug dump enabled")
		p.dump.dir = real
		p.dump.seq = map[string]int{}
	})
	return p.dump.dir
}

func realDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return real, nil
}

// within reports whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var dumpNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// dumpReply writes the request and the reply of one parse attempt of a stage.
// It never fails the call: a write error is told on the notice stream only.
func (p *Planner) dumpReply(r *run, stage string, req provider.Request, text string) {
	dir := p.dumpDir(r)
	if dir == "" {
		return
	}
	p.dump.mu.Lock()
	p.dump.seq[stage]++
	n := p.dump.seq[stage]
	p.dump.mu.Unlock()
	base := filepath.Join(dir, fmt.Sprintf("%s-%d", dumpNameRE.ReplaceAllString(stage, "_"), n))
	reqJSON, _ := json.MarshalIndent(map[string]any{"stage": stage, "max_tokens": req.MaxTokens, "messages": req.Messages}, "", "  ")
	for name, body := range map[string][]byte{base + ".request.json": reqJSON, base + ".reply.txt": []byte(text)} {
		if err := os.WriteFile(name, body, 0o600); err != nil {
			fmt.Fprintf(debugOut, "debug dump: %v\n", err)
		}
	}
}
