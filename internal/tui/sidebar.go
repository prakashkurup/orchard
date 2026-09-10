package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/repo"
)

const sidebarWidth = 30

type repoWorkspaceView struct {
	mode          uiMode
	detailOffset  int
	diffOffset    int
	diffPath      string
	sessionCursor int
}

func isWorkspaceMode(mode uiMode) bool {
	return mode == modeDetail || mode == modeDiff || mode == modeSessions
}

func (m model) workspacePath() string {
	switch m.mode {
	case modeDetail:
		return m.detailRepo
	case modeDiff:
		return m.diffRepo.Path
	case modeSessions:
		return m.sessionsRepo.Path
	}
	return ""
}

func (m model) sidebarVisible() bool {
	return isWorkspaceMode(m.mode) && !m.sidebarHidden && m.innerWidth() >= sidebarWidth+2+70 && m.height >= 12
}

func (m model) workspaceWidth() int {
	if m.sidebarVisible() {
		return m.innerWidth() - sidebarWidth - 2
	}
	return m.innerWidth()
}

// Keep navigation separate from the dashboard cursor, filters and multi-selection.
// Snapshot paths, not repo indices: scans can replace or reorder m.repos.
func (m *model) beginWorkspace(r repo.Repo) {
	if len(m.sidebarPaths) == 0 {
		if current, ok := m.currentRepo(); ok {
			m.workspaceDashboardPath = current.Path
			m.workspaceDashboardOffset = m.viewport.YOffset
		}
		repos := append([]repo.Repo(nil), m.repos...)
		sort.SliceStable(repos, func(i, j int) bool {
			if repos[i].Name == repos[j].Name {
				return repos[i].Path < repos[j].Path
			}
			return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
		})
		for _, r := range repos {
			m.sidebarPaths = append(m.sidebarPaths, r.Path)
		}
	}
	for i, path := range m.sidebarPaths {
		if path == r.Path {
			m.sidebarCursor = i
			return
		}
	}
	m.sidebarPaths = append(m.sidebarPaths, r.Path)
	m.sidebarCursor = len(m.sidebarPaths) - 1
}

func (m *model) rememberWorkspaceView() {
	path := m.workspacePath()
	if path == "" {
		return
	}
	if m.workspaceViews == nil {
		m.workspaceViews = map[string]repoWorkspaceView{}
	}
	v := m.workspaceViews[path]
	v.mode = m.mode
	switch m.mode {
	case modeDetail:
		if m.detail != nil {
			v.detailOffset = m.detailVP.YOffset
		}
	case modeDiff:
		v.diffPath = m.diffPath
		if !m.diffLoading {
			v.diffOffset = m.detailVP.YOffset
		}
	case modeSessions:
		if !m.sessionsLoading {
			v.sessionCursor = m.sessionCursor
		}
	}
	m.workspaceViews[path] = v
}

