package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpH := msg.Height - inputHeight - statusHeight
		if vpH < 1 {
			vpH = 1
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpH)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpH
		}
		m.input.SetWidth(msg.Width - 2)
		if r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(msg.Width-2)); err == nil {
			m.render = r
		}
		m.sync()
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tokenMsg:
		m.stream += string(msg)
		m.tokens++
		m.sync()
		return m, waitFor(m.sub)

	case assistantMsg:
		m.appendLine(string(msg))
		m.sync()
		return m, waitFor(m.sub)

	case toolCallMsg:
		m.appendLine("● " + msg.name + "  " + oneLine(msg.args))
		m.sync()
		return m, waitFor(m.sub)

	case toolResultMsg:
		m.appendLine("  " + oneLine(msg.text))
		m.sync()
		return m, waitFor(m.sub)

	case approvalMsg:
		m.st = stateApproval
		m.pending = msg
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
		return m, waitFor(m.sub)

	case errMsg:
		m.appendLine("error: " + msg.err.Error())
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
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return m, tea.Quit

	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyEsc:
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

	if msg.Type == tea.KeyEnter && m.st == stateIdle {
		mm, cmd := m.handleSubmit()
		return mm, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleSubmit() (model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if text == "" {
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
		m.appendLine("Commands: /help  /clear  /exit · y/n/a to approve · Esc to interrupt")
		m.sync()
		return m, nil
	}

	m.appendLine("")
	m.appendLine("› " + text)
	m.st = stateWorking
	m.sync()
	if m.agent == nil {
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	ag := m.agent
	go func() {
		ans, err := ag.Send(ctx, text)
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
