package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestParseAssistantOutput(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"result":"feat: add thing"}`, "feat: add thing"},
		{`{"type":"result","result":"  feat: trimmed  "}`, "feat: trimmed"},
		{"{\"result\":\"```\\nfeat: fenced\\n```\"}", "feat: fenced"},
		{"not json at all", "not json at all"}, // fallback cleans raw text
	}
	for _, c := range cases {
		if got := parseAssistantOutput(c.in); got != c.want {
			t.Errorf("parseAssistantOutput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCommitMessagePromptRequestsCompactFormat(t *testing.T) {
	for _, prompt := range []string{commitMsgPromptHeadless, commitMsgPrompt} {
		for _, want := range []string{"under 72 characters", "2-4 concise bullet points", "no paragraph body"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("commit prompt missing %q:\n%s", want, prompt)
			}
		}
	}
}

func TestClickToRow(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 120, 40
	m.resize()
	m.repos = []repo.Repo{{Path: "/a", Name: "a"}, {Path: "/b", Name: "b"}}
	m.view = []viewItem{{header: true}, {repoIdx: 0}, {repoIdx: 1}}
	m.viewport.SetYOffset(0)

	// in mouse coords the grid starts at line 6 after the app's top padding:
	// header(group) at 6, repos at 7 and 8.
	m.cursor = 1
	m.clickToRow(6) // a group header -> ignored
	if m.cursor != 1 || len(m.selected) != 0 {
		t.Errorf("clicking a header should not move or select, cursor=%d selected=%v", m.cursor, m.selected)
	}
	m.clickToRow(8) // second repo row -> focus + select
	if m.cursor != 2 || !m.selected["/b"] {
		t.Errorf("click at y=8 -> cursor 2 + /b selected, got cursor=%d selected=%v", m.cursor, m.selected)
	}
	m.clickToRow(8) // click again -> deselect
	if m.selected["/b"] {
		t.Error("second click should deselect /b")
	}
	m.clickToRow(99) // below the list -> ignored
	if m.cursor != 2 {
		t.Errorf("out-of-range click should not move the cursor, got %d", m.cursor)
	}
}

func TestClickToRowIgnoresFlatGridHeader(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 120, 40
	m.resize()
	m.repos = []repo.Repo{{Path: "/a", Name: "a"}, {Path: "/b", Name: "b"}}
	m.view = []viewItem{{repoIdx: 0}, {repoIdx: 1}}
	m.viewport.SetYOffset(0)

	m.clickToRow(5) // grid column header, before the first repo row
	if m.cursor != 0 || len(m.selected) != 0 {
		t.Fatalf("grid header click should be ignored, cursor=%d selected=%v", m.cursor, m.selected)
	}

	m.clickToRow(6)
	if m.cursor != 0 || !m.selected["/a"] {
		t.Fatalf("first repo click failed, cursor=%d selected=%v", m.cursor, m.selected)
	}
}

func TestPresetsSaveLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	want := map[string][]string{"payments": {"/a/acme", "/a/payments"}}
	if err := savePresets(want); err != nil {
		t.Fatalf("savePresets() error = %v", err)
	}
	got := loadPresets()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch: got %v, want %v", got, want)
	}
	if names := sortedPresetNames(map[string][]string{"b": nil, "a": nil}); names[0] != "a" || names[1] != "b" {
		t.Errorf("sortedPresetNames not sorted: %v", names)
	}
}

func TestPresetsSaveReportsFilesystemError(t *testing.T) {
	tmp := t.TempDir()
	homeFile := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(homeFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("XDG_CONFIG_HOME", homeFile)

	if err := savePresets(map[string][]string{"x": {"/repo"}}); err == nil {
		t.Fatal("savePresets() error = nil, want filesystem error")
	}
}

func TestClaudePanelRowsDoNotWrap(t *testing.T) {
	m := newModel("root", 4)
	m.claudeUsage = &claude.Usage{
		TotalSessions: 12,
		TotalTurns:    345,
		TotalTokens:   987654,
		ReposUsed:     4,
		Last:          time.Now().Add(-2 * time.Hour),
		Models: map[string]int{
			"claude-sonnet-4.5": 220,
			"claude-opus-4.5":   120,
			"claude-haiku-4.5":  90,
			"claude-extra":      30,
		},
		Repos: []claude.RepoUsage{
			{Name: "payments-api-with-a-long-name", Turns: 200},
			{Name: "identity-platform-service", Turns: 120},
			{Name: "merchant-dashboard", Turns: 90},
			{Name: "warehouse-tools", Turns: 40},
		},
	}

	const width = 120
	out := m.claudePanel(width)
	for _, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("panel line width = %d, want <= %d\n%s", got, width, line)
		}
	}
}

func TestDetailLoadingFillsWidth(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 100, 20
	m.resize()
	m.detailRepo = "/x"
	m.detail = nil // loading state
	first := strings.SplitN(m.detailBody(m.detailVP.Width), "\n", 2)[0]
	if got := lipgloss.Width(first); got != m.detailVP.Width {
		t.Fatalf("loading line must fill the full width (no gray band): got %d want %d", got, m.detailVP.Width)
	}
	if !strings.Contains(ansiPattern.ReplaceAllString(first, ""), "loading") {
		t.Fatalf("loading line should mention loading")
	}
}

func TestDetailClaudeTouchMapReadable(t *testing.T) {
	m := newModel("root", 4)
	now := time.Now()
	m.detailRepo = "/repo"
	m.instructionsByPath = map[string]instrState{
		"/repo": {},
	}
	m.detail = &detailState{
		sessions: []claude.Session{{Title: "Add context passing", Assistant: 2, Tokens: 1000, Modified: now.Add(-24 * time.Hour)}},
		touched: []claude.TouchedFile{{
			Path:  "app/src/main/kotlin/com/upstart/borrowerlifecycle/constants/IterableEvents.kt",
			Reads: 1,
			Last:  now.Add(-24 * time.Hour),
		}},
	}

	out := ansiPattern.ReplaceAllString(m.detailBody(120), "")
	for _, want := range []string{
		"none · no CLAUDE.md or AGENTS.md", // context label value
		"1 touched",                        // files summary
		"0 edited",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail body missing %q\n%s", want, out)
		}
	}
	// the touch row reads as a labeled table line: read action, compacted path,
	// touch count, and age, all on one line.
	var rowFound bool
	for _, ln := range strings.Split(out, "\n") {
		if !strings.Contains(ln, "…/constants/IterableEvents.kt") {
			continue
		}
		rowFound = true
		for _, part := range []string{"read", "1 touch", "1d"} {
			if !strings.Contains(ln, part) {
				t.Fatalf("touch row missing %q: %q", part, ln)
			}
		}
	}
	if !rowFound {
		t.Fatalf("touch map row missing\n%s", out)
	}
	if strings.Contains(out, "app/src/main/kotlin/com/upstart") {
		t.Fatalf("touch map should compact long source roots\n%s", out)
	}
}

func TestDetailSectionIndentation(t *testing.T) {
	m := newModel("root", 4)
	now := time.Now()
	m.detailRepo = "/repo"
	m.instructionsByPath = map[string]instrState{
		"/repo": {hasClaude: true, hasAgents: true, imports: true},
	}
	m.detail = &detailState{
		langs:    []lang.Stat{{Name: "Kotlin", Icon: "K", Color: accent, Pct: 66}},
		sessions: []claude.Session{{Title: "Design event flow", Assistant: 3, Tokens: 1000, Modified: now.Add(-2 * time.Hour)}},
		info: orchardgit.DetailInfo{Graph: []orchardgit.GraphRow{{
			Rail: "*", IsCommit: true, Hash: "fab9b60", Rel: "3 days ago", Author: "Prakash", Subject: "Wire email infra",
		}}},
	}

	out := ansiPattern.ReplaceAllString(m.detailBody(140), "")
	for _, want := range []string{
		"    Languages",
		"    K Kotlin",
		"    Claude Code",
		"    activity",
		"    Commit graph",
		"    ● fab9b60",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail body missing aligned line %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "  ● fab9b60") && !strings.Contains(out, "    ● fab9b60") {
		t.Fatalf("commit graph row is under-indented\n%s", out)
	}
}
