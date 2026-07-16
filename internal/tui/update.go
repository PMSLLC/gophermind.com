package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/jbrahy/bubblecomplete"
	"gophermind/internal/agent"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 2)
		justReady := !m.ready
		if !m.ready {
			// Height is provisional here; applyInputHeight (below) recomputes
			// it from the input's row count now that m.ready is true.
			m.viewport = viewport.New(msg.Width, 1)
			m.ready = true
			m.appendLine(m.banner)
		} else {
			m.viewport.Width = msg.Width
		}
		applyInputHeight(&m)
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(m.glamourStyle), glamour.WithWordWrap(msg.Width-2)); err == nil {
			m.render = r
		}
		m.sync()
		if justReady {
			// The banner is taller than the viewport; anchor the first paint
			// to the top so the gopher's face is what greets the user.
			m.viewport.GotoTop()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tokenMsg:
		m.stream += string(msg)
		m.sync()
		return m, waitFor(m.sub)

	case usageMsg:
		m.usage = agent.UsageSnapshot(msg)
		return m, waitFor(m.sub)

	case assistantMsg:
		// Intermediate narration accompanying a tool call. It already streamed
		// live into m.stream; commit it as a styled line and clear the buffer so
		// the final answer render doesn't repeat it.
		m.appendLine(renderNarration(string(msg)))
		m.stream = ""
		m.sync()
		return m, waitFor(m.sub)

	case toolCallMsg:
		m.appendLine(renderToolCall(msg.name, msg.args))
		m.sync()
		return m, waitFor(m.sub)

	case toolResultMsg:
		m.appendLine(renderToolResult(msg.text))
		m.sync()
		return m, waitFor(m.sub)

	case approvalMsg:
		m.st = stateApproval
		m.pending = msg
		return m, waitFor(m.sub)

	case configDoneMsg:
		// The /config wizard finished (via tea.Exec); persist and apply it.
		m.handleConfigDone(msg)
		return m, waitFor(m.sub)

	case doneMsg:
		if s := strings.TrimSpace(m.stream); s != "" {
			out := s
			if m.render != nil {
				if rendered, err := m.render.Render(s); err == nil {
					out = strings.TrimRight(rendered, "\n")
				}
			}
			m.appendLine(out)
		}
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		m.sync()
		// A /project turn is post-processed by its state machine (advance the
		// interview, validate the plan, or move to review).
		if m.projTurn {
			m.projTurn = false
			return m.afterProjectTurn(msg.answer)
		}
		return m, waitFor(m.sub)

	case errMsg:
		// A cancelled turn (Ctrl-C / Esc mid-stream) is a user action, not a
		// fault: show a brief "cancelled" line and drop the partial stream rather
		// than surfacing a raw "context canceled" error.
		if errors.Is(msg.err, context.Canceled) {
			m.appendLine(renderError("⨯ cancelled"))
		} else {
			m.appendLine(renderError("error: " + msg.err.Error()))
		}
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		m.sync()
		return m, waitFor(m.sub)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		// Mid-stream Ctrl-C cancels the in-flight turn (snappy abort) and returns
		// to the prompt; the cancelled stream surfaces as a "cancelled" line via
		// the errMsg path. Ctrl-C while idle (no active request) quits as usual.
		if m.st == stateWorking && m.cancel != nil {
			m.cancel()
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyCtrlD:
		return m, tea.Quit

	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyEsc:
		// An active suggestion (ghost or menu) eats the first Esc — dismiss it
		// instead of the cancel/interrupt behavior below. A second Esc (or Esc
		// with nothing active) falls through to that behavior as before.
		if m.st == stateIdle && m.complete.Active() {
			cm, res := m.complete.Update(msg)
			m.complete = cm
			if res.Consumed {
				return m, nil
			}
		}
		if m.st == stateApproval {
			m.pending.reply <- false
			m.st = stateWorking
		}
		if m.cancel != nil {
			m.cancel()
		}
		return m, nil
	}

	// While awaiting a decision, y/n/a take priority over text entry.
	if m.st == stateApproval {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.pending.reply <- true
			m.st = stateWorking
		case "n":
			m.pending.reply <- false
			m.st = stateWorking
		case "a":
			m.allowed.add(m.pending.tool)
			m.pending.reply <- true
			m.st = stateWorking
		}
		return m, nil
	}

	// Predictive-text gets first refusal on keys while idle (Tab/→ accept a
	// suggestion, ↑/↓ navigate an open menu, ...). Suggestions are never
	// queried/shown outside stateIdle (see the final Query call below and
	// handleSubmit), so this is inert elsewhere.
	if m.st == stateIdle {
		cm, res := m.complete.Update(msg)
		m.complete = cm
		if res.Accepted != nil {
			m.applyCandidate(*res.Accepted)
			m.queryComplete()
			applyInputHeight(&m)
			return m, nil
		}
		if res.Consumed {
			return m, nil
		}
	}

	// Shift+Enter is indistinguishable from plain Enter on most terminals, so
	// Alt+Enter and Ctrl+J are the reliable ways to insert a literal newline;
	// msg.String() == "shift+enter" covers terminals that do report it
	// distinctly (bubbletea's own key parser has no such string as of
	// v1.3.10, so this arm is a forward-compatible no-op today).
	isNewlineKey := (msg.Type == tea.KeyEnter && msg.Alt) || msg.Type == tea.KeyCtrlJ || msg.String() == "shift+enter"
	if isNewlineKey && m.st == stateIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyEnter})
		applyInputHeight(&m)
		m.queryComplete()
		return m, cmd
	}

	if msg.Type == tea.KeyEnter && !msg.Alt && m.st == stateIdle {
		if m.complete.Mode() == bubblecomplete.ModeMenu {
			cm, cand := m.complete.Accept()
			m.complete = cm
			if cand != nil {
				m.applyCandidate(*cand)
			}
			m.queryComplete()
			applyInputHeight(&m)
			return m, nil
		}
		mm, cmd := m.handleSubmit()
		return mm, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	applyInputHeight(&m)
	if m.st == stateIdle {
		m.queryComplete()
	}
	return m, cmd
}

