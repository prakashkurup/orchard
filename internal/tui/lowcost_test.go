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

func TestUpdateBannerVisibleWithStatus(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 130, 30
	m.resize()
	m.status = "scanned 23 repos"
	m.updateTag = "v0.6.2"
	out := ansiPattern.ReplaceAllString(m.headerView(m.innerWidth()), "")
	if !strings.Contains(out, "v0.6.2 available") {
		t.Fatalf("update banner must stay visible even when a status is set\n%s", out)
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

func TestClaudeCellStates(t *testing.T) {
	now := time.Now()
	strip := func(r repo.Repo) string {
		return strings.TrimSpace(ansiPattern.ReplaceAllString(claudeCell(r, 12, bg, false), ""))
	}
	cases := []struct {
		name, want string
		r          repo.Repo
	}{
		{"no sessions", "·", repo.Repo{}},
		{"live + dirty", "!live", repo.Repo{CCSessions: 1, CCLast: now.Add(-10 * time.Second), Dirty: true}},
		{"live + clean", "live", repo.Repo{CCSessions: 1, CCLast: now.Add(-10 * time.Second)}},
		{"recent + dirty", "!", repo.Repo{CCSessions: 1, CCLast: now.Add(-2 * time.Hour), Dirty: true}},
		{"recent + clean", "2h", repo.Repo{CCSessions: 1, CCLast: now.Add(-2 * time.Hour)}},
		{"old + dirty", "1d", repo.Repo{CCSessions: 1, CCLast: now.Add(-30 * time.Hour), Dirty: true}},
		{"future stamp", "live", repo.Repo{CCSessions: 1, CCLast: now.Add(10 * time.Second)}}, // clock skew
	}
	for _, c := range cases {
		if got := strip(c.r); !strings.Contains(got, c.want) {
			t.Errorf("%s: claudeCell = %q, want contains %q", c.name, got, c.want)
		}
	}
	// a live + clean repo must not carry the red bang
	if got := strip(repo.Repo{CCSessions: 1, CCLast: now.Add(-10 * time.Second)}); strings.Contains(got, "!") {
		t.Errorf("live+clean should be '● live' with no bang: %q", got)
	}
	// past the 24h dirty window the bang is dropped
	if got := strip(repo.Repo{CCSessions: 1, CCLast: now.Add(-30 * time.Hour), Dirty: true}); strings.Contains(got, "!") {
		t.Errorf("old dirty repo should not show the bang: %q", got)
	}
}

func TestTouchMapUncommittedFlag(t *testing.T) {
	now := time.Now()
	m := newModel("root", 4)
	m.detailRepo = "/repo"
	m.instructionsByPath = map[string]instrState{"/repo": {}}
	m.detail = &detailState{
		info: orchardgit.DetailInfo{StatusLines: []string{" M src/a.go", " M src/c.go"}},
		touched: []claude.TouchedFile{
			{Path: "src/a.go", Writes: 2, Last: now}, // edited + dirty -> flagged
			{Path: "src/b.go", Writes: 1, Last: now}, // edited, clean   -> not flagged
			{Path: "src/c.go", Reads: 3, Last: now},  // read-only, dirty -> not flagged
		},
	}
	out := ansiPattern.ReplaceAllString(m.detailBody(120), "")
	if !strings.Contains(out, "1 uncommitted") {
		t.Fatalf("summary should report 1 uncommitted (only the edited+dirty file)\n%s", out)
	}
	for _, ln := range strings.Split(out, "\n") {
		touchRow := strings.Contains(ln, "edit ") || strings.Contains(ln, "read ")
		if !touchRow { // skip the working-tree section, which lists the same files
			continue
		}
		switch {
		case strings.Contains(ln, "a.go") && !strings.Contains(ln, "uncommitted"):
			t.Errorf("a.go (edited+dirty) should be flagged: %q", ln)
		case strings.Contains(ln, "b.go") && strings.Contains(ln, "uncommitted"):
			t.Errorf("b.go (clean) must not be flagged: %q", ln)
		case strings.Contains(ln, "c.go") && strings.Contains(ln, "uncommitted"):
			t.Errorf("c.go (read-only) must not be flagged: %q", ln)
		}
	}
}

func TestDetailHealthNudges(t *testing.T) {
	now := time.Now()
	m := newModel("root", 4)
	m.detailRepo = "/repo"
	m.instructionsByPath = map[string]instrState{"/repo": {hasClaude: true, claudeBytes: 41000}}
	m.detail = &detailState{
		sessions:     []claude.Session{{Title: "x", Assistant: 1, Tokens: 100, Modified: now.Add(-time.Hour)}},
		commitsSince: 14,
	}
	out := ansiPattern.ReplaceAllString(m.detailBody(120), "")
	if !strings.Contains(out, "CLAUDE.md is large") {
		t.Errorf("should warn on a large CLAUDE.md\n%s", out)
	}
	if !strings.Contains(out, "commits since it last ran") {
		t.Errorf("should warn on stale context\n%s", out)
	}
	// below the thresholds, neither warning fires
	m.instructionsByPath = map[string]instrState{"/repo": {hasClaude: true, claudeBytes: 1000}}
	m.detail.commitsSince = 3
	out = ansiPattern.ReplaceAllString(m.detailBody(120), "")
	if strings.Contains(out, "is large") || strings.Contains(out, "may be stale") {
		t.Errorf("no warnings expected below thresholds\n%s", out)
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
