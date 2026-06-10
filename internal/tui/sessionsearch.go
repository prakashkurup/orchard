package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	"github.com/prakashkurup/orchard/internal/repo"
)

type sessionSearchMsg struct {
	query string
	hits  []claude.SessionHit
}

func (m model) openSessionSearch() (tea.Model, tea.Cmd) {
	if !m.agentSupportsSessions() {
		m.status = "session search needs claude or codex"
		return m, nil
	}
	m.returnMode = m.mode
	m.mode = modeSessionSearch
	m.sessionSearchInput.SetValue("")
	m.sessionSearchInput.Focus()
	m.sessionSearchFocus = true
	m.sessionSearchResults = nil
	m.sessionSearchQuery = ""
	m.sessionSearchCursor = 0
	return m, textinput.Blink
}

func sessionSearchCmd(repos []repo.Repo, query string, useCodex bool) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return sessionSearchMsg{query: query, hits: demoSessionHits(query)} }
	}
	return func() tea.Msg {
		targets := make([]claude.Target, 0, len(repos))
		for _, r := range repos {
			targets = append(targets, claude.Target{Name: r.Name, Path: r.Path})
		}
		if useCodex {
			return sessionSearchMsg{query: query, hits: codex.SearchSessions(targets, query, 200)}
		}
		return sessionSearchMsg{query: query, hits: claude.SearchSessions(targets, query, 200)}
	}
}

func (m model) handleSessionSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = m.returnMode
		return m, nil
	}

	if m.sessionSearchFocus {
		switch msg.String() {
		case "enter":
			q := strings.TrimSpace(m.sessionSearchInput.Value())
			if q == "" {
				return m, nil
			}
			m.sessionSearchQuery = q
			m.sessionSearchRunning = true
			m.sessionSearchResults = nil
			m.sessionSearchCursor = 0
			return m, sessionSearchCmd(m.repos, q, m.assistantIsCodex())
		default:
			var cmd tea.Cmd
			m.sessionSearchInput, cmd = m.sessionSearchInput.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "up", "k", "ctrl+p":
		if m.sessionSearchCursor > 0 {
			m.sessionSearchCursor--
		}
	case "down", "j", "ctrl+n":
		if m.sessionSearchCursor < len(m.sessionSearchResults)-1 {
			m.sessionSearchCursor++
		}
	case "/", "i":
		m.sessionSearchFocus = true
		m.sessionSearchInput.Focus()
		return m, textinput.Blink
	case "enter":
		if len(m.sessionSearchResults) == 0 {
			return m, nil
		}
		h := m.sessionSearchResults[m.sessionSearchCursor]
		m.mode = m.returnMode
		return m.runAssistant(h.RepoPath, []string{"--resume", h.ID}, nil, "resuming "+m.assistantLabel+" · "+h.RepoName, []string{h.RepoPath})
	}
	return m, nil
}

func (m model) sessionSearchView(width int) string {
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).Render("ORCHARD") +
		subtleStyle.Render("  · search Claude sessions")
	rule := hrule(width)

	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(claudeC)).Background(lipgloss.Color(bg)).Bold(true).Render(" ✦ ")
	queryLine := fillLine(prompt+m.sessionSearchInput.View(), width, bg)

	var body []string
	switch {
	case m.sessionSearchRunning:
		body = append(body, fillLine(seg(muted, "  searching…"), width, bg))
	case m.sessionSearchQuery == "":
		body = append(body, fillLine(seg(muted, "  type a query and press enter to search every repo's Claude history"), width, bg))
	case len(m.sessionSearchResults) == 0:
		body = append(body, fillLine(seg(muted, "  no sessions mention ")+seg(ice, m.sessionSearchQuery), width, bg))
	default:
		body = append(body, fillLine(seg(muted, fmt.Sprintf("  %d sessions match ", len(m.sessionSearchResults)))+seg(ice, m.sessionSearchQuery), width, bg))
		body = append(body, fillLine("", width, bg))
		// window of results around the cursor that fits the remaining height
		rows := max(4, m.height-9)
		perHit := 2
		win := max(1, rows/perHit)
		start := 0
		if m.sessionSearchCursor >= win {
			start = m.sessionSearchCursor - win + 1
		}
		end := min(len(m.sessionSearchResults), start+win)
		for i := start; i < end; i++ {
			h := m.sessionSearchResults[i]
			sel := i == m.sessionSearchCursor
			cursor := seg(muted, "  ")
			nameC, titleC := blue, ice
			if sel {
				cursor = segB(accent, "▸ ")
				nameC = accent
			}
			head := cursor + segB(nameC, fit(h.RepoName, 24)) + seg(muted, "  "+relTime(h.Modified)+"  ") + seg(titleC, fit(h.DisplayTitle(), max(10, width-44)))
			body = append(body, fillLine(head, width, bg))
			body = append(body, fillLine(seg(muted, "      "+fit(h.Snippet, max(10, width-8))), width, bg))
		}
	}

	hint := "↑↓ move · enter resume · / or i edit query · esc close"
	if m.sessionSearchFocus {
		hint = "enter search · esc close"
	}
	hints := fillLine(subtleStyle.Render("  "+hint), width, bg)

	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg), rule, queryLine, rule,
		strings.Join(body, "\n"), rule, hints,
	)
}
