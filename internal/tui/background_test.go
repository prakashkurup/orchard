package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// TestEveryModeAvoidsLegacyGrayBackgrounds renders every TUI screen with
// representative content. It guards both sources of the large gray blocks that
// can otherwise slip into blank space: the former modal panel and Glamour's
// default document canvas.
func TestEveryModeAvoidsLegacyGrayBackgrounds(t *testing.T) {
	base := sidebarTestModel(t)
	base.height = 40
	base.resize()
	detail := sidebarLoadDetail(sidebarKey(base, "enter"))

	views := []struct {
		name  string
		model model
	}{
		{"dashboard", base},
		{"detail", detail},
	}

	add := func(name string, m model) {
		m.height = 40
		m.resize()
		views = append(views, struct {
			name  string
			model model
		}{name, m})
	}

	m := base
	m.mode = modeEditor
	m.editorRepo = m.repos[0].Path
	add("editor", m)

	m = base
	m.mode = modeSearch
	m.searchQuery = "cache"
	m.searchResults = demoSearch(m.searchQuery)
	m.flattenSearch()
	m.setSearchContent()
	add("search", m)

	m = base
	m.mode = modeBranch
	m.branchRepo = m.repos[0].Path
	m.branchAll = demoBranches()
	add("branch", m)

	m = base
	m.mode = modeHelp
	m.detailVP.SetContent(m.helpBody(m.detailVP.Width))
	add("help", m)

	m = base
	m.mode = modeWorklog
	m.worklogWindow = "1 day ago"
	m.detailVP.SetContent(m.worklogBody(m.detailVP.Width, worklogMsg{window: m.worklogWindow}))
	add("worklog", m)

	m = base
	m.mode = modeClone
	add("clone", m)

	m = base
	m.mode = modeConfirm
	m.confirmKind = confirmClaude
	m.confirmRepos = m.repos[:2]
	m.confirmYes = true
	add("confirm", m)

	m = detail
	m.mode = modeSessions
	m.sessionsRepo = m.repoByPath(m.detailRepo)
	m.sessions = demoCodexSessions()
	add("sessions", m)

	m = detail
	m.mode = modeDiff
	m.diffRepo = m.repoByPath(m.detailRepo)
	m.diffText = demoDiff()
	m.setDiffContent()
	add("diff", m)

	m = base
	m.mode = modeStats
	m.statsHarvest, m.statsClaude, m.statsCodex = demoStatsData()
	m.detailVP.SetContent(m.statsBody(m.detailVP.Width))
	add("stats", m)

	m = base
	m.mode = modeCommitMsg
	m.commitMsgRepo = m.repos[0]
	m.commitMsg = demoCommitMsg()
	add("commit message", m)

	m = base
	m.mode = modeSessionSearch
	m.sessionSearchQuery = "checkout"
	m.sessionSearchResults = demoSessionHits(m.sessionSearchQuery)
	add("session search", m)

	m = base
	m.mode = modePresets
	m.presets = map[string][]string{"daily": {m.repos[0].Path, m.repos[1].Path}}
	add("presets", m)

	m = detail
	m.mode = modeTouched
	m.touchedRepo = m.repoByPath(m.detailRepo)
	m.touchedFiles = demoTouched()
	m.touchedDirty = dirtyPathSet(demoDetail(m.touchedRepo).StatusLines)
	add("files", m)

	m = detail
	m.mode = modePreview
	m.previewRepo = m.repoByPath(m.detailRepo)
	m.previewDocs = []string{"README.md"}
	m.setPreviewContent()
	add("docs", m)

	m = base
	m.mode = modeCodeburn
	payload := demoCodeburn(m.codeburnPeriod)
	m.codeburnPayload = &payload
	m.codeburnPayloadPeriod = m.codeburnPeriod
	m.setCodeburnContent()
	add("codeburn", m)

	m = base
	m.mode = modeAgentLaunch
	m.agentChoices = []assistantChoice{{cmd: "claude", label: "Claude Code"}, {cmd: "codex", label: "Codex"}}
	m.agentTargets = m.repos[:1]
	add("agent launch", m)

	legacy := map[string]string{
		"old modal panel":     "#20243A",
		"Glamour Tokyo Night": "#1A1B26",
	}
	for _, tc := range views {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.model.View()
			if offset, cell, ok := firstUnpaintedCell(out); ok {
				lo, hi := max(0, offset-80), min(len(out), offset+80)
				t.Fatalf("printable cell %q at byte %d has no background near %q", cell, offset, out[lo:hi])
			}
			for source, color := range legacy {
				if strings.Contains(out, backgroundCode(color)) {
					t.Fatalf("%s background %s leaked into rendered screen", source, color)
				}
			}
		})
	}
	const uiModeCount = int(modeAgentLaunch) + 1
	if len(views) != uiModeCount {
		t.Fatalf("background audit covers %d of %d modes; add a representative render for the new mode", len(views), uiModeCount)
	}
}

func backgroundCode(color string) string {
	rendered := lipgloss.NewStyle().Background(lipgloss.Color(color)).Render(" ")
	return regexp.MustCompile(`48;2;\d+;\d+;\d+`).FindString(rendered)
}

// firstUnpaintedCell follows SGR state through a rendered frame and reports the
// first terminal cell emitted while the default background is active. A default
// background becomes visible as a rectangular gap in terminals whose canvas
// color differs from Orchard's.
func firstUnpaintedCell(s string) (int, rune, bool) {
	painted := false
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			next, sgr, ok := escapeSequence(s, i)
			if ok {
				if sgr != "" {
					painted = sgrBackground(painted, sgr)
				}
				i = next
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != '\n' && r != '\r' && r != '\t' && runewidth.RuneWidth(r) > 0 && !painted {
			return i, r, true
		}
		i += size
	}
	return 0, 0, false
}

func escapeSequence(s string, start int) (next int, sgr string, ok bool) {
	if start+1 >= len(s) {
		return start + 1, "", false
	}
	switch s[start+1] {
	case '[': // CSI: the final byte is in the range 0x40..0x7e.
		for i := start + 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				if s[i] == 'm' {
					return i + 1, s[start+2 : i], true
				}
				return i + 1, "", true
			}
		}
	case ']': // OSC: terminated by BEL or ST (ESC backslash).
		for i := start + 2; i < len(s); i++ {
			if s[i] == '\a' {
				return i + 1, "", true
			}
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, "", true
			}
		}
	}
	return start + 1, "", false
}

func sgrBackground(current bool, sgr string) bool {
	if sgr == "" {
		return false
	}
	parts := strings.Split(sgr, ";")
	for i := 0; i < len(parts); i++ {
		p, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		switch {
		case p == 0 || p == 49:
			current = false
		case p >= 40 && p <= 47, p >= 100 && p <= 107:
			current = true
		case p == 48:
			current = true
		}
	}
	return current
}
