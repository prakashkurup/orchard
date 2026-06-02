package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestTouchedFlow(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	m := newModel("root", 4)
	m.width, m.height = 120, 40
	m.resize()
	r := repo.Repo{Name: "acme-web", Path: "/x/acme-web"}
	m.mode = modeDetail

	// open: switches to the loading touched view, remembers the caller
	mm, _ := m.openTouched(r)
	m = mm.(model)
	if m.mode != modeTouched || !m.touchedLoading {
		t.Fatalf("openTouched -> modeTouched loading; got mode=%v loading=%v", m.mode, m.touchedLoading)
	}
	if m.touchedReturn != modeDetail {
		t.Fatalf("touchedReturn should be modeDetail, got %v", m.touchedReturn)
	}

	// the load command (demo) populates the files
	mm, _ = m.Update(touchedCmd(r)())
	m = mm.(model)
	if m.touchedLoading || len(m.touchedFiles) == 0 {
		t.Fatalf("after touchedMsg expected loaded files; loading=%v n=%d", m.touchedLoading, len(m.touchedFiles))
	}
	if !m.touchedFiles[0].Wrote() {
		t.Fatalf("files should be edited-first, got read-only at [0]: %+v", m.touchedFiles[0])
	}

	// navigation
	mm, _ = m.handleTouchedKey(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(model)
	if m.touchedCursor != 1 {
		t.Fatalf("down -> cursor 1, got %d", m.touchedCursor)
	}

	// 'd' opens a single-file diff that returns to the touched view
	mm, _ = m.handleTouchedKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = mm.(model)
	if m.mode != modeDiff || m.diffPath == "" {
		t.Fatalf("d -> single-file diff; got mode=%v diffPath=%q", m.mode, m.diffPath)
	}
	if m.returnMode != modeTouched {
		t.Fatalf("diff opened from touched should return to it, got %v", m.returnMode)
	}

	// 'enter' on a file that does not exist on disk reports it, stays put
	m.mode = modeTouched
	mm, _ = m.handleTouchedKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)
	if m.mode != modeTouched {
		t.Fatalf("opening a missing file should not change mode, got %v", m.mode)
	}
	if m.status == "" {
		t.Fatal("opening a missing file should set a status message")
	}

	// esc returns to the caller
	mm, _ = m.handleTouchedKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(model)
	if m.mode != modeDetail {
		t.Fatalf("esc -> returnMode (detail), got %v", m.mode)
	}
}
