package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestAvailableDocs(t *testing.T) {
	dir := t.TempDir()
	// AGENTS.md and README.md exist, CLAUDE.md does not
	for _, n := range []string{"AGENTS.md", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("# x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := availableDocs(dir); !reflect.DeepEqual(got, []string{"AGENTS.md", "README.md"}) {
		t.Fatalf("availableDocs = %v, want [AGENTS.md README.md] in priority order", got)
	}
	if got := availableDocs(filepath.Join(dir, "nope")); len(got) != 0 {
		t.Errorf("a repo with no docs should yield none, got %v", got)
	}
}

func TestRenderMarkdownWidthSafe(t *testing.T) {
	md := "# Heading\n\nA long paragraph " + strings.Repeat("word ", 80) +
		"\n\n- item one\n- item two\n\n```go\nfmt.Println(\"a very long line of code that exceeds the wrap width by a lot indeed\")\n```\n"
	for _, w := range []int{60, 100, 140} {
		for _, ln := range strings.Split(renderMarkdown(md, w), "\n") {
			if got := lipgloss.Width(ln); got != w {
				t.Fatalf("width %d: a line is %d wide (banding/overflow): %q", w, got, ln)
			}
		}
	}
}

func TestPreviewFlow(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.resize()
	m.mode = modeDetail

	mm, _ := m.openPreview(repo.Repo{Name: "acme-web", Path: "/x/acme-web"})
	m = mm.(model)
	if m.mode != modePreview {
		t.Fatalf("openPreview -> modePreview, got %v", m.mode)
	}
	if m.returnMode != modeDetail {
		t.Fatalf("returnMode should be detail, got %v", m.returnMode)
	}
	if len(m.previewDocs) < 2 {
		t.Fatalf("demo should expose CLAUDE.md + README.md, got %v", m.previewDocs)
	}
	if m.previewIdx != 0 || m.previewBytes == 0 {
		t.Fatalf("should start on the first doc with its size set, idx=%d bytes=%d", m.previewIdx, m.previewBytes)
	}

	// tab cycles to the next doc
	mm, _ = m.handlePreviewKey(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(model)
	if m.previewIdx != 1 {
		t.Fatalf("tab should advance to doc 1, got %d", m.previewIdx)
	}

	// esc returns to the caller
	mm, _ = m.handlePreviewKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(model)
	if m.mode != modeDetail {
		t.Fatalf("esc -> returnMode (detail), got %v", m.mode)
	}
}

func TestPreviewShowsTokenEstimate(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.resize()
	mm, _ := m.openPreview(repo.Repo{Name: "acme-web", Path: "/x/acme-web"})
	m = mm.(model)

	out := ansiPattern.ReplaceAllString(m.previewView(m.innerWidth()), "")
	if !strings.Contains(out, "tokens/session") {
		t.Fatalf("CLAUDE.md preview should show an est. tokens/session readout\n%s", out)
	}
	// a large doc surfaces a KB size warning
	m.previewBytes = 41000
	out = ansiPattern.ReplaceAllString(m.previewView(m.innerWidth()), "")
	if !strings.Contains(out, "41KB") {
		t.Fatalf("a large doc should show its size; got\n%s", out)
	}
}
