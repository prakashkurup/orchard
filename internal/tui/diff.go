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
	m.diffPath = ""
	m.diffText = ""
	m.returnMode = m.mode
	m.mode = modeDiff
	m.detailVP.SetContent(fillLine(subtleStyle.Render("  loading diff…"), m.detailVP.Width, bg))
	m.detailVP.GotoTop()
	return m, diffCmd(r)
}

// openFileDiff shows the working-tree diff for a single file (vs HEAD), reusing
// the scrollable diff view. Returning from it restores the caller (e.g. the
// Files-touched list it was opened from).
func (m model) openFileDiff(r repo.Repo, relPath string) (tea.Model, tea.Cmd) {
	if r.Path == "" || relPath == "" {
		return m, nil
	}
	m.diffRepo = r
	m.diffPath = relPath
	m.diffText = ""
	m.returnMode = m.mode
	m.mode = modeDiff
	m.detailVP.SetContent(fillLine(subtleStyle.Render("  loading diff…"), m.detailVP.Width, bg))
	m.detailVP.GotoTop()
	return m, diffCmd(r, relPath)
}

func diffCmd(r repo.Repo, pathspec ...string) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return diffMsg{path: r.Path, text: demoDiff()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := orchardgit.Diff(ctx, r.Path, pathspec...)
		return diffMsg{path: r.Path, text: out, err: err}
	}
}

func (m model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "d":
		m.mode = m.returnMode
		if m.mode == modeDetail {
			m.setDetailContent() // the diff reused detailVP; restore the detail body
		}
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
		rows = append(rows, fillLine(seg(color, sanitizeDiffLine(ln)), width, bg))
	}
	return strings.Join(rows, "\n")
}

// sanitizeDiffLine strips terminal control bytes (ESC, CR, BEL, the C1 set, etc.)
// from a diff line so crafted working-tree content cannot inject escape sequences
// (window-title/clipboard OSC, cursor moves, SGR spoofing) into the viewer. Tabs
// are kept so code indentation stays readable.
func sanitizeDiffLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (m model) diffView(width int) string {
	scope := "(working tree vs HEAD)"
	if m.diffPath != "" {
		scope = cleanText(m.diffPath)
	}
	title := titleStyle.Render(" Diff") + subtleStyle.Render("  · "+m.diffRepo.Name+"  "+scope)
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
