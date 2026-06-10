package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

// touchedViewSessions is how many recent transcripts the full Files view scans -
// more than the detail-page preview, since it is opened on demand.
const touchedViewSessions = 40

type touchedMsg struct {
	path  string
	files []claude.TouchedFile
	dirty map[string]bool // repo-relative paths with uncommitted changes
}

// openTouched opens the full list of files Claude read or edited in a repo, as a
// focusable modal where each file can be opened in the editor or diffed.
func (m model) openTouched(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	m.touchedRepo = r
	m.touchedFiles = nil
	m.touchedDirty = nil
	m.touchedCursor = 0
	m.touchedLoading = true
	m.touchedReturn = m.mode
	m.mode = modeTouched
	return m, tea.Batch(touchedCmd(r, m.assistantIsCodex()), m.spinner.Tick)
}

func touchedCmd(r repo.Repo, useCodex bool) tea.Cmd {
	if demoMode() {
		return func() tea.Msg {
			return touchedMsg{path: r.Path, files: demoTouched(), dirty: dirtyPathSet(demoDetail(r).StatusLines)}
		}
	}
	return func() tea.Msg {
		files := claude.TouchMap(r.Path, touchedViewSessions)
		if useCodex {
			files = codex.TouchMap(r.Path, touchedViewSessions)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lines, _ := orchardgit.StatusLines(ctx, r.Path)
		return touchedMsg{path: r.Path, files: files, dirty: dirtyPathSet(lines)}
	}
}

func (m model) currentTouched() (claude.TouchedFile, bool) {
	if m.touchedCursor < 0 || m.touchedCursor >= len(m.touchedFiles) {
		return claude.TouchedFile{}, false
	}
	return m.touchedFiles[m.touchedCursor], true
}

func (m model) handleTouchedKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "f":
		m.mode = m.touchedReturn
		if m.mode == modeDetail {
			m.setDetailContent() // a file diff may have left its content in detailVP
		}
		return m, nil
	case "up", "k", "ctrl+p":
		m.touchedCursor = clamp(m.touchedCursor-1, 0, max(0, len(m.touchedFiles)-1))
	case "down", "j", "ctrl+n":
		m.touchedCursor = clamp(m.touchedCursor+1, 0, max(0, len(m.touchedFiles)-1))
	case "g", "home":
		m.touchedCursor = 0
	case "G", "end":
		m.touchedCursor = max(0, len(m.touchedFiles)-1)
	case "enter", "o":
		if t, ok := m.currentTouched(); ok {
			abs := filepath.Join(m.touchedRepo.Path, t.Path)
			if _, err := os.Stat(abs); err != nil {
				m.status = t.Path + " no longer exists on disk"
				return m, nil
			}
			return m.openFileInEditor(abs, t.Path)
		}
	case "d":
		if t, ok := m.currentTouched(); ok {
			return m.openFileDiff(m.touchedRepo, t.Path)
		}
	case "y":
		if t, ok := m.currentTouched(); ok {
			m.status = copyToClipboard(filepath.Join(m.touchedRepo.Path, t.Path), "path")
		}
		return m, nil
	}
	return m, nil
}

// openFileInEditor opens a single file in the resolved editor (at line 1), GUI or
// terminal, mirroring how the search view opens a match.
func (m model) openFileInEditor(absPath, label string) (tea.Model, tea.Cmd) {
	e, ok := m.chosenEditor()
	if !ok {
		m.status = "no editor detected to open file"
		return m, nil
	}
	cmd, terminal := e.CommandAt(absPath, 1)
	if cmd == nil {
		m.status = "cannot open in " + e.Name
		return m, nil
	}
	if terminal {
		return m, tea.ExecProcess(cmd, func(error) tea.Msg {
			return statusMsg{text: "returned from " + e.Name}
		})
	}
	return m, func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "open failed: " + err.Error()}
		}
		go func() { _ = cmd.Wait() }() // reap the child so it doesn't linger as a zombie
		return statusMsg{text: "opened " + label + " in " + e.Name}
	}
}

func (m model) touchedView(width int) string {
	fg := panelFG
	inner := clamp(width-12, 52, 100)
	return modalBox(inner, func(add func(string)) {
		add(fg(claudeC).Bold(true).Render("✦ Files Claude touched") + fg(muted).Render("  · "+m.touchedRepo.Name))
		add("")
		switch {
		case m.touchedLoading:
			add(fg(claudeC).Render("  "+m.spinner.View()) + fg(muted).Render(" scanning recent sessions…"))
		case len(m.touchedFiles) == 0:
			add(fg(muted).Render("  no files recorded · run Claude inside this repo"))
		default:
			edited, uncommitted, countW := 0, 0, 0
			for _, t := range m.touchedFiles {
				if t.Wrote() {
					edited++
					if m.touchedDirty[t.Path] {
						uncommitted++
					}
				}
				if w := lipgloss.Width(touchCountLabel(t.Touches())); w > countW {
					countW = w
				}
			}
			head := fg(muted).Render(fmt.Sprintf("  %d files · %d edited", len(m.touchedFiles), edited))
			if uncommitted > 0 {
				head += fg(yellow).Render(fmt.Sprintf(" · %d uncommitted", uncommitted))
			}
			add(head)
			add("")

			const actionW, ageW, tagW = 5, 4, 11
			pathW := max(10, inner-2-actionW-2-countW-2-ageW-2-tagW)
			const maxRows = 14
			start := 0
			if m.touchedCursor >= maxRows {
				start = m.touchedCursor - maxRows + 1
			}
			end := min(start+maxRows, len(m.touchedFiles))
			for i := start; i < end; i++ {
				t := m.touchedFiles[i]
				cursor, pc := fg(panel).Render("  "), muted
				if i == m.touchedCursor {
					cursor = fg(accent).Bold(true).Render("▌ ")
				}
				action, ac := "read", muted
				if t.Wrote() {
					action, ac, pc = "edit", claudeC, ice
				}
				p := fitLeft(compactTouchedPath(t.Path), pathW)
				p += strings.Repeat(" ", max(0, pathW-lipgloss.Width(p)))
				tag := ""
				if t.Wrote() && m.touchedDirty[t.Path] {
					tag = "uncommitted"
				}
				meta := fmt.Sprintf("  %*s  %*s  ", countW, touchCountLabel(t.Touches()), ageW, relTime(t.Last))
				add(cursor + fg(ac).Render(fmt.Sprintf("%-*s", actionW, action)) +
					fg(pc).Render(p) + fg(muted).Render(meta) + fg(yellow).Render(tag))
			}
			if len(m.touchedFiles) > end {
				add(fg(muted).Render(fmt.Sprintf("  … %d more below", len(m.touchedFiles)-end)))
			}
		}
		add("")
		add(fg(muted).Render("↑↓ move · ⏎ open · d diff · y copy · esc close"))
	})
}
