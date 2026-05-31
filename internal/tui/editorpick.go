package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/editor"
	"github.com/prakashkurup/orchard/internal/repo"
	"strings"
)

// openEditor opens a repo in the editor. With forcePick (or no saved default),
// it shows the picker; otherwise it launches the remembered default directly.
func (m model) openEditor(r repo.Repo, forcePick bool) (tea.Model, tea.Cmd) {
	avail := editor.Available()
	if len(avail) == 0 {
		m.status = "no editor detected (install code / idea / vim …)"
		return m, nil
	}
	if !forcePick && m.editorID != "" {
		if e, ok := editor.ByID(m.editorID); ok && e.Installed() {
			return m.launchEditor(e, r)
		}
	}
	if len(avail) == 1 && !forcePick {
		return m.launchEditor(avail[0], r)
	}
	m.editorPick = avail
	m.editorCursor = 0
	for i, e := range avail {
		if e.ID == m.editorID {
			m.editorCursor = i
		}
	}
	m.editorRepo = r.Path
	m.mode = modeEditor
	return m, nil
}

func (m model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeList
		return m, nil
	case "up", "k":
		m.editorCursor = clamp(m.editorCursor-1, 0, len(m.editorPick)-1)
	case "down", "j":
		m.editorCursor = clamp(m.editorCursor+1, 0, len(m.editorPick)-1)
	case "enter":
		if m.editorCursor < 0 || m.editorCursor >= len(m.editorPick) {
			m.mode = modeList
			return m, nil
		}
		e := m.editorPick[m.editorCursor]
		m.editorID = e.ID
		_ = editor.SaveDefault(e.ID)
		r := m.repoByPath(m.editorRepo)
		m.mode = modeList
		return m.launchEditor(e, r)
	}
	// number shortcuts 1-9
	if n := int(msg.String()[0]) - '1'; len(msg.String()) == 1 && n >= 0 && n < len(m.editorPick) {
		e := m.editorPick[n]
		m.editorID = e.ID
		_ = editor.SaveDefault(e.ID)
		r := m.repoByPath(m.editorRepo)
		m.mode = modeList
		return m.launchEditor(e, r)
	}
	return m, nil
}

func (m model) launchEditor(e editor.Editor, r repo.Repo) (tea.Model, tea.Cmd) {
	cmd, terminal := e.Command(r.Path)
	if cmd == nil {
		m.status = "cannot launch " + e.Name
		return m, nil
	}
	if terminal {
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return statusMsg{text: e.Name + " exited: " + err.Error()}
			}
			return statusMsg{text: "returned from " + e.Name + " · " + r.Name}
		})
	}
	name := r.Name
	return m, func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "failed to open " + e.Name + ": " + err.Error()}
		}
		go func() { _ = cmd.Wait() }() // reap the child so it doesn't linger as a zombie
		return statusMsg{text: "opened " + name + " in " + e.Name + "  ·  press E to change editor"}
	}
}

func (m model) editorView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 44, 58)
	_, base := splitDirBase(strings.TrimRight(m.editorRepo, "/"))

	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render(iconLogo + "  Open " + base + " in…"))
		add("")
		for i, e := range m.editorPick {
			cursor := fg(panel).Render("  ")
			nameStyle := fg(ice)
			if i == m.editorCursor {
				cursor = fg(accent).Bold(true).Render("▌ ")
				nameStyle = fg(accent).Bold(true)
			}
			num := fg(blue).Render(fmt.Sprintf("%d ", i+1))
			kind := "GUI"
			if e.Terminal {
				kind = "terminal"
			}
			meta := fg(muted).Render("  " + fit(e.Cmd, 8) + "  " + kind)
			tag := ""
			if e.ID == m.editorID {
				tag = fg(green).Render("  ● default")
			}
			add(cursor + num + nameStyle.Render(fit(e.Name, 16)) + meta + tag)
		}
		add("")
		add(fg(muted).Render("↑↓ move · 1-9 jump · ⏎ open · esc cancel"))
	})
}
