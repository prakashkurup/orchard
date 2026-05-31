package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/prakashkurup/orchard/internal/editor"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

func sampleRepos() []repo.Repo {
	return []repo.Repo{
		{Name: "alpha", Path: "/tmp/alpha", Branch: "main", DefaultBranch: "main", Display: repo.DisplayClean, ChangedFiles: 0},
		{Name: "beta", Path: "/tmp/beta", Branch: "feat/x", DefaultBranch: "main", Display: repo.DisplayFeature},
		{Name: "gamma", Path: "/tmp/gamma", Branch: "main", DefaultBranch: "main", Dirty: true, Display: repo.DisplayDirty},
		{Name: "delta", Path: "/tmp/delta", Branch: "main", DefaultBranch: "main", Behind: 3, Display: repo.DisplayBehind},
	}
}

func TestModesRenderWithinWidth(t *testing.T) {
	width, height := 140, 30
	cases := []struct {
		name  string
		setup func(*model)
	}{
		{"list", func(m *model) {}},
		{"grouped", func(m *model) { m.grouped = true; m.rebuildView() }},
		{"filtered", func(m *model) { m.quick = filterAttention; m.rebuildView() }},
		{"sorted-name", func(m *model) { m.sortMode = sortName; m.rebuildView() }},
		{"editor-picker", func(m *model) {
			m.mode = modeEditor
			m.editorRepo = "/tmp/alpha"
			m.editorPick = []editor.Editor{
				{ID: "vscode", Name: "VS Code", Cmd: "code"},
				{ID: "nvim", Name: "Neovim", Cmd: "nvim", Terminal: true},
			}
		}},
		{"detail", func(m *model) {
			m.mode = modeDetail
			m.detailRepo = "/tmp/gamma"
			m.detail = &detailState{repo: m.repos[2], info: orchardgit.DetailInfo{
				StatusLines: []string{" M file.go", "?? new.go"},
				Commits:     []orchardgit.Commit{{Hash: "a1b2c3d", Rel: "2h ago", Subject: "fix things", Author: "you"}},
				Remotes:     []string{"origin  git@github.com:org/gamma.git"},
			}}
			m.setDetailContent()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel("root", 8)
			m.width, m.height = width, height
			m.repos = sampleRepos()
			m.resize()
			tc.setup(&m)
			out := m.View()
			for _, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > width+4 {
					t.Fatalf("%s: line width = %d, want <= %d\n%s", tc.name, got, width+4, line)
				}
			}
		})
	}
}

func TestPullRenderNoEscapeCorruption(t *testing.T) {
	m := newModel("root", 8)
	m.width, m.height = 150, 24
	m.repos = sampleRepos()
	m.loading = true
	m.status = "pulling 1 repos"
	m.pulling = map[string]bool{"/tmp/delta": true}
	m.resize()

	stripped := ansiPattern.ReplaceAllString(m.View(), "")
	for _, frag := range []string{"[38;2", "[0m", "[1m", "\x1b"} {
		if strings.Contains(stripped, frag) {
			t.Fatalf("corrupted escape fragment %q leaked into output (spinner ANSI re-processed)", frag)
		}
	}
	if !strings.Contains(stripped, "pulling 1 repos") {
		t.Fatal("status text garbled during pull")
	}
}

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestRenderedRowsFitViewportWidth(t *testing.T) {
	repos := []repo.Repo{
		{
			Name:          "payments-lifecycle-service",
			Path:          "/tmp/payments-lifecycle-service",
			Branch:        "feature/checkout-redesign-2024",
			DefaultBranch: "main",
			Dirty:         true,
			LastCommit:    "5 hours ago\t[PROJ-1234] very long commit subject to exercise truncation and width handling",
			Display:       repo.DisplayDirty,
		},
	}

	width := 116
	rendered := renderRows(repos, map[string]bool{}, 0, width)
	for _, line := range strings.Split(rendered, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line width = %d, want <= %d\n%s", got, width, line)
		}
	}
	if strings.Contains(rendered, "\t") {
		t.Fatal("rendered row contains a tab")
	}
}
