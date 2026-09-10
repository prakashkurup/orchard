package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/repo"
)

func sidebarTestModel(t *testing.T) model {
	t.Helper()
	t.Setenv("ORCHARD_DEMO", "1")
	t.Setenv("ORCHARD_NO_ANIM", "1")
	m := newModel("/orchard-demo", 4)
	m.width, m.height = 140, 24
	m.repos = demoRepos()
	m.loading = false
	m.assistantCmd, m.assistantLabel = "codex", "Codex"
	m.resize()
	return m
}

func sidebarUpdate(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func sidebarKey(m model, key string) model {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	}
	return sidebarUpdate(m, msg)
}

func sidebarLoadDetail(m model) model {
	return sidebarUpdate(m, detailCmd(m.repoByPath(m.detailRepo), m.detailRequest)())
}

func sidebarLoadDiff(m model, text string) model {
	return sidebarUpdate(m, diffMsg{request: m.diffRequest, path: m.diffRepo.Path, text: text})
}

func TestSidebarRestoresRepoScrollAndDashboard(t *testing.T) {
	m := sidebarTestModel(t)
	m.filterText = "a"
	m.syncRows()
	m.cursorToEdge(false)
	m.selected[m.repos[0].Path] = true
	m.syncRows()
	beforeRepo, _ := m.currentRepo()
	beforeCursor, beforeOffset := m.cursor, m.viewport.YOffset
	m = sidebarLoadDetail(sidebarKey(m, "enter"))
	for i := 0; i < 5; i++ {
		m = sidebarKey(m, "j")
	}
	if m.detailVP.YOffset != 5 {
		t.Fatalf("scroll = %d, want 5", m.detailVP.YOffset)
	}
	// Rendering must not consume a keypress or mutate remembered state.
	_ = m.View()
	m = sidebarKey(m, "[")
	if m.detailRepo == beforeRepo.Path {
		t.Fatal("previous repo did not activate")
	}
	m = sidebarLoadDetail(m)
	m = sidebarLoadDetail(sidebarKey(m, "]"))
	if m.detailRepo != beforeRepo.Path || m.detailVP.YOffset != 5 {
		t.Fatalf("return = %s offset %d", m.detailRepo, m.detailVP.YOffset)
	}
	m = sidebarKey(m, "esc")
	if m.mode != modeList || m.cursor != beforeCursor || m.viewport.YOffset != beforeOffset || m.filterText != "a" || !m.selected[m.repos[0].Path] {
		t.Fatalf("dashboard state changed: cursor %d/%d offset %d/%d filter %q", m.cursor, beforeCursor, m.viewport.YOffset, beforeOffset, m.filterText)
	}
}

