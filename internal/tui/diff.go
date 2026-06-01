package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

type diffMsg struct {
	path string
	text string
	err  error
}

// openDiff shows the working-tree diff (vs HEAD) for a repo in a scrollable view.
func (m model) openDiff(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	m.diffRepo = r
	m.diffText = ""
	m.mode = modeDiff
	m.detailVP.SetContent(fillLine(subtleStyle.Render("  loading diff…"), m.detailVP.Width, bg))
	m.detailVP.GotoTop()
	return m, diffCmd(r)
}

func diffCmd(r repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return diffMsg{path: r.Path, text: demoDiff()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := orchardgit.Diff(ctx, r.Path)
		return diffMsg{path: r.Path, text: out, err: err}
	}
}

func (m model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "d":
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
	case "g", "home":
		m.detailVP.GotoTop()
	case "G", "end":
		m.detailVP.GotoBottom()
	}
	return m, nil
}

// colorizeDiff renders unified-diff text with per-line colors, every line painted
// on the app background so there is no banding.
func colorizeDiff(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return fillLine(seg(green, "  "+iconCheck+"  clean - nothing to diff"), width, bg)
	}
	var rows []string
	for _, ln := range strings.Split(text, "\n") {
		var color string
		switch {
		case strings.HasPrefix(ln, "diff --git"), strings.HasPrefix(ln, "index "),
			strings.HasPrefix(ln, "--- "), strings.HasPrefix(ln, "+++ "),
			strings.HasPrefix(ln, "new file"), strings.HasPrefix(ln, "deleted file"),
			strings.HasPrefix(ln, "rename "), strings.HasPrefix(ln, "similarity "):
			color = blue
		case strings.HasPrefix(ln, "@@"):
			color = cyan
		case strings.HasPrefix(ln, "+"):
			color = green
		case strings.HasPrefix(ln, "-"):
			color = red
		default:
			color = muted
		}
		rows = append(rows, fillLine(seg(color, ln), width, bg))
	}
	return strings.Join(rows, "\n")
}

func (m model) diffView(width int) string {
	title := titleStyle.Render(" Diff") + subtleStyle.Render("  · "+m.diffRepo.Name+"  (working tree vs HEAD)")
	rule := hrule(width)
	hints := fillLine(strings.Join([]string{
		cmdHint("↑↓", "scroll"), cmdHint("g/G", "top/bottom"), cmdHint("esc", "back"),
	}, ""), width, bg)
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg),
		rule,
		m.detailVP.View(),
		rule,
		hints,
	)
}
