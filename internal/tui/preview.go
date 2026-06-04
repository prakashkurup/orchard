package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/repo"
)

// previewDocs are the markdown files orchard can render, agent-instructions first:
// "what context does the agent load here?" then the repo's README.
var previewDocs = []string{"CLAUDE.md", "AGENTS.md", "README.md"}

// openPreview renders a repo's instructions / README with glamour in a scrollable
// pager, defaulting to the first agent-instruction file that exists.
func (m model) openPreview(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	m.previewRepo = r
	m.previewDocs = availableDocs(r.Path)
	m.previewIdx = 0
	m.returnMode = m.mode
	m.mode = modePreview
	m.setPreviewContent()
	return m, nil
}

// availableDocs is the subset of previewDocs that exist in the repo, in priority
// order. In demo mode it pretends CLAUDE.md exists so the feature is demoable.
func availableDocs(repoPath string) []string {
	if demoMode() {
		return []string{"CLAUDE.md", "README.md"}
	}
	var out []string
	for _, name := range previewDocs {
		if fi, err := os.Stat(filepath.Join(repoPath, name)); err == nil && !fi.IsDir() {
			out = append(out, name)
		}
	}
	return out
}

func (m *model) setPreviewContent() {
	w := m.detailVP.Width
	m.previewBytes = 0
	if len(m.previewDocs) == 0 {
		m.detailVP.SetContent(fillLine(seg(muted, "  no CLAUDE.md, AGENTS.md, or README.md in this repo"), w, bg))
		m.detailVP.GotoTop()
		return
	}
	name := m.previewDocs[m.previewIdx]
	var md string
	if demoMode() {
		md = demoDoc(name)
	} else {
		data, err := os.ReadFile(filepath.Join(m.previewRepo.Path, name))
		if err != nil {
			m.detailVP.SetContent(fillLine(seg(red, "  "+name+": "+err.Error()), w, bg))
			m.detailVP.GotoTop()
			return
		}
		md = string(data)
	}
	m.previewBytes = len(md)
	m.detailVP.SetContent(renderMarkdown(md, w))
	m.detailVP.GotoTop()
}

// renderMarkdown renders markdown to styled terminal output via glamour, then
// pads each line to width on the app background so the pager has no gray bands.
// On any render error it falls back to sanitized raw text.
func renderMarkdown(md string, width int) string {
	pad := func(s string) string {
		var b strings.Builder
		for i, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(fillLine(ln, width, bg))
		}
		return b.String()
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("tokyo-night"), // matches orchard's own palette
		glamour.WithWordWrap(max(20, width-2)),
	)
	if err == nil {
		if out, rerr := r.Render(md); rerr == nil {
			return pad(out)
		}
	}
	return pad(seg(ice, cleanText(md)))
}

func (m model) handlePreviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "v":
		m.mode = m.returnMode
		if m.mode == modeDetail {
			m.setDetailContent() // the pager reused detailVP; restore the detail body
		}
		return m, nil
	case "tab":
		if len(m.previewDocs) > 1 {
			m.previewIdx = (m.previewIdx + 1) % len(m.previewDocs)
			m.setPreviewContent()
		}
	}
	return m, nil
}

func (m model) previewView(width int) string {
	name := "docs"
	if len(m.previewDocs) > 0 {
		name = m.previewDocs[m.previewIdx]
	}
	title := titleStyle.Render(" "+name) + subtleStyle.Render("  · "+m.previewRepo.Name)
	// for the files the agent actually loads, show size + an estimated per-session
	// token cost (~4 chars/token), warned in yellow when it is large.
	if (name == "CLAUDE.md" || name == "AGENTS.md") && m.previewBytes > 0 {
		size := fmt.Sprintf("%dB", m.previewBytes)
		if m.previewBytes >= 1000 {
			size = fmt.Sprintf("%dKB", m.previewBytes/1000)
		}
		info := fmt.Sprintf("  %s · ~%s tokens/session", size, humanTokens(m.previewBytes/4))
		if m.previewBytes > claudeMDLargeBytes {
			title += lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)).Background(lipgloss.Color(bg)).Bold(true).Render("  ⚠" + info)
		} else {
			title += subtleStyle.Render(info)
		}
	}
	if len(m.previewDocs) > 1 {
		title += subtleStyle.Render("   (" + strings.Join(m.previewDocs, " · ") + ")")
	}
	rule := hrule(width)
	parts := []string{cmdHint("↑↓", "scroll"), cmdHint("g/G", "top/bottom")}
	if len(m.previewDocs) > 1 {
		parts = append(parts, cmdHint("tab", "switch doc"))
	}
	parts = append(parts, cmdHint("esc", "back"))
	hints := fillLine(strings.Join(parts, ""), width, bg)
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg),
		rule,
		m.detailVP.View(),
		rule,
		hints,
	)
}