func (m model) handleSubmit() (model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	applyInputHeight(&m)
	// Drop any suggestion left over from the submitted text — the input is
	// now empty, so a stale ghost/menu must not linger over it.
	m.queryComplete()
	if text == "" {
		return m, nil
	}

	// While a guided /project flow is active, its state machine consumes input.
	if m.proj != projNone {
		if nm, cmd, handled := m.handleProjectInput(text); handled {
			return nm, cmd
		}
	}

	// Commands taking an argument are matched by their first field so the value
	// can follow (e.g. "/temp 0.7").
	if cmd := strings.Fields(text)[0]; cmd == "/temp" || cmd == "/topp" {
		m.handleSamplingCommand(cmd, text)
		m.sync()
		return m, nil
	}

	// "/goal ..." sets, shows, or clears the session-scoped steering goal that
	// is injected into every subsequent normal prompt (see the injection
	// below). It always prints to the transcript and returns; there is no
	// agent turn.
	if strings.Fields(text)[0] == "/goal" {
		m.handleGoalCommand(text)
		m.sync()
		return m, nil
	}

	switch text {
	case "/exit", "/quit":
		return m, tea.Quit
	case "/clear":
		m.content = ""
		m.stream = ""
		m.st = stateIdle
		if m.agent != nil {
			m.agent.Reset()
		}
		m.sync()
		return m, nil
	case "/help":
		m.appendLine(helpLine())
		m.sync()
		return m, nil
	}

	// "/phase ..." runs the PhaseFlow workflow. State commands print to the
	// transcript and return; a loop step yields a seeded prompt sent to the agent
	// (the /phase line stays as the shown user prompt).
	sendText := text
	if strings.Fields(text)[0] == "/phase" {
		reply, agentTask := m.handlePhaseCommand(text)
		if agentTask == "" {
			m.appendLine(reply)
			m.sync()
			return m, nil
		}
		sendText = agentTask
	}

	// "/config" launches the interactive configuration wizard, saves the result,
	// and reports back. It reuses the same wizard that runs on first launch.
	if strings.Fields(text)[0] == "/config" {
		return m.handleConfigCommand()
	}

	// "/project [name]" starts the guided new-project flow (interview → plan →
	// approve). Subsequent input is consumed by the block above until it ends.
	if strings.Fields(text)[0] == "/project" {
		return m.handleProjectCommand(text)
	}

	// A session goal (set via "/goal") is injected into every ordinary prompt
	// as a steering preamble, so it reaches the model every turn regardless of
	// backend. The transcript still shows the raw text; only sendText carries
	// the preamble. Slash-command-derived sends (e.g. a "/phase" loop step)
	// are excluded by the leading "/" check on text.
	if m.goal != "" && !strings.HasPrefix(text, "/") {
		sendText = goalPreamble(m.goal, sendText)
	}

	m.appendLine("")
	m.appendLine(renderUserPrompt(text))
	m.st = stateWorking
	m.sync()

	// Record real prompts (not slash commands, e.g. "/phase ..." whose seeded
	// agent task still starts with "/") in history so recall and markov
	// completion see them on the next Query.
	if !strings.HasPrefix(text, "/") {
		if m.hist != nil {
			m.hist.Append(text)
		}
		if m.ngram != nil {
			m.ngram.Train(text)
		}
	}

	if m.agent == nil {
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	ag := m.agent
	go func() {
		ans, err := ag.Send(ctx, sendText)
		if err != nil {
			sub <- errMsg{err: err}
		} else {
			sub <- doneMsg{answer: ans}
		}
	}()
	return m, nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
