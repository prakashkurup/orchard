package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/editor"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/search"
)

func searchCmd(repos []repo.Repo, query string) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return searchResultMsg{query: query, results: demoSearch(query)} }
	}
	return func() tea.Msg {
		targets := make([]search.Target, 0, len(repos))
		for _, r := range repos {
			targets = append(targets, search.Target{Name: r.Name, Path: r.Path})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return searchResultMsg{query: query, results: search.Search(ctx, targets, query, 0)}
	}
}

// searchFileGroup is one file's matches; searchRepoGroup gathers a repo's files.
type searchFileGroup struct {
	file    string
	matches []search.Match
}
type searchRepoGroup struct {
	repo  string
	count int
	files []searchFileGroup
}

// searchGroups organizes the results as repo -> file.
func (m model) searchGroups() []searchRepoGroup {
	var groups []searchRepoGroup
	for _, r := range m.searchResults {
		g := searchRepoGroup{repo: r.Repo}
		idx := map[string]int{}
		for _, mt := range r.Matches {
			i, ok := idx[mt.File]
			if !ok {
				i = len(g.files)
				idx[mt.File] = i
				g.files = append(g.files, searchFileGroup{file: mt.File})
			}
			g.files[i].matches = append(g.files[i].matches, mt)
			g.count++
		}
		if g.count > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

func (m *model) flattenSearch() {
	m.searchFlat = m.searchFlat[:0]
	for _, g := range m.searchGroups() {
		for _, f := range g.files {
			m.searchFlat = append(m.searchFlat, f.matches...)
		}
	}
	m.searchCursor = clamp(m.searchCursor, 0, max(0, len(m.searchFlat)-1))
}

// splitPath divides a (possibly left-truncated) path into its directory prefix
// and basename so the directory can be dimmed and the filename emphasized.
func splitPath(p string) (dir, base string) {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1], p[i+1:]
	}
	return "", p
}

// highlightMatch renders text on bgc, with case-insensitive runs of query in the
// search-highlight color. Width is preserved (only color changes), so callers can
// truncate first and highlight the result safely.
func highlightMatch(text, query, baseFg, bgc string) string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(baseFg)).Background(lipgloss.Color(bgc))
	lower, lq := strings.ToLower(text), strings.ToLower(query)
	// Skip highlighting when byte offsets in the lowercased copy would not map
	// cleanly back onto the original (rare Unicode case), to keep slicing safe.
	if lq == "" || len(lower) != len(text) {
		return base.Render(text)
	}
	hl := lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)).Background(lipgloss.Color(bgc)).Bold(true)
	var b strings.Builder
	for i := 0; ; {
		j := strings.Index(lower[i:], lq)
		if j < 0 {
			b.WriteString(base.Render(text[i:]))
			return b.String()
		}
		j += i
		if j > i {
			b.WriteString(base.Render(text[i:j]))
		}
		b.WriteString(hl.Render(text[j : j+len(lq)]))
		i = j + len(lq)
	}
}

func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchFocus {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.mode = modeList
			m.searchInput.Blur()
			return m, nil
		case "enter":
			q := strings.TrimSpace(m.searchInput.Value())
			if q == "" {
				return m, nil
			}
			m.searchQuery = q
			m.searchRunning = true
			m.loading = true
			m.status = "searching: " + q
			m.searchInput.Blur()
			m.searchVP.SetContent(fillLine(subtleStyle.Render("  searching…"), m.searchVP.Width, bg))
			return m, searchCmd(m.repos, q)
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeList
		return m, nil
	case "i", "/":
		m.searchFocus = true
		m.searchInput.SetValue(m.searchQuery)
		m.searchInput.CursorEnd()
		return m, m.searchInput.Focus()
	case "up", "k":
		m.searchCursor = clamp(m.searchCursor-1, 0, max(0, len(m.searchFlat)-1))
		m.setSearchContent()
	case "down", "j":
		m.searchCursor = clamp(m.searchCursor+1, 0, max(0, len(m.searchFlat)-1))
		m.setSearchContent()
	case "pgup":
		m.searchCursor = clamp(m.searchCursor-m.searchVP.Height, 0, max(0, len(m.searchFlat)-1))
		m.setSearchContent()
	case "pgdown":
		m.searchCursor = clamp(m.searchCursor+m.searchVP.Height, 0, max(0, len(m.searchFlat)-1))
		m.setSearchContent()
	case "enter":
		return m.openMatch()
	case "O":
		return m.openMatchOnWeb()
	}
	return m, nil
}

// openMatchOnWeb opens the highlighted match on the repo's web host at its line.
func (m model) openMatchOnWeb() (tea.Model, tea.Cmd) {
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchFlat) {
		return m, nil
	}
	mt := m.searchFlat[m.searchCursor]
	r := m.repoByPath(mt.Path)
	branch := r.Branch
	if r.Detached || branch == "" {
		branch = r.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}
	m.status = "opening " + mt.Repo + " on the web…"
	return m, openMatchURLCmd(mt.Path, mt.File, mt.Line, branch)
}

