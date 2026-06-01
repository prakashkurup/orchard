package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/repo"
)

// sessionsLimit caps how many past sessions the picker loads per repo.
const sessionsLimit = 50

type sessionsMsg struct {
	path     string
	sessions []claude.Session
}

// openSessions opens the Claude Code session-history picker for a repo. Resuming a
// session needs Claude Code, so other assistants get a status note instead.
func (m model) openSessions(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	if !m.assistantIsClaude() {
		m.status = "session history needs Claude Code"
		return m, nil
	}
	m.sessionsRepo = r
	m.sessions = nil
	m.sessionCursor = 0
	m.sessionsLoading = true
	m.mode = modeSessions
	return m, sessionsCmd(r)
}

func sessionsCmd(r repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return sessionsMsg{path: r.Path, sessions: demoSessions()} }
	}
	return func() tea.Msg {
		return sessionsMsg{path: r.Path, sessions: claude.Sessions(r.Path, sessionsLimit)}
	}
}

func (m model) handleSessionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "h", "H":
		m.mode = modeList
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
		m.mode = modeList
		return m.runAssistant(r.Path, []string{"--resume", s.ID},
			"resuming "+m.assistantLabel+" · "+r.Name)
	}
	return m, nil
}

func (m model) sessionsView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 44, 88)
	return modalBox(inner, func(add func(string)) {
		add(fg(claudeC).Bold(true).Render("✦ Claude Code sessions") + fg(muted).Render("  · "+m.sessionsRepo.Name))
		add("")
		switch {
		case m.sessionsLoading:
			add(fg(muted).Render("  loading sessions…"))
		case len(m.sessions) == 0:
			add(fg(muted).Render("  no Claude Code sessions in this repo yet"))
		default:
			const maxRows = 12
			start := 0
			if m.sessionCursor >= maxRows {
				start = m.sessionCursor - maxRows + 1
			}
			end := min(start+maxRows, len(m.sessions))
			// fixed columns so title / model+turns / age line up across rows
			const relW, metaW = 10, 26
			titleW := max(12, inner-4-1-metaW-3-relW)
			for i := start; i < end; i++ {
				s := m.sessions[i]
				cursor := fg(panel).Render("  ")
				titleStyle := fg(ice)
				if i == m.sessionCursor {
					cursor = fg(claudeC).Bold(true).Render("▌ ")
					titleStyle = fg(selFg).Bold(true)
				}
				model := claude.PrettyModel(s.Model)
				if model == "" {
					model = "claude"
				}
				meta := fmt.Sprintf("%s · %dt · %s", model, s.Assistant, humanTokens(s.Tokens))
				add(cursor +
					titleStyle.Render(padRight(fit(s.DisplayTitle(), titleW), titleW)) +
					fg(muted).Render(" "+padRight(fit(meta, metaW), metaW)+" · "+fit(relTime(s.Modified), relW)))
			}
			if len(m.sessions) > maxRows {
				add(fg(muted).Render(fmt.Sprintf("  … %d more", len(m.sessions)-maxRows)))
			}
		}
		add("")
		add(fg(muted).Render("↑↓ move · ⏎ resume · esc cancel"))
	})
}
