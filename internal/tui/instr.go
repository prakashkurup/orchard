package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/repo"
)

// instrState is a repo's AI-instructions health: which of CLAUDE.md / AGENTS.md
// exist, and whether CLAUDE.md imports AGENTS.md (Claude Code does not read
// AGENTS.md natively).
type instrState struct {
	hasClaude bool
	hasAgents bool
	imports   bool
}

// needsWiring: has AGENTS.md but Claude Code will not read it.
func (s instrState) needsWiring() bool {
	return s.hasAgents && (!s.hasClaude || !s.imports)
}

// canWire: orchard can safely fix it by creating a new CLAUDE.md (never touches
// an existing one).
func (s instrState) canWire() bool {
	return s.hasAgents && !s.hasClaude
}

// blind: Claude Code reads no project instructions here (no CLAUDE.md).
func (s instrState) blind() bool {
	return !s.hasClaude
}

func detectInstr(path string) instrState {
	var s instrState
	if data, err := os.ReadFile(filepath.Join(path, "CLAUDE.md")); err == nil {
		s.hasClaude = true
		s.imports = strings.Contains(string(data), "@AGENTS.md")
	}
	if _, err := os.Stat(filepath.Join(path, "AGENTS.md")); err == nil {
		s.hasAgents = true
	}
	return s
}

type instrMsg struct{ byPath map[string]instrState }

func instrCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return instrMsg{byPath: demoInstr()} }
	}
	return func() tea.Msg {
		byPath := make(map[string]instrState, len(repos))
		for _, r := range repos {
			byPath[r.Path] = detectInstr(r.Path)
		}
		return instrMsg{byPath: byPath}
	}
}

type wireInstrMsg struct {
	wired   int
	skipped int
	err     string
}

// agentsImport is the CLAUDE.md body that makes Claude Code load AGENTS.md.
const agentsImport = "@AGENTS.md\n"

// wireInstrCmd creates a CLAUDE.md that imports AGENTS.md for each target that
// has AGENTS.md but no CLAUDE.md. It never modifies an existing CLAUDE.md.
func wireInstrCmd(targets []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return wireInstrMsg{wired: len(targets)} }
	}
	return func() tea.Msg {
		var wired, skipped int
		for _, r := range targets {
			if !detectInstr(r.Path).canWire() {
				skipped++
				continue
			}
			if err := os.WriteFile(filepath.Join(r.Path, "CLAUDE.md"), []byte(agentsImport), 0o644); err != nil {
				return wireInstrMsg{wired: wired, skipped: skipped, err: err.Error()}
			}
			wired++
		}
		return wireInstrMsg{wired: wired, skipped: skipped}
	}
}
