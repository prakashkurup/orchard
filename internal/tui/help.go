package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// openHelp shows the full keymap, remembering the screen it was opened from so
// esc returns there (the dashboard, or the detail page when opened from it).
func (m model) openHelp() (tea.Model, tea.Cmd) {
	m.returnMode = m.mode
	m.mode = modeHelp
	m.detailVP.SetContent(m.helpBody(m.detailVP.Width))
	m.detailVP.GotoTop()
	return m, nil
}

func (m model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "?", "enter":
		m.mode = m.returnMode
		if m.mode == modeDetail {
			m.setDetailContent() // the keymap reused detailVP; restore the detail body
		}
		return m, nil
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

	groups := []struct {
		name string
		keys [][2]string
	}{
		{"NAVIGATE", [][2]string{
			{"↑↓ / j k", "move · g / G top / bottom"},
			{"n", "jump to next repo with new commits"},
		}},
		{"SELECT", [][2]string{
			{"space / a", "select current / all visible"},
			{"x", "clear all selections"},
			{"y", "copy the repo path to the clipboard"},
			{"mouse", "click a row to select · wheel to scroll"},
		}},
		{"GIT", [][2]string{
			{"p / P", "pull selected / all (ff-only)"},
			{"f / F", "fetch selected / all"},
			{"b", "switch branch"},
			{"r / w", "refresh now / toggle live refresh (also fetches remotes in the background)"},
		}},
		{"AI AGENTS (CLAUDE CODE / CODEX)", [][2]string{
			{"c / C", "launch / resume last session"},
			{"H", "browse and resume past sessions"},
			{"f", "files the agent touched (detail page; open / diff them)"},
			{"v", "preview CLAUDE.md / AGENTS.md / README (detail page)"},
			{"A", "one session across selected repos (2+)"},
			{"M", "draft a commit message in a window"},
			{"I", "wire AGENTS.md into a new CLAUDE.md (selected)"},
			{"B", "build code graph for orchard mcp (selected; served to the agent)"},
			{"D", "delete the code graph (cache; rebuild with B)"},
			{"m", "toggle auto-wiring the code graph into launches (this session)"},
			{"R", "search all past sessions, then resume one"},
			{"W", "workspace presets: save a repo set, launch a session on it"},
		}},
		{"INSPECT", [][2]string{
			{"enter", "repo detail (status, graph, remotes)"},
			{"d", "working-tree diff (vs HEAD)"},
			{"L / T", "worklog / stats and heatmaps"},
			{"U", "agent usage and cost (via CodeBurn)"},
			{"S", "search code across all repos"},
		}},
		{"FILTER & SORT", [][2]string{
			{"/", "filter: text, or branch: / name: prefix"},
			{"tab", "quick filters: dirty, behind, feature, at-risk, ai-touched, needs-md (no CLAUDE.md)"},
			{"s / o", "cycle sort / toggle grouping"},
		}},
		{"APP", [][2]string{
			{"e / E", "open in editor / change default editor"},
			{"O / +", "open in browser / clone a repo"},
			{"? / q", "this help / quit"},
		}},
	}
	for _, g := range groups {
		rows = append(rows, line(""), line(fg(blue).Bold(true).Render("  "+g.name)))
		for _, k := range g.keys {
			rows = append(rows, line(fg(blue).Render("    "+padRight(k[0], 12))+fg(ice).Render("  "+k[1])))
		}
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
