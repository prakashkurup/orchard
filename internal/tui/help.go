package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "?", "enter":
		m.mode = modeList
		return m, nil
	case "up", "k":
		m.detailVP.ScrollUp(1)
	case "down", "j":
		m.detailVP.ScrollDown(1)
	case "pgup":
		m.detailVP.ScrollUp(m.detailVP.Height)
	case "pgdown":
		m.detailVP.ScrollDown(m.detailVP.Height)
	}
	return m, nil
}

func (m model) helpBody(width int) string {
	fg := bgFG
	line := func(s string) string { return fillLine(s, width, bg) }
	var rows []string
	rows = append(rows, line(""), line(fg(blue).Bold(true).Render("  STATUS SYMBOLS  (ST column)")))
	legend := []struct{ g, c, d, gloss string }{
		{"✓", green, "clean - up to date", "healthy"},
		{"!", yellow, "dirty - uncommitted changes", "untended"},
		{"↓", red, "behind remote", "needs water"},
		{"↑", green, "ahead of remote", "fruiting"},
		{"↕", orange, "diverged (ahead and behind)", "tangled"},
		{"⎇", teal, "on a non-default / feature branch", "new growth"},
		{"⌁", orange, "detached HEAD", "fallen branch"},
		{"?", yellow, "no upstream configured", "wild"},
		{"×", red, "error reading repo", "blighted"},
	}
	for _, l := range legend {
		rows = append(rows, line(fg(l.c).Bold(true).Render("    "+l.g)+fg(ice).Render("   "+padRight(l.d, 34))+fg(muted).Render(l.gloss)))
	}
	rows = append(rows, line(fg(accent).Bold(true).Render("    ●")+fg(ice).Render("   new commits since last visit (in the repo name)")))

	rows = append(rows, line(""), line(fg(blue).Bold(true).Render("  CHANGES COLUMN")))
	changes := []struct{ g, c, d string }{
		{"↑", green, "commits ahead of remote (to push)"},
		{"↓", red, "commits behind remote (to pull)"},
		{"●", yellow, "uncommitted changed files"},
		{"≡", cyan, "stashes"},
		{"·", muted, "clean and in sync"},
	}
	for _, l := range changes {
		rows = append(rows, line(fg(l.c).Bold(true).Render("    "+l.g)+fg(ice).Render("   "+l.d)))
	}
	rows = append(rows, line(fg(green).Bold(true).Render("    ▁▄█")+fg(ice).Render("   ACTIVITY: weekly commit cadence over the last 12 weeks")))
	rows = append(rows, line(fg(claudeC).Bold(true).Render("    3h")+fg(ice).Render("   CLAUDE: last Claude Code session ")+fg(red).Render("(red !  = Claude-edited, uncommitted)")))

	rows = append(rows, line(""), line(fg(blue).Bold(true).Render("  KEYS")))
	keys := [][2]string{
		{"↑↓ / j k", "move"},
		{"g / G", "top / bottom"},
		{"n", "jump to next repo with new commits"},
		{"space / a", "select current / select all visible"},
		{"x", "clear all selections"},
		{"enter", "repo detail (status, graph, remotes)"},
		{"d", "view the working-tree diff (vs HEAD)"},
		{"p / P", "pull selected / all (ff-only)"},
		{"f / F", "fetch selected / all"},
		{"b", "switch branch"},
		{"c", "open Claude Code in new tab(s) · confirm if >1 repo"},
		{"C", "resume the last Claude Code session in this repo"},
		{"H", "browse and resume past Claude Code sessions for this repo"},
		{"A", "one Claude session across selected repos (space-select 2+ first)"},
		{"M", "draft a commit message in a window (copy / regenerate)"},
		{"e / E", "open in editor / change default editor"},
		{"O", "open repo(s) in browser · confirm if >1 repo"},
		{"S", "search code across all repos"},
		{"L", "worklog - your commits across repos"},
		{"T", "stats page - languages, Claude usage, and heatmaps"},
		{"+", "clone a repo into the dashboard"},
		{"/", "filter by name   ·   tab quick filter"},
		{"s / o", "cycle sort / toggle grouping"},
		{"r / w", "refresh now / toggle live auto-refresh"},
		{"? / q", "this help / quit"},
	}
	for _, k := range keys {
		rows = append(rows, line(fg(blue).Render("    "+padRight(k[0], 12))+fg(ice).Render("  "+k[1])))
	}
	return strings.Join(rows, "\n")
}

func (m model) helpView(width int) string {
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).Render("ORCHARD") +
		subtleStyle.Render("  · help & legend")
	rule := hrule(width)
	hints := fillLine(strings.Join([]string{cmdHint("↑↓", "scroll"), cmdHint("esc", "close")}, ""), width, bg)
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg),
		rule,
		m.detailVP.View(),
		rule,
		hints,
	)
}
