package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestRequestClaudeConfirmsOnlyForMultiple(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()

	// single repo opens immediately, no confirmation prompt
	one, _ := m.requestClaude(m.repos[:1])
	if one.(model).mode == modeConfirm {
		t.Fatal("a single repo should not prompt for confirmation")
	}

	// multiple repos enter the confirm modal, defaulting to Yes
	many, _ := m.requestClaude(m.repos[:3])
	got := many.(model)
	if got.mode != modeConfirm {
		t.Fatalf("multiple repos should enter confirm mode, got %v", got.mode)
	}
	if !got.confirmYes {
		t.Fatal("confirmation should default to Yes")
	}
	if len(got.confirmRepos) != 3 {
		t.Fatalf("expected 3 confirm targets, got %d", len(got.confirmRepos))
	}
}

func TestConfirmKeyTogglesAndCancels(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()
	mm, _ := m.requestClaude(m.repos[:2])
	m = mm.(model)

	// arrow toggles the default Yes to No
	mm, _ = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyLeft})
	m = mm.(model)
	if m.confirmYes {
		t.Fatal("left should toggle the selection to No")
	}

	// esc cancels and clears the pending state
	mm, _ = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(model)
	if m.mode != modeList || m.confirmRepos != nil {
		t.Fatal("esc should return to the list and clear confirm state")
	}
}

func TestRequestBrowserConfirmsOnlyForMultiple(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()

	one, _ := m.requestBrowser(m.repos[:1])
	if one.(model).mode == modeConfirm {
		t.Fatal("a single repo should not prompt for browser confirmation")
	}

	many, _ := m.requestBrowser(m.repos[:3])
	got := many.(model)
	if got.mode != modeConfirm {
		t.Fatalf("multiple repos should enter confirm mode, got %v", got.mode)
	}
	if got.confirmKind != confirmBrowser {
		t.Fatal("confirm kind should be browser")
	}
	if !got.confirmYes {
		t.Fatal("browser confirmation should default to Yes")
	}
}

func TestConfirmViewListsRepos(t *testing.T) {
	m := newModel("root", 8)
	m.width, m.height = 120, 30
	m.repos = sampleRepos()
	m.resize()
	mm, _ := m.requestClaude(m.repos[:3])
	m = mm.(model)

	out := ansiPattern.ReplaceAllString(m.confirmView(m.innerWidth()), "")
	for _, r := range m.repos[:3] {
		if !strings.Contains(out, r.Name) {
			t.Fatalf("confirm modal should list repo %q", r.Name)
		}
	}
	if !strings.Contains(out, "Yes") || !strings.Contains(out, "No") {
		t.Fatal("confirm modal should offer Yes / No")
	}
}

func TestConfirmBrowserAcceptPath(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()
	mm, _ := m.requestBrowser(m.repos[:3])
	m = mm.(model)

	// enter with the default Yes dispatches the browser action
	mm, _ = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := mm.(model)
	if got.mode != modeList {
		t.Fatalf("after confirm, mode = %v, want modeList", got.mode)
	}
	if got.confirmRepos != nil {
		t.Fatal("confirmRepos should be cleared after dispatch")
	}
	if got.status != "opening 3 repos in browser" {
		t.Fatalf("status = %q, want \"opening 3 repos in browser\"", got.status)
	}
}

func TestConfirmYKeyDispatchesRegardlessOfToggle(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()
	mm, _ := m.requestBrowser(m.repos[:2])
	m = mm.(model)
	m.confirmYes = false // 'y' confirms even when No is highlighted

	mm, _ = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	got := mm.(model)
	if got.mode != modeList || got.confirmRepos != nil {
		t.Fatal("'y' should dispatch and clear confirm state")
	}
	if got.status != "opening 2 repos in browser" {
		t.Fatalf("status = %q", got.status)
	}
}

func TestConfirmEnterWithNoCancels(t *testing.T) {
	m := newModel("root", 8)
	m.repos = sampleRepos()
	mm, _ := m.requestClaude(m.repos[:2])
	m = mm.(model)
	m.confirmYes = false // user toggled to No

	mm, _ = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := mm.(model)
	if got.mode != modeList || got.status != "cancelled" || got.confirmRepos != nil {
		t.Fatalf("enter+No: mode=%v status=%q, want list/cancelled/cleared", got.mode, got.status)
	}
}

// Cancelling a confirm opened from the detail view must return there, not the
// list (both the esc and the enter+No paths).
func TestConfirmCancelReturnsToOrigin(t *testing.T) {
	mk := func() model {
		m := newModel("root", 4)
		m.mode = modeConfirm
		m.returnMode = modeDetail
		m.confirmRepos = []repo.Repo{{Path: "/x"}}
		return m
	}
	enterNo := mk()
	enterNo.confirmYes = false
	if got, _ := enterNo.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter}); got.(model).mode != modeDetail {
		t.Fatalf("enter+No should return to detail, got %v", got.(model).mode)
	}
	if got, _ := mk().handleConfirmKey(tea.KeyMsg{Type: tea.KeyEsc}); got.(model).mode != modeDetail {
		t.Fatalf("esc should return to detail, got %v", got.(model).mode)
	}
}
