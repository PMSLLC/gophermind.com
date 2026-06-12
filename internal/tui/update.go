package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 2)
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tokenMsg:
		m.stream += string(msg)
		m.tokens++
		return m, waitFor(m.sub)

	case assistantMsg:
		m.transcript += "\n" + string(msg) + "\n"
		return m, waitFor(m.sub)

	case toolCallMsg:
		m.transcript += "\n● " + msg.name + "  " + oneLine(msg.args) + "\n"
		return m, waitFor(m.sub)

	case toolResultMsg:
		m.transcript += "  " + oneLine(msg.text) + "\n"
		return m, waitFor(m.sub)

	case approvalMsg:
		m.st = stateApproval
		m.pending = msg
		return m, waitFor(m.sub)

	case doneMsg:
		if s := strings.TrimSpace(m.stream); s != "" {
			if out, err := m.render.Render(s); err == nil {
				m.transcript += "\n" + out
			} else {
				m.transcript += "\n" + s + "\n"
			}
		}
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		return m, waitFor(m.sub)

	case errMsg:
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		m.transcript += "\nerror: " + msg.err.Error() + "\n"
		return m, waitFor(m.sub)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return m, tea.Quit

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
		m.transcript = ""
		m.stream = ""
		if m.agent != nil {
			m.agent.Reset()
		}
		return m, nil
	case "/help":
		m.transcript += "\nCommands: /help  /clear  /exit · y/n/a to approve · Esc to interrupt\n"
		return m, nil
	}

	m.transcript += "\n› " + text + "\n"
	m.st = stateWorking
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