func (m model) openMatch() (tea.Model, tea.Cmd) {
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchFlat) {
		return m, nil
	}
	mt := m.searchFlat[m.searchCursor]
	e, ok := m.chosenEditor()
	if !ok {
		m.status = "no editor detected to open match"
		return m, nil
	}
	cmd, terminal := e.CommandAt(filepath.Join(mt.Path, mt.File), mt.Line)
	if cmd == nil {
		m.status = "cannot open in " + e.Name
		return m, nil
	}
	if terminal {
		return m, tea.ExecProcess(cmd, func(error) tea.Msg {
			return statusMsg{text: "returned from " + e.Name}
		})
	}
	name := mt.File
	return m, func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "open failed: " + err.Error()}
		}
		go func() { _ = cmd.Wait() }() // reap the child so it doesn't linger as a zombie
		return statusMsg{text: fmt.Sprintf("opened %s:%d in %s", name, mt.Line, e.Name)}
	}
}

func (m model) chosenEditor() (editor.Editor, bool) {
	if m.editorID != "" {
		if e, ok := editor.ByID(m.editorID); ok && e.Installed() {
			return e, true
		}
	}
	if avail := editor.Available(); len(avail) > 0 {
		return avail[0], true
	}
	return editor.Editor{}, false
}

func (m *model) setSearchContent() {
	width := m.searchVP.Width
	cell := func(fg, bgc, s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bgc)).Render(s)
	}
	cellB := func(fg, bgc, s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bgc)).Bold(true).Render(s)
	}
	if len(m.searchFlat) == 0 {
		msg := "  no matches"
		if m.searchQuery == "" {
			msg = "  type a query and press enter"
		}
		m.searchVP.SetContent(fillLine(subtleStyle.Render(msg), width, bg))
		return
	}

	var rows []string
	add := func(s, bgc string) { rows = append(rows, fillLine(s, width, bgc)) }
	matchIdx := 0 // index into searchFlat (cursor space, matches only)
	selRow := 0   // display row of the selected match
	selLead := 0  // header rows directly above it (its repo/file headers)
	pending := 0  // header rows emitted since the last match row

	for gi, g := range m.searchGroups() {
		if gi > 0 {
			add("", bg)
			pending++
		}

		countStr := fmt.Sprintf("%d", g.count)
		wc := lipgloss.Width(countStr)
		name := fit(g.repo, max(1, width-6-wc))
		dashes := max(0, width-6-lipgloss.Width(name)-wc)
		add(cellB(cyan, bg, "▌ ")+cellB(cyan, bg, name)+
			cell(muted, bg, " "+strings.Repeat("─", dashes)+" "+countStr+" ─"), bg)
		pending++
		for _, f := range g.files {
			// File header on the rail: dim directory, bright basename.
			dir, base := splitPath(fitLeft(f.file, max(6, width-4)))
			add(cell(muted, bg, "│   "+dir)+cell(blue, bg, base), bg)
			pending++
			for _, mt := range f.matches {
				bgc, railFg, lineFg, textFg := bg, muted, muted, ice
				if matchIdx == m.searchCursor {
					bgc, railFg, lineFg, textFg = rowHot, selFg, ice, selFg
					selRow, selLead = len(rows), pending
				}
				prefixText := fmt.Sprintf("  %4d  ", mt.Line)
				rail := cell(railFg, bgc, "│ ")
				prefix := cell(lineFg, bgc, prefixText)
				body := highlightMatch(fit(mt.Text, max(6, width-2-lipgloss.Width(prefixText))), m.searchQuery, textFg, bgc)
				add(rail+prefix+body, bgc)
				pending = 0
				matchIdx++
			}
		}
	}
	m.searchVP.SetContent(strings.Join(rows, "\n"))

	if top := selRow - selLead; top < m.searchVP.YOffset {
		m.searchVP.SetYOffset(max(0, top))
	}
	if bottom := m.searchVP.YOffset + m.searchVP.Height; selRow >= bottom {
		m.searchVP.SetYOffset(selRow - m.searchVP.Height + 1)
	}
}

func (m model) searchView(width int) string {
	title := titleStyle.Render(" Search code")
	var head string
	if m.searchFocus {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).Render(" / ")
		head = title + subtleStyle.Render("   ") + prompt +
			lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(ice)).Render(m.searchInput.View())
	} else {
		count := len(m.searchFlat)
		summary := fmt.Sprintf("“%s”  ·  %d matches in %d repos  ·  %s", m.searchQuery, count, len(m.searchResults), search.Engine())
		head = title + subtleStyle.Render("   ") + statusStyle.Render(fit(summary, max(10, width-lipgloss.Width(title)-4)))
	}
	rule := hrule(width)
	var hints string
	if m.searchFocus {
		hints = fillLine(strings.Join([]string{cmdHint("⏎", "run"), cmdHint("esc", "back")}, ""), width, bg)
	} else {
		hints = fillLine(strings.Join([]string{cmdHint("↑↓", "move"), cmdHint("⏎", "open"), cmdHint("O", "web"), cmdHint("/", "edit query"), cmdHint("esc", "back")}, ""), width, bg)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(head, width, bg),
		rule,
		m.searchVP.View(),
		rule,
		hints,
	)
}
