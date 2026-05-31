package tui

import (
	"fmt"

	"github.com/prakashkurup/orchard/internal/repo"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmKind is the action a confirmation modal will run on Yes.
type confirmKind int

const (
	confirmClaude confirmKind = iota
	confirmBrowser
)

func (m model) requestClaude(targets []repo.Repo) (tea.Model, tea.Cmd) {
	if len(targets) <= 1 {
		return m.openClaude(targets)
	}
	return m.enterConfirm(confirmClaude, targets)
}

func (m model) requestBrowser(targets []repo.Repo) (tea.Model, tea.Cmd) {
	switch len(targets) {
	case 0:
		m.status = "nothing to open"
		return m, nil
	case 1:
		m.status = "opening " + targets[0].Name + " in browser"
		return m, openCmd(targets[0])
	default:
		return m.enterConfirm(confirmBrowser, targets)
	}
}

func (m model) enterConfirm(kind confirmKind, targets []repo.Repo) (tea.Model, tea.Cmd) {
	m.mode = modeConfirm
	m.confirmKind = kind
	m.confirmRepos = targets
	m.confirmYes = true // default to Yes
	m.status = ""
	return m, nil
}

func (m model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "n", "q":
		m.mode = modeList
		m.confirmRepos = nil
		m.status = "cancelled"
		return m, nil
	case "left", "right", "h", "l", "tab":
		m.confirmYes = !m.confirmYes
		return m, nil
	case "y":
		return m.runConfirm()
	case "enter":
		if !m.confirmYes {
			m.mode = modeList
			m.confirmRepos = nil
			m.status = "cancelled"
			return m, nil
		}
		return m.runConfirm()
	}
	return m, nil
}

// runConfirm dispatches the pending action and returns to the list.
func (m model) runConfirm() (tea.Model, tea.Cmd) {
	m.mode = modeList
	kind, targets := m.confirmKind, m.confirmRepos
	m.confirmRepos = nil
	switch kind {
	case confirmBrowser:
		return m.openBrowserAll(targets)
	default:
		return m.openClaude(targets)
	}
}

// openBrowserAll opens every target's origin in the browser.
func (m model) openBrowserAll(targets []repo.Repo) (tea.Model, tea.Cmd) {
	if len(targets) == 0 {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, len(targets))
	for _, r := range targets {
		cmds = append(cmds, openCmd(r))
	}
	m.status = fmt.Sprintf("opening %d repos in browser", len(targets))
	return m, tea.Batch(cmds...)
}

func (m model) confirmView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 40, 68)
	n := len(m.confirmRepos)

	// per-action title / prompt / list glyph
	title, glyph, glyphColor := "✦ Open Claude Code", "✦", claudeC
	promptLines := []string{fmt.Sprintf("Open Claude Code for these %d repos", n), "in separate windows?"}
	if m.confirmKind == confirmBrowser {
		title, glyph, glyphColor = iconRemote+" Open in browser", iconRemote, blue
		promptLines = []string{fmt.Sprintf("Open these %d repos in", n), "separate browser tabs?"}
	}

	// Yes / No buttons - the active one is filled, defaulting to Yes.
	btn := func(label, color string, active bool) string {
		if active {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(bg)).Background(lipgloss.Color(color)).
				Bold(true).Padding(0, 2).Render(label)
		}
		return fg(muted).Padding(0, 2).Render(label)
	}

	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render(title) + fg(muted).Render(fmt.Sprintf("  · %d repos", n)))
		add("")
		for _, p := range promptLines {
			add(fg(ice).Render(p))
		}
		add("")
		const maxRows = 8
		for i, r := range m.confirmRepos {
			if i >= maxRows {
				add(fg(muted).Render(fmt.Sprintf("    … and %d more", n-maxRows)))
				break
			}
			add(fg(glyphColor).Render("  "+glyph+" ") + fg(ice).Render(fit(r.Name, inner-6)))
		}
		add("")
		add("  " + btn("Yes", green, m.confirmYes) + fg(panel).Render("   ") + btn("No", red, !m.confirmYes))
		add("")
		add(fg(muted).Render("⏎ confirm · ←→ switch · esc cancel"))
	})
}