// Reload on activation so remembered positions never imply cached git status.
func (m model) activateWorkspaceRepo(path string) (tea.Model, tea.Cmd) {
	r := m.repoByPath(path)
	if r.Path == "" {
		m.status = "repository is no longer available · return to dashboard and refresh"
		return m, nil
	}
	m.rememberWorkspaceView()
	m.beginWorkspace(r)
	m.sidebarFocus = false
	m.mode, m.detailRepo, m.detail = modeDetail, r.Path, nil
	m.detailRequest++
	m.status = "loading " + r.Name
	m.detailVP.GotoTop()
	m.layoutWorkspace()
	cmds := []tea.Cmd{detailCmd(r, m.detailRequest), m.spinner.Tick}
	v := m.workspaceViews[path]
	switch v.mode {
	case modeDiff:
		var next tea.Model
		var cmd tea.Cmd
		if v.diffPath != "" {
			next, cmd = m.openFileDiff(r, v.diffPath)
		} else {
			next, cmd = m.openDiff(r)
		}
		m = next.(model)
		cmds = append(cmds, cmd)
	case modeSessions:
		next, cmd := m.openSessions(r)
		m = next.(model)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// One shared footer reserves navigation space even when the sidebar is collapsed.
func (m *model) layoutWorkspace() {
	if !isWorkspaceMode(m.mode) {
		m.detailVP.Width = m.innerWidth()
		m.detailVP.Height = clamp(m.height-7, 3, max(3, m.height))
		return
	}
	if !m.sidebarVisible() {
		m.sidebarFocus = false
	}
	m.detailVP.Width = m.workspaceWidth()
	m.detailVP.Height = max(1, m.height-8)
	if m.mode == modeDetail && (m.status != "" || m.graphBuilding) {
		m.detailVP.Height = max(1, m.height-9)
	}
	v := m.workspaceViews[m.workspacePath()]
	switch m.mode {
	case modeDetail:
		m.setDetailContent()
		if m.detail != nil {
			m.detailVP.SetYOffset(v.detailOffset)
		}
	case modeDiff:
		m.setDiffContent()
		if !m.diffLoading {
			m.detailVP.SetYOffset(v.diffOffset)
		}
	}
}

func (m model) sidebarPage() (start, count int) {
	count = max(1, (m.height-7)/3)
	start = max(0, m.sidebarCursor-count+1)
	return start, count
}

func (m model) sidebarView(height int) string {
	label := " Repositories"
	if m.sidebarFocus {
		label += " • focused"
	}
	rows := []string{fillLine(segB(accent, label), sidebarWidth, bg), hrule(sidebarWidth)}
	start, count := m.sidebarPage()
	for i := start; i < min(start+count, len(m.sidebarPaths)); i++ {
		r := m.repoByPath(m.sidebarPaths[i])
		name, branch := r.Name, r.Branch
		if r.Path == "" {
			name, branch = m.sidebarPaths[i], "unavailable"
		}
		marker, color := "  ", ice
		if m.sidebarPaths[i] == m.workspacePath() {
			marker, color = "▌ ", accent
		}
		if m.sidebarFocus && i == m.sidebarCursor {
			marker, color = "> ", selFg
		}
		state := ""
		if r.Dirty {
			state = " *"
		}
		rows = append(rows, fillLine(segB(color, marker+fit(name, sidebarWidth-2-len(state)))+seg(yellow, state), sidebarWidth, bg))
		rows = append(rows, fillLine(seg(muted, "  "+fit(branch, sidebarWidth-2)), sidebarWidth, bg))
		agents := []string{}
		if !r.CCLast.IsZero() {
			agents = append(agents, "Claude "+relTime(r.CCLast))
		}
		if !r.CodexLast.IsZero() {
			agents = append(agents, "Codex "+relTime(r.CodexLast))
		}
		rows = append(rows, fillLine(seg(muted, "  "+fit(strings.Join(agents, " · "), sidebarWidth-2)), sidebarWidth, bg))
	}
	for len(rows) < height-1 {
		rows = append(rows, fillLine("", sidebarWidth, bg))
	}
	rows = append(rows, fillLine(seg(muted, fmt.Sprintf(" %d–%d / %d repos", min(start+1, len(m.sidebarPaths)), min(start+count, len(m.sidebarPaths)), len(m.sidebarPaths))), sidebarWidth, bg))
	return strings.Join(rows, "\n")
}

func (m model) workspaceView() string {
	// Preview helpers can render without dispatching Update first.
	if len(m.sidebarPaths) == 0 {
		m.beginWorkspace(m.repoByPath(m.workspacePath()))
	}
	offset := m.detailVP.YOffset
	m.layoutWorkspace()
	m.detailVP.SetYOffset(offset)
	width, height := m.workspaceWidth(), max(1, m.height-3)
	var body string
	switch m.mode {
	case modeDetail:
		body = m.detailView(width)
	case modeDiff:
		body = m.diffView(width)
	case modeSessions:
		body = m.workspaceSessionsView(width)
	}
	body = lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Width(width).Height(height).MaxWidth(width).MaxHeight(height).
		Render(body)
	if m.sidebarVisible() {
		divider := strings.TrimSuffix(strings.Repeat(seg(muted, "│ ")+"\n", height), "\n")
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebarView(height), divider, body)
	}
	hints := []string{cmdHint("[ / ]", "repo"), cmdHint("\\", "sidebar")}
	if m.sidebarVisible() {
		hints = append([]string{cmdHint("tab", "focus"), cmdHint("enter", "open")}, hints...)
	}
	if m.sidebarFocus {
		hints = append([]string{cmdHint("↑↓", "select repo")}, hints...)
	}
	return body + "\n" + fillLine(packHints(m.innerWidth(), hints, nil), m.innerWidth(), bg)
}

func (m model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !isWorkspaceMode(m.mode) {
		return m, nil, false
	}
	if len(m.sidebarPaths) == 0 {
		m.beginWorkspace(m.repoByPath(m.workspacePath()))
	}
	switch msg.String() {
	case "\\":
		m.sidebarHidden = !m.sidebarHidden
		m.sidebarFocus = false
		return m, nil, true
	case "tab", "shift+tab":
		if m.sidebarVisible() {
			m.sidebarFocus = !m.sidebarFocus
		}
		return m, nil, true
	case "[", "]":
		delta := 1
		if msg.String() == "[" {
			delta = -1
		}
		m.beginWorkspace(m.repoByPath(m.workspacePath()))
		idx := clamp(m.sidebarCursor+delta, 0, max(0, len(m.sidebarPaths)-1))
		if idx == m.sidebarCursor || len(m.sidebarPaths) == 0 {
			return m, nil, true
		}
		next, cmd := m.activateWorkspaceRepo(m.sidebarPaths[idx])
		return next, cmd, true
	}
	if !m.sidebarFocus {
		return m, nil, false
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit, true
	case "esc", "q":
		m.sidebarFocus = false
	case "up", "k":
		m.sidebarCursor = max(0, m.sidebarCursor-1)
	case "down", "j":
		m.sidebarCursor = min(max(0, len(m.sidebarPaths)-1), m.sidebarCursor+1)
	case "g", "home":
		m.sidebarCursor = 0
	case "G", "end":
		m.sidebarCursor = max(0, len(m.sidebarPaths)-1)
	case "pgup", "pgdown":
		_, count := m.sidebarPage()
		if msg.String() == "pgup" {
			count = -count
		}
		m.sidebarCursor = clamp(m.sidebarCursor+count, 0, max(0, len(m.sidebarPaths)-1))
	case "enter":
		if m.sidebarCursor < len(m.sidebarPaths) {
			path := m.sidebarPaths[m.sidebarCursor]
			if path == m.workspacePath() {
				m.sidebarFocus = false
				return m, nil, true
			}
			next, cmd := m.activateWorkspaceRepo(path)
			return next, cmd, true
		}
	}
	// Repo actions apply only with content focus, avoiding accidental launches.
	return m, nil, true
}

func (m model) handleSidebarMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	if !m.sidebarVisible() {
		return m, nil, false
	}
	if msg.X >= sidebarWidth+2 {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.sidebarFocus = false
			return m, nil, true
		}
		return m, nil, false
	}
	if msg.X < 2 || msg.Y < 1 || msg.Y >= m.height-2 {
		return m, nil, true
	}
	start, count := m.sidebarPage()
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.sidebarFocus = true
		m.sidebarCursor = max(0, m.sidebarCursor-1)
	case tea.MouseButtonWheelDown:
		m.sidebarFocus = true
		m.sidebarCursor = min(max(0, len(m.sidebarPaths)-1), m.sidebarCursor+1)
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress && msg.Y >= 3 && msg.Y < 3+count*3 {
			idx := start + (msg.Y-3)/3
			if idx < len(m.sidebarPaths) {
				path := m.sidebarPaths[idx]
				m.sidebarCursor = idx
				if path == m.workspacePath() {
					m.sidebarFocus = true
					return m, nil, true
				}
				next, cmd := m.activateWorkspaceRepo(path)
				return next, cmd, true
			}
		}
	}
	return m, nil, true
}