func TestSidebarFocusAndMouseRouting(t *testing.T) {
	m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
	path := m.detailRepo
	m = sidebarKey(m, "tab")
	m = sidebarKey(m, "j")
	if !m.sidebarFocus || m.detailRepo != path || m.detailVP.YOffset != 0 {
		t.Fatal("sidebar selection scrolled or switched content")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd != nil || next.(model).detailRepo != path {
		t.Fatal("agent command leaked through sidebar focus")
	}
	m = sidebarKey(m, "enter")
	if m.sidebarFocus || m.detailRepo == path {
		t.Fatal("enter should open selected repo and focus content")
	}
	m = sidebarLoadDetail(m)
	beforeOffset := m.detailVP.YOffset
	m = sidebarUpdate(m, tea.MouseMsg{X: 4, Y: 4, Button: tea.MouseButtonWheelDown})
	if !m.sidebarFocus || m.detailVP.YOffset != beforeOffset {
		t.Fatal("sidebar wheel affected the content viewport")
	}
	m = sidebarUpdate(m, tea.MouseMsg{X: 4, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.workspacePath() != m.sidebarPaths[0] {
		t.Fatal("click did not open the first visible repo")
	}
	m = sidebarKey(m, "tab")
	m = sidebarKey(m, "esc")
	if m.mode != modeDetail || m.sidebarFocus {
		t.Fatal("esc with sidebar focus should return to content")
	}
}

func TestSidebarDiffRestoreAndLateResults(t *testing.T) {
	m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
	path := m.detailRepo
	m = sidebarKey(m, "j")
	m = sidebarKey(m, "j")
	m = sidebarKey(m, "d")
	oldRequest := m.diffRequest
	m = sidebarKey(m, "]")
	m = sidebarLoadDetail(m)
	before := m.detailVP.View()
	m = sidebarUpdate(m, diffMsg{path: path, request: oldRequest, text: "WRONG REPO"})
	if m.detailVP.View() != before {
		t.Fatal("late diff overwrote another repo's detail")
	}
	m = sidebarKey(m, "[")
	if m.mode != modeDiff || m.diffRepo.Path != path {
		t.Fatal("did not restore diff view")
	}
	m = sidebarLoadDetail(m)
	if !strings.Contains(ansi.Strip(m.detailVP.View()), "loading diff") {
		t.Fatal("background detail load overwrote active diff")
	}
	m = sidebarUpdate(m, diffMsg{path: path, request: oldRequest, text: "STALE DIFF"})
	if !m.diffLoading {
		t.Fatal("superseded diff request was accepted")
	}
	text := strings.Repeat("+new line\n", 80)
	m = sidebarLoadDiff(m, text)
	for i := 0; i < 4; i++ {
		m = sidebarKey(m, "j")
	}
	m = sidebarKey(m, "]")
	m = sidebarLoadDetail(m)
	m = sidebarKey(m, "[")
	m = sidebarLoadDiff(m, text)
	if m.detailVP.YOffset != 4 {
		t.Fatalf("restored diff offset = %d, want 4", m.detailVP.YOffset)
	}
	m = sidebarLoadDetail(m)
	m = sidebarKey(m, "esc")
	if m.mode != modeDetail || m.detailVP.YOffset != 2 {
		t.Fatalf("diff back did not restore detail scroll: mode %v offset %d", m.mode, m.detailVP.YOffset)
	}
}

func TestSidebarFileDiffScopeAndErrorsSurviveResize(t *testing.T) {
	m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
	next, _ := m.openFileDiff(m.repoByPath(m.detailRepo), "src/file.go")
	m = next.(model)
	m = sidebarUpdate(m, diffMsg{path: m.diffRepo.Path, request: m.diffRequest, err: errors.New("cannot read diff")})
	m = sidebarUpdate(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(ansi.Strip(m.View()), "cannot read diff") {
		t.Fatal("resize lost diff error")
	}
	m = sidebarKey(m, "]")
	m = sidebarKey(m, "[")
	if m.mode != modeDiff || m.diffPath != "src/file.go" || !m.diffLoading {
		t.Fatal("file diff scope was not restored")
	}
}

func TestSidebarRemembersCodexSessionSelection(t *testing.T) {
	m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
	m = sidebarKey(m, "H")
	sessions := []claude.Session{{ID: "a", Title: "first"}, {ID: "b", Title: "second"}, {ID: "c", Title: "third"}}
	m = sidebarUpdate(m, sessionsMsg{path: m.sessionsRepo.Path, request: m.sessionsRequest, sessions: sessions})
	m = sidebarKey(m, "j")
	m = sidebarKey(m, "j")
	oldRequest := m.sessionsRequest
	m = sidebarKey(m, "]")
	m = sidebarKey(m, "[")
	if m.mode != modeSessions {
		t.Fatal("session view was not restored")
	}
	m = sidebarUpdate(m, sessionsMsg{path: m.sessionsRepo.Path, request: oldRequest, sessions: sessions[:1]})
	if !m.sessionsLoading {
		t.Fatal("superseded session request was accepted")
	}
	m = sidebarUpdate(m, sessionsMsg{path: m.sessionsRepo.Path, request: m.sessionsRequest, sessions: sessions})
	if m.sessionCursor != 2 || !strings.Contains(ansi.Strip(m.View()), "Codex sessions") {
		t.Fatal("session selection or Codex label was lost")
	}
}

func TestSidebarIgnoresOldDetailRequestAfterRevisit(t *testing.T) {
	m := sidebarKey(sidebarTestModel(t), "enter")
	path, request := m.detailRepo, m.detailRequest
	m = sidebarKey(m, "]")
	m = sidebarKey(m, "[")
	m = sidebarUpdate(m, detailMsg{path: path, request: request, err: errors.New("old response")})
	if m.detail != nil {
		t.Fatal("old request accepted after revisiting same repo")
	}
	m = sidebarLoadDetail(m)
	if m.detail == nil {
		t.Fatal("current detail request was discarded")
	}
}

func TestSidebarResizeAndRenderBounds(t *testing.T) {
	for _, mode := range []uiMode{modeDetail, modeDiff, modeSessions} {
		for _, width := range []int{60, 80, 105, 106, 140} {
			for _, height := range []int{12, 24, 40} {
				t.Run(fmt.Sprintf("mode%d/%dx%d", mode, width, height), func(t *testing.T) {
					m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
					if mode == modeDiff {
						m = sidebarLoadDiff(sidebarKey(m, "d"), strings.Repeat("+changed\n", 100))
					} else if mode == modeSessions {
						m = sidebarKey(m, "H")
						m = sidebarUpdate(m, sessionsCmd(m.sessionsRepo, true, m.sessionsRequest)())
					}
					m = sidebarUpdate(m, tea.WindowSizeMsg{Width: width, Height: height})
					if m.sidebarVisible() != (width >= 106) {
						t.Fatal("wrong automatic collapse threshold")
					}
					out := m.View()
					if got := lipgloss.Height(out); got > height {
						t.Fatalf("height %d exceeds terminal height %d", got, height)
					}
					for _, line := range strings.Split(out, "\n") {
						if got := lipgloss.Width(line); got > width {
							t.Fatalf("line width %d exceeds %d: %q", got, width, ansi.Strip(line))
						}
					}
					m = sidebarKey(m, "\\")
					m = sidebarUpdate(m, tea.WindowSizeMsg{Width: 160, Height: 30})
					if m.sidebarVisible() {
						t.Fatal("resize overrode manually hidden sidebar")
					}
					m = sidebarKey(m, "\\")
					if !m.sidebarVisible() {
						t.Fatal("sidebar did not reopen")
					}
				})
			}
		}
	}
}

func TestSidebarStablePathsAcrossScans(t *testing.T) {
	m := sidebarKey(sidebarTestModel(t), "enter")
	paths := append([]string(nil), m.sidebarPaths...)
	repos := append([]repo.Repo(nil), m.repos...)
	for i, j := 0, len(repos)-1; i < j; i, j = i+1, j-1 {
		repos[i], repos[j] = repos[j], repos[i]
	}
	m = sidebarUpdate(m, silentScanMsg{repos: repos})
	if !reflect.DeepEqual(paths, m.sidebarPaths) {
		t.Fatal("background scan reordered sidebar")
	}
	// Deleted repos retain a stable placeholder; selecting one never launches work.
	m.repos = []repo.Repo{repos[0]}
	next, cmd := m.activateWorkspaceRepo(paths[0])
	if cmd != nil || !strings.Contains(next.(model).status, "no longer available") {
		t.Fatal("missing repo should produce an actionable message")
	}
}

func TestSharedViewportsPaintEmptySpace(t *testing.T) {
	m := sidebarTestModel(t)
	for name, view := range map[string]string{
		"repo list": m.viewport.View(),
		"detail":    m.detailVP.View(),
		"search":    m.searchVP.View(),
	} {
		if !strings.Contains(view, "48;2;21;22;31") {
			t.Errorf("%s viewport does not paint empty space with Orchard's background", name)
		}
	}
}

func TestLeavingSidebarReflowsFullWidthContent(t *testing.T) {
	m := sidebarLoadDetail(sidebarKey(sidebarTestModel(t), "enter"))
	workspaceWidth := m.detailVP.Width
	m = sidebarKey(m, "?")
	if m.mode != modeHelp || m.detailVP.Width <= workspaceWidth {
		t.Fatalf("help viewport width = %d, want wider than workspace %d", m.detailVP.Width, workspaceWidth)
	}
	for i, line := range strings.Split(m.detailVP.View(), "\n") {
		if got := lipgloss.Width(line); got != m.detailVP.Width {
			t.Fatalf("help line %d width = %d, want %d", i, got, m.detailVP.Width)
		}
	}
}
