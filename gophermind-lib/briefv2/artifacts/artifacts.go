// Package artifacts saves what a model attempt produced, so a bad run can be
// inspected afterwards: the reply it gave and the compiler or test output that
// judged it. Files go under <run>/attempts/<node-id>/<n>/ and are scrubbed of
// every declared secret value before they touch disk. A prompt is never saved:
// it carries the brief slice and the dependency signatures and adds nothing a
// reply does not show. Saving is switched off by settings (executor.artifacts).
package artifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// Meta describes one attempt. It holds no reply text.
type Meta struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Revision    int       `json:"revision"`
	Order       int       `json:"order"`
	Verdict     string    `json:"verdict"`
	Class       string    `json:"class"`
	Reason      string    `json:"reason,omitempty"`
	ReplySHA256 string    `json:"reply_sha256,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	DurationMS  int64     `json:"duration_ms"`
}

// Files is what to save. An empty field creates no file.
type Files struct {
	Source      []byte // the formatted file the reply carried: reply.go
	Raw         string // the reply text as the model wrote it: reply.txt
	CheckOutput string // compiler, vet or test output: check-output.txt
}

// Writer saves attempts for one run. The zero value and nil save nothing.
type Writer struct {
	root     string
	on       bool
	maxBytes int
	scrub    func(string) string
}

// New returns a writer for the run folder runDir. scrub maps a text to itself
// or, when it holds a declared secret value, to a redaction mark; nil means no
// secrets are declared. maxBytes caps each saved file.
func New(runDir string, on bool, maxBytes int, scrub func(string) string) *Writer {
	return &Writer{root: filepath.Join(runDir, "attempts"), on: on, maxBytes: maxBytes, scrub: scrub}
}

var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Save writes one attempt of nodeID and returns its folder.
func (w *Writer) Save(nodeID string, m Meta, f Files) (string, error) {
	if w == nil || !w.on {
		return "", nil
	}
	if !nodeIDRE.MatchString(nodeID) {
		return "", errors.New("artifacts: not a node id")
	}
	nodeDir := filepath.Join(w.root, nodeID)
	n, err := nextNumber(nodeDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(nodeDir, strconv.Itoa(n))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for name, text := range map[string]string{"reply.go": string(f.Source), "reply.txt": f.Raw, "check-output.txt": f.CheckOutput} {
		if text == "" {
			continue
		}
		if err := runfs.WriteFileAtomic(filepath.Join(dir, name), []byte(w.prepare(text))); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, "attempt.json"), append(b, '\n')); err != nil {
		return "", err
	}
	return dir, nil
}

// prepare scrubs the whole text first (a secret may straddle the cut), then
// caps it.
func (w *Writer) prepare(s string) string {
	if w.scrub != nil {
		s = w.scrub(s)
	}
	if w.maxBytes <= 0 || len(s) <= w.maxBytes {
		return s
	}
	cut := w.maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[truncated: %d bytes cut]\n", len(s)-cut)
}

// nextNumber is one more than the highest numbered attempt folder of the node.
func nextNumber(nodeDir string) (int, error) {
	ents, err := os.ReadDir(nodeDir)
	if errors.Is(err, os.ErrNotExist) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range ents {
		if n, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() && n > max {
			max = n
		}
	}
	return max + 1, nil
}
