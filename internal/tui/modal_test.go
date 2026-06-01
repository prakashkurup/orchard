package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/prakashkurup/orchard/internal/editor"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

func modalCases() []struct {
	name  string
	setup func(*model)
} {
	return []struct {
		name  string
		setup func(*model)
	}{
		{"branch", func(m *model) {
			m.mode = modeBranch
			m.branchRepo = "/tmp/alpha"
			m.branchAll = []orchardgit.Branch{
				{Name: "main", Current: true, Rel: "3 days ago"},
				{Name: "some/really-long-unbreakable-branch-name-that-would-overflow-the-modal-box-by-a-lot", Remote: true, Rel: "4 weeks ago"},
				{Name: "feat/x", Remote: true, Rel: "1 day ago"},
			}
		}},
		{"editor", func(m *model) {
			m.mode = modeEditor
			m.editorRepo = "/tmp/alpha"
			m.editorPick = []editor.Editor{
				{ID: "vscode", Name: "VS Code", Cmd: "code"},
				{ID: "nvim", Name: "Neovim", Cmd: "nvim", Terminal: true},
			}
		}},
		{"clone", func(m *model) { m.mode = modeClone }},
		{"commitMsg", func(m *model) {
			m.mode = modeCommitMsg
			m.commitMsgRepo = repo.Repo{Name: "acme-web", Path: "/tmp/acme-web"}
			m.commitMsg = "feat(checkout): apply promo codes to the order summary\n\n" +
				"Wire the promo field into buildCheckoutSummary and format the discounted total in SummaryCard."
		}},
		{"commitMsgLoading", func(m *model) {
			m.mode = modeCommitMsg
			m.commitMsgRepo = repo.Repo{Name: "acme-web", Path: "/tmp/acme-web"}
			m.commitMsgLoading = true
		}},
	}
}

// A modal must render as a clean rectangle that fits the screen - no border line
// wider than the screen, and every border line the same width (regression guard
// for the "gray overhang" where long content pushed the box past its border).
func TestModalsAreCleanRectangles(t *testing.T) {
	for _, w := range []int{80, 140, 200} {
		for _, tc := range modalCases() {
			m := newModel("root", 8)
			m.width, m.height = w, 40
			m.repos = sampleRepos()
			m.resize()
			tc.setup(&m)

			out := ansiPattern.ReplaceAllString(m.View(), "")
			boxWidths := map[int]bool{}
			for _, ln := range strings.Split(out, "\n") {
				if vis := runewidth.StringWidth(ln); vis > w {
					t.Fatalf("%s @%d: line exceeds screen width: %d > %d", tc.name, w, vis, w)
				}
				// isolate the box border span (the modal now floats over the
				// dimmed dashboard, so margins may contain backdrop text).
				rs := []rune(ln)
				first, last := -1, -1
				for i, r := range rs {
					if strings.ContainsRune("╭╮╰╯│", r) {
						if first < 0 {
							first = i
						}
						last = i
					}
				}
				if first >= 0 {
					boxWidths[runewidth.StringWidth(string(rs[first:last+1]))] = true
				}
			}
			if len(boxWidths) != 1 {
				t.Fatalf("%s @%d: box is not a uniform rectangle, border widths = %v", tc.name, w, boxWidths)
			}
		}
	}
}

// A modal must float over the dimmed dashboard, not a dark void: the backdrop
// stays visible (dimmed) in the margins, and there is no flat undimmed band.
func TestModalShowsDimmedBackdrop(t *testing.T) {
	// truecolor profile is forced for the package in TestMain
	m := newModel("root", 8)
	m.width, m.height = 140, 30
	m.repos = sampleRepos()
	m.resize()
	m.mode = modeBranch
	m.branchRepo = "/tmp/alpha"
	m.branchAll = []orchardgit.Branch{{Name: "main", Current: true, Rel: "3 days ago"}}
	// the composited backdrop+box (before the outer app frame is added)
	out := m.overlayModal(m.branchView(m.innerWidth()), m.innerWidth())

	// backdrop content (dashboard) is visible behind the modal
	stripped := ansiPattern.ReplaceAllString(out, "")
	if !strings.Contains(stripped, "ORCHARD") || !strings.Contains(stripped, "REPOS") {
		t.Fatal("dashboard backdrop not visible behind modal (void instead of dimmed context)")
	}
	// the backdrop is dimmed, with no flat undimmed bg band (derive the exact
	// SGR codes from the palette so this survives theme tweaks).
	bgCode := func(c string) string {
		s := lipgloss.NewStyle().Background(lipgloss.Color(c)).Render(" ")
		return regexp.MustCompile(`48;2;\d+;\d+;\d+`).FindString(s)
	}
	if !strings.Contains(out, bgCode(dimBg)) {
		t.Fatal("dimmed backdrop background missing")
	}
	if strings.Contains(out, bgCode(bg)) {
		t.Fatal("undimmed app background leaked into modal backdrop (letterbox band)")
	}
}

// The branch picker aligns name / type / time into fixed columns, so the
// "remote" tags and the "· time" separators line up regardless of name length.
func TestBranchPickerColumnsAlign(t *testing.T) {
	m := newModel("root", 8)
	m.width, m.height = 140, 30
	m.repos = sampleRepos()
	m.resize()
	m.mode = modeBranch
	m.branchRepo = "/tmp/alpha"
	m.branchAll = []orchardgit.Branch{
		{Name: "main", Current: true, Rel: "10 days ago"},
		{Name: "a-very-long-local-feature-branch-name", Rel: "3 weeks ago"},
		{Name: "dep-update", Remote: true, Rel: "6 days ago"},
		{Name: "x/long-feature-branch-name-here", Remote: true, Rel: "4 weeks ago"},
	}
	box := ansiPattern.ReplaceAllString(m.branchView(m.innerWidth()), "")

	// measure visual columns (·, ▌, ● are multibyte, so byte offsets mislead)
	visCol := func(ln, sub string) int { return runewidth.StringWidth(ln[:strings.Index(ln, sub)]) }
	dotCols, remoteCols := map[int]bool{}, map[int]bool{}
	for _, ln := range strings.Split(box, "\n") {
		if strings.Contains(ln, "ago") { // a picker row (rel time), not the title
			dotCols[visCol(ln, " · ")] = true
		}
		if strings.Contains(ln, "remote") {
			remoteCols[visCol(ln, "remote")] = true
		}
	}
	if len(dotCols) != 1 {
		t.Fatalf("time separators not aligned, columns = %v", dotCols)
	}
	if len(remoteCols) != 1 {
		t.Fatalf("remote tags not aligned, columns = %v", remoteCols)
	}
}

// Long branch names must be truncated to fit, never wrapped onto extra lines.
func TestBranchModalTruncatesNoWrap(t *testing.T) {
	m := newModel("root", 8)
	m.width, m.height = 140, 40
	m.repos = sampleRepos()
	m.resize()
	m.mode = modeBranch
	m.branchRepo = "/tmp/alpha"
	m.branchAll = []orchardgit.Branch{
		{Name: "main", Current: true, Rel: "3 days ago"},
		{Name: "team.member/VERY-LONG-2026q1-feature-flag-removal-and-backfill-job-cleanup", Remote: true, Rel: "2 days ago"},
		{Name: "another.person/also-quite-long-branch-name-for-some-migration-work", Remote: true, Rel: "5 weeks ago"},
	}
	out := ansiPattern.ReplaceAllString(m.View(), "")
	remoteRows := strings.Count(out, "remote")
	if remoteRows != 2 {
		t.Fatalf("expected exactly 2 remote rows (no wrapping), got %d", remoteRows)
	}
}
