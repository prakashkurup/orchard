package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	"github.com/prakashkurup/orchard/internal/repo"
)

// sessionsLimit caps how many past sessions the picker loads per repo.
const sessionsLimit = 50

type sessionsMsg struct {
	request  uint64
	path     string
	sessions []claude.Session
}

// openSessions opens the session-history picker for a repo. Resuming needs an
// assistant with readable history (Claude Code or Codex); other assistants get a
// status note instead.
func (m model) openSessions(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	if !m.agentSupportsSessions() {
		m.status = "session history needs claude or codex"
		return m, nil
	}
	m.sessionsRepo = r
	m.beginWorkspace(r)
	m.sessions = nil
	m.sessionCursor = 0
	m.sessionsLoading = true
	m.returnMode = m.mode
	m.mode = modeSessions
	m.sessionsRequest++
	m.layoutWorkspace()
	return m, sessionsCmd(r, m.assistantIsCodex(), m.sessionsRequest)
}

func sessionsCmd(r repo.Repo, useCodex bool, request uint64) tea.Cmd {
	if demoMode() {
		if useCodex {
			return func() tea.Msg { return sessionsMsg{request: request, path: r.Path, sessions: demoCodexSessions()} }
		}
		return func() tea.Msg { return sessionsMsg{request: request, path: r.Path, sessions: demoSessions()} }
	}
	return func() tea.Msg {
		if useCodex {
			return sessionsMsg{request: request, path: r.Path, sessions: codex.Sessions(r.Path, sessionsLimit)}
		}
		return sessionsMsg{request: request, path: r.Path, sessions: claude.Sessions(r.Path, sessionsLimit)}
	}
}

func (m model) handleSessionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "h", "H":
		m.mode = m.returnMode
		return m, nil
	case "up", "k", "ctrl+p":
		m.sessionCursor = clamp(m.sessionCursor-1, 0, max(0, len(m.sessions)-1))
	case "down", "j", "ctrl+n":
		m.sessionCursor = clamp(m.sessionCursor+1, 0, max(0, len(m.sessions)-1))
	case "enter":
		if m.sessionCursor < 0 || m.sessionCursor >= len(m.sessions) {
			return m, nil
		}
		s := m.sessions[m.sessionCursor]
		r := m.sessionsRepo
		m.mode = m.returnMode
		return m.runAssistant(r.Path, m.agentResumeArgs(s.ID), nil,
			"resuming "+m.assistantLabel+" · "+r.Name)
	}
	return m, nil
}

// The workspace history is a full-height list alongside repo navigation.
func (m model) workspaceSessionsView(width int) string {
	label, color := "Claude Code", claudeC
	if m.assistantIsCodex() {
		label, color = "Codex", codexC
	}
	rows := []string{fillLine(segB(color, " "+label+" sessions")+seg(muted, " · "+cleanText(m.sessionsRepo.Name)), width, bg), hrule(width)}
	available := max(1, m.height-8)
	switch {
	case m.sessionsLoading:
		rows = append(rows, fillLine(seg(muted, "  loading sessions…"), width, bg))
	case len(m.sessions) == 0:
		rows = append(rows, fillLine(seg(muted, "  no "+label+" sessions in this repo yet"), width, bg))
	default:
		start := max(0, m.sessionCursor-available+1)
		for i := start; i < min(start+available, len(m.sessions)); i++ {
			s := m.sessions[i]
			marker, fg := "  ", ice
			if i == m.sessionCursor {
				marker, fg = "▌ ", color
			}
			meta := fmt.Sprintf("%dt · %s", s.Assistant, relTime(s.Modified))
			titleW := max(1, width-3-lipgloss.Width(meta))
			rows = append(rows, fillLine(seg(fg, marker+padRight(s.DisplayTitle(), titleW))+seg(muted, " "+meta), width, bg))
		}
	}
	for len(rows) < available+2 {
		rows = append(rows, fillLine("", width, bg))
	}
	rows = append(rows, hrule(width), fillLine(packHints(width, []string{cmdHint("↑↓", "session"), cmdHint("enter", "resume"), cmdHint("esc", "back")}, nil), width, bg))
	return strings.Join(rows, "\n")
}
