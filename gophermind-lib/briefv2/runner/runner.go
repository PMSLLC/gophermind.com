//go:build !windows

// Package runner runs the commands the executor needs (go build, vet, test,
// gofmt, acceptance shell commands) with an exact environment, its own process
// group, a timeout, a capped in-memory output buffer and, when a profile is
// set, under sandbox.Wrap.
//
// It builds on unix only because it kills process groups with syscall.Kill. The
// executor refuses to run off darwin unless the sandbox is off, and no Windows
// target exists, so there is no Windows stub.
//
// Output text is reachable only through Output.Text. Nothing here logs, formats
// or serializes it, and errors never quote it (decision E8).
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gophermind/gophermind-lib/briefv2/sandbox"
)

const (
	defaultOutputCap = 65536
	defaultGrace     = 2 * time.Second
)

// Output is captured combined stdout and stderr. Its text is reachable only
// through Text. It has no exported fields; String, GoString, Format and
// MarshalJSON expose sizes only, so an accidental %v, log line or json.Marshal
// cannot leak it.
type Output struct {
	text      []byte
	size      int
	truncated bool
	sum       [32]byte
}

func newOutput(text string, limit int) Output {
	b := []byte(text)
	o := Output{size: len(b), sum: sha256.Sum256(b)}
	if limit > 0 && len(b) > limit {
		b, o.truncated = b[:limit], true
	}
	o.text = b
	return o
}

// Text is the first OutputCap bytes the process wrote.
func (o Output) Text() string { return string(o.text) }

// Size is the total bytes the process wrote, including any cut off.
func (o Output) Size() int { return o.size }

// Truncated reports that the process wrote more than was kept.
func (o Output) Truncated() bool { return o.truncated }

// SHA256 is the hex hash of all bytes the process wrote, not only the kept ones.
func (o Output) SHA256() string { return hex.EncodeToString(o.sum[:]) }

// Lines returns the first n lines of Text with "\r" trimmed.
func (o Output) Lines(n int) []string {
	t := strings.TrimSuffix(string(o.text), "\n")
	if t == "" || n <= 0 {
		return nil
	}
	var out []string
	for _, l := range strings.Split(t, "\n") {
		if len(out) >= n {
			break
		}
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}

func (o Output) String() string {
	if o.truncated {
		return fmt.Sprintf("runner.Output(%d bytes, truncated)", o.size)
	}
	return fmt.Sprintf("runner.Output(%d bytes)", o.size)
}

// GoString is the %#v form.
func (o Output) GoString() string { return o.String() }

// Format implements fmt.Formatter so every verb prints the size form.
func (o Output) Format(f fmt.State, _ rune) { _, _ = fmt.Fprint(f, o.String()) }

// MarshalJSON emits size, truncation and hash only.
func (o Output) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Size      int    `json:"size"`
		Truncated bool   `json:"truncated"`
		SHA256    string `json:"sha256"`
	}{o.size, o.truncated, o.SHA256()})
}

// cappedWriter counts and hashes every byte and keeps the first limit bytes. One
// mutex makes it safe for the two copiers os/exec starts when Stdout == Stderr.
type cappedWriter struct {
	mu    sync.Mutex
	buf   []byte
	limit int
	size  int
	h     hash.Hash
}

func newCappedWriter(limit int) *cappedWriter {
	return &cappedWriter{limit: limit, h: sha256.New()}
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size += len(p)
	w.h.Write(p)
	if room := w.limit - len(w.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
	}
	return len(p), nil
}

func (w *cappedWriter) output() Output {
	w.mu.Lock()
	defer w.mu.Unlock()
	o := Output{text: append([]byte(nil), w.buf...), size: w.size, truncated: w.size > len(w.buf)}
	copy(o.sum[:], w.h.Sum(nil))
	return o
}

// Config is set once per Runner.
type Config struct {
	Sandbox   *sandbox.Profile // nil means run unsandboxed (settings say off)
	OutputCap int              // bytes kept; <=0 means 65536
	Grace     time.Duration    // cmd.WaitDelay after the leader exits; <=0 means 2s
}

// Runner runs commands. It holds no per-call state.
type Runner struct{ cfg Config }

// New applies the defaults. It does not locate binaries.
func New(c Config) *Runner {
	if c.OutputCap <= 0 {
		c.OutputCap = defaultOutputCap
	}
	if c.Grace <= 0 {
		c.Grace = defaultGrace
	}
	return &Runner{cfg: c}
}

// Spec describes one command.
type Spec struct {
	Dir     string        // working directory
	Argv    []string      // program and arguments, run directly, never through a shell; Argv[0] must be absolute
	Env     []string      // exactly this environment; nothing is inherited
	Timeout time.Duration // 0 means bounded only by ctx
	// ModCacheWritable wraps this one command with a copy of Config.Sandbox whose
	// ModCacheWritable is true (the deps step, spec 14, decision E15). Ignored when
	// Config.Sandbox is nil.
	ModCacheWritable bool
}

// Shell returns the argv for a shell command line.
func Shell(cmd string) []string { return []string{"/bin/sh", "-c", cmd} }

// Result is the outcome of one Run.
type Result struct {
	ExitCode int // -1 when killed by a signal
	Duration time.Duration
	TimedOut bool // Spec.Timeout elapsed (not ctx cancellation)
	Canceled bool // ctx was cancelled
	Out      Output
	Err      error // start failure only; never wraps output
}

func startErr(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("runner: program or working directory not found")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("runner: permission denied starting the program")
	}
	return errors.New("runner: could not start the program")
}

// Run runs s. The whole process group is killed on timeout or cancellation, and
// once more after every run so a command's backgrounded leftovers cannot outlive it.
func (r *Runner) Run(ctx context.Context, s Spec) Result {
	if len(s.Argv) == 0 || !filepath.IsAbs(s.Argv[0]) {
		return Result{ExitCode: -1, Err: errors.New("runner: Argv[0] must be an absolute path")}
	}
	w := newCappedWriter(r.cfg.OutputCap)
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = append([]string{}, s.Env...) // non-nil: an empty Env means an empty environment
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = r.cfg.Grace
	cmd.Stdout, cmd.Stderr = w, w
	if r.cfg.Sandbox != nil {
		p := *r.cfg.Sandbox
		if s.ModCacheWritable {
			p.ModCacheWritable = true
		}
		wrapped, err := sandbox.Wrap(cmd, p)
		if err != nil {
			return Result{ExitCode: -1, Err: err}
		}
		cmd = wrapped
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Err: startErr(err)}
	}
	pgid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timeout <-chan time.Time
	if s.Timeout > 0 {
		t := time.NewTimer(s.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	var res Result
	select {
	case <-done:
	case <-timeout:
		res.TimedOut = true
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
	case <-ctx.Done():
		res.Canceled = true
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL) // leftovers: always, even after a clean exit (ESRCH is fine)
	res.Duration = time.Since(start)
	res.ExitCode = -1
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	res.Out = w.output()
	return res
}
