package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/prakashkurup/orchard/internal/repo"
)

// A preset is a named set of repo paths you can launch a cross-repo Claude
// session (A) against in one keystroke. Stored as name -> []path.

func presetsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "orchard", "presets.json")
}

func loadPresets() map[string][]string {
	out := map[string][]string{}
	if p := presetsPath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &out)
		}
	}
	return out
}

func savePresets(m map[string][]string) error {
	p := presetsPath()
	if p == "" {
		return fmt.Errorf("config path unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func sortedPresetNames(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (m model) openPresets() (tea.Model, tea.Cmd) {
	m.presets = loadPresets()
	m.presetNaming = false
	m.presetCursor = 0
	m.returnMode = m.mode
	m.mode = modePresets
	return m, nil
}

func (m model) handlePresetsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.presetNaming {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.presetNaming = false
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.presetInput.Value())
			targets := m.selectionTargets()
			if name != "" && len(targets) > 0 {
				paths := make([]string, 0, len(targets))
				for _, r := range targets {
					paths = append(paths, r.Path)
				}
				m.presets[name] = paths
				if err := savePresets(m.presets); err != nil {
					m.status = "preset save failed: " + firstLine(err.Error())
				} else {
					m.status = fmt.Sprintf("saved preset %q (%d repos)", name, len(paths))
				}
			}
			m.presetNaming = false
			return m, nil
		default:
			var cmd tea.Cmd
			m.presetInput, cmd = m.presetInput.Update(msg)
			return m, cmd
		}
	}

	names := sortedPresetNames(m.presets)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "W":
		m.mode = m.returnMode
		return m, nil
	case "up", "k", "ctrl+p":
		if m.presetCursor > 0 {
			m.presetCursor--
		}
	case "down", "j", "ctrl+n":
		if m.presetCursor < len(names)-1 {
			m.presetCursor++
		}
	case "s":
		if len(m.selectionTargets()) == 0 {
			m.status = "select repos first (space), then s to save a preset"
			return m, nil
		}
		m.presetNaming = true
		m.presetInput.SetValue("")
		return m, tea.Batch(textinput.Blink, m.presetInput.Focus())
	case "d":
		if len(names) > 0 && m.presetCursor < len(names) {
			name := names[m.presetCursor]
			delete(m.presets, name)
			if err := savePresets(m.presets); err != nil {
				m.status = "preset delete failed: " + firstLine(err.Error())
			} else {
				m.status = "deleted preset " + name
			}
			if m.presetCursor > 0 {
				m.presetCursor--
			}
		}
	case "enter":
		if len(names) == 0 {
			return m, nil
		}
		var targets []repo.Repo
		for _, p := range m.presets[names[m.presetCursor]] {
			if r := m.repoByPath(p); r.Path != "" {
				targets = append(targets, r)
			}
		}
		m.mode = m.returnMode
		if len(targets) == 0 {
			m.status = "preset has no repos in the current root"
			return m, nil
		}
		return m.openClaudeCombined(targets)
	}
	return m, nil
}

func (m model) presetsView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 44, 80)
	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render("✦ Workspace presets"))
		add("")
		if m.presetNaming {
			add(fg(ice).Render(fmt.Sprintf("  name for these %d repos:", len(m.selectionTargets()))))
			add("  " + m.presetInput.View())
			add("")
			add(fg(muted).Render("enter save · esc cancel"))
			return
		}
		names := sortedPresetNames(m.presets)
		if len(names) == 0 {
			add(fg(muted).Render("  no presets yet"))
		}
		for i, n := range names {
			cursor, nameC := fg(muted).Render("  "), fg(ice)
			if i == m.presetCursor {
				cursor, nameC = fg(accent).Bold(true).Render("▸ "), fg(accent).Bold(true)
			}
			add(cursor + nameC.Render(fit(n, inner-16)) + fg(muted).Render(fmt.Sprintf("   %d repos", len(m.presets[n]))))
		}
		add("")
		if n := len(m.selectionTargets()); n > 0 {
			add(fg(green).Render(fmt.Sprintf("  s  save current selection (%d repos)", n)))
		}
		add(fg(muted).Render("  ↑↓ move · enter launch · d delete · esc close"))
	})
}
