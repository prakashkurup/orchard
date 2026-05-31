package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
)

func (m model) handleCloneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		m.cloneInput.Blur()
		return m, nil
	case "enter":
		url := strings.TrimSpace(m.cloneInput.Value())
		if url == "" {
			return m, nil
		}
		m.mode = modeList
		m.cloneInput.Blur()
		m.loading = true
		m.status = "planting " + orchardgit.RepoNameFromURL(url) + "…"
		return m, cloneCmd(url, m.root)
	}
	var cmd tea.Cmd
	m.cloneInput, cmd = m.cloneInput.Update(msg)
	return m, cmd
}

func cloneCmd(rawURL, root string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		name, err := orchardgit.Clone(ctx, rawURL, root)
		return cloneDoneMsg{name: name, err: err}
	}
}

func (m model) cloneView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 44, 60)

	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render(" Clone repository"))
		add("")
		add(fg(accent).Bold(true).Render(" / ") + lipgloss.NewStyle().Background(lipgloss.Color(panel)).Render(m.cloneInput.View()))
		add("")
		add(fg(muted).Render("paste an SSH/HTTPS URL or owner/repo"))
		add(fg(muted).Render("clones into " + displayRoot(m.root)))
		add("")
		add(fg(muted).Render("⏎ clone   ·   esc cancel"))
	})
}
