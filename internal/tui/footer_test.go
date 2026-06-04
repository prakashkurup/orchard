package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPackHints(t *testing.T) {
	opts := []string{cmdHint("a", "alpha"), cmdHint("b", "beta"), cmdHint("c", "gamma")}
	tail := []string{cmdHint("?", "help")}

	// Too narrow for any opt: the tail must still be present.
	narrow := packHints(1, opts, tail)
	if !strings.Contains(narrow, "help") {
		t.Error("tail must always be present, even when nothing else fits")
	}
	if strings.Contains(narrow, "alpha") {
		t.Error("no opts should fit at width 1")
	}

	// Plenty of room: everything shows, in order, ending with the tail.
	wide := packHints(1000, opts, tail)
	for _, w := range []string{"alpha", "beta", "gamma", "help"} {
		if !strings.Contains(wide, w) {
			t.Errorf("expected %q at wide width", w)
		}
	}
	if strings.Index(wide, "gamma") > strings.Index(wide, "help") {
		t.Error("tail must come after the packed opts")
	}
}

func TestPackHintsMarksHiddenCommands(t *testing.T) {
	opts := []string{cmdHint("a", "alpha"), cmdHint("b", "beta"), cmdHint("c", "gamma"), cmdHint("d", "delta")}
	tail := []string{cmdHint("?", "help")}

	// Everything fits: no marker, the footer is not lying about coverage.
	if got := packHints(1000, opts, tail); strings.Contains(got, "more") {
		t.Errorf("no +N marker should appear when all opts fit: %q", got)
	}

	// Narrow: some opts drop, so a "+N more" marker appears before the ? help tail.
	narrow := ansiPattern.ReplaceAllString(packHints(40, opts, tail), "")
	if !strings.Contains(narrow, "+") || !strings.Contains(narrow, "more") {
		t.Errorf("expected a +N more marker when opts are dropped, got %q", narrow)
	}
	if !strings.Contains(narrow, "help") {
		t.Errorf("? help must stay visible next to the marker, got %q", narrow)
	}
	if lipgloss.Width(packHints(40, opts, tail)) > 40 {
		t.Error("packed footer with marker must not exceed the width")
	}
}

func TestHelpReturnsToOpener(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.resize()

	// ? from the detail page returns there on close, not to the dashboard.
	m.mode = modeDetail
	mm, _ := m.openHelp()
	m = mm.(model)
	if m.mode != modeHelp || m.returnMode != modeDetail {
		t.Fatalf("openHelp from detail: mode=%v returnMode=%v", m.mode, m.returnMode)
	}
	mm, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.(model).mode != modeDetail {
		t.Fatalf("esc from help should return to detail, got %v", mm.(model).mode)
	}

	// ? from the dashboard returns to the dashboard.
	m = newModel("root", 4)
	m.width, m.height = 120, 30
	m.resize()
	mm, _ = m.openHelp()
	mm, _ = mm.(model).handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.(model).mode != modeList {
		t.Fatalf("esc from help should return to dashboard, got %v", mm.(model).mode)
	}
}
