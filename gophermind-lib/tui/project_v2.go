package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/briefv2/projectrun"
)

// This file implements the slash form of "/project <brief> [flags]": the same
// projectrun.Run the CLI uses, run on a goroutine so the event loop never
// blocks. The TUI never prompts: Run is always unattended here (ParseArgs
// refuses --attended and In is nil), so every question is answered by the
// planner's own unattended rule and nothing is dropped.

// projectRunFn and projectEnvFn are package variables so tests replace them.
var projectRunFn = projectrun.Run
var projectEnvFn = projectrun.DefaultEnv

// projectV2LineMax bounds one progress line shown in the transcript.
const projectV2LineMax = 300

// projectV2LineMsg is one printed line of the run.
type projectV2LineMsg string

// projectV2DoneMsg is the run's end. interrupted is set when the context was
// cancelled (Esc or Ctrl-C) before Run returned.
type projectV2DoneMsg struct {
	res         projectrun.Result
	interrupted bool
}

// projectV2Writer turns Run's output into one message per line. It buffers a
// partial line, one-lines and bounds each line, and is safe for the two
// writers (Out and Err) Run may use concurrently.
type projectV2Writer struct {
	mu   sync.Mutex
	buf  []byte
	post func(projectV2LineMsg)
}

func (w *projectV2Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// flush emits whatever partial line is left.
func (w *projectV2Writer) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

func (w *projectV2Writer) emit(line string) {
	line = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, line)
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if r := []rune(line); len(r) > projectV2LineMax {
		line = string(r[:projectV2LineMax]) + "..."
	}
	w.post(projectV2LineMsg(line))
}

// projectV2DoneLine is the transcript line for a finished run.
func projectV2DoneLine(msg projectV2DoneMsg) string {
	status := msg.res.Status
	if status == "" {
		status = "unknown"
	}
	line := fmt.Sprintf("project: exit %d (%s)", msg.res.ExitCode, status)
	if msg.interrupted || msg.res.ExitCode == projectrun.ExitInterrupted {
		line += "; interrupted; continue with /project <brief> --resume"
	}
	return line
}

// handleProjectV2Command dispatches "/project <brief> [flags]".
func (m model) handleProjectV2Command(text string) (model, tea.Cmd) {
	if m.projV2 {
		m.appendLine("a project run is already working; wait for it or press Esc")
		m.sync()
		return m, nil
	}
	o, err := projectrun.ParseArgs(strings.Fields(text)[1:], false)
	if err != nil {
		m.appendLine("project: " + err.Error())
		m.sync()
		return m, nil
	}
	m.projV2 = true
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	w := &projectV2Writer{post: func(l projectV2LineMsg) { sub <- l }}
	o.Out, o.Err, o.In = w, w, nil
	env := projectEnvFn()
	run := projectRunFn
	go func() {
		res := run(ctx, o, env)
		w.flush()
		sub <- projectV2DoneMsg{res: res, interrupted: ctx.Err() != nil}
	}()
	m.sync()
	return m, nil
}
