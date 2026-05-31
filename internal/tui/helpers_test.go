package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestRelTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "never"},
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-2 * 24 * time.Hour), "2d"},
		{now.Add(-2 * 7 * 24 * time.Hour), "2w"},
		{now.Add(-60 * 24 * time.Hour), "2mo"},
	}
	for _, c := range cases {
		if got := relTime(c.t); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestRelTimeSince(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{time.Hour - time.Second, "59m"},
		{time.Hour, "1h"},
		{25 * time.Hour, "1d"},
		{8 * 24 * time.Hour, "1w"},
		{40 * 24 * time.Hour, "1mo"},
	}
	for _, c := range cases {
		if got := relTimeSince(c.d); got != c.want {
			t.Errorf("relTimeSince(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFreshnessColor(t *testing.T) {
	if got := freshnessColor(time.Time{}); got != muted {
		t.Errorf("freshnessColor(zero) = %s, want muted", got)
	}
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, green},
		{5 * time.Hour, teal},
		{3 * 24 * time.Hour, blue},
		{10 * 24 * time.Hour, yellow},
		{60 * 24 * time.Hour, orange},
	}
	for _, c := range cases {
		if got := freshnessSince(c.d); got != c.want {
			t.Errorf("freshnessSince(%v) = %s, want %s", c.d, got, c.want)
		}
	}
}

func TestCommitAgeColor(t *testing.T) {
	cases := map[string]string{
		"now":           green,
		"5 minutes ago": green,
		"2 hours ago":   teal,
		"3 days ago":    blue,
		"2 weeks ago":   yellow,
		"3 months ago":  orange,
		"1 year ago":    orange,
		"":              muted,
	}
	for in, want := range cases {
		if got := commitAgeColor(in); got != want {
			t.Errorf("commitAgeColor(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	cases := map[string]string{
		"1 day ago":    "last 24h",
		"7 days ago":   "last 7 days",
		"30 days ago":  "last 30 days",
		"99 weeks ago": "99 weeks ago",
	}
	for in, want := range cases {
		if got := windowLabel(in); got != want {
			t.Errorf("windowLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitState(t *testing.T) {
	join := func(ts []stateToken) string {
		parts := make([]string, len(ts))
		for i, tk := range ts {
			parts[i] = tk.text
		}
		return strings.Join(parts, " ")
	}
	cases := []struct {
		r    repo.Repo
		want string
	}{
		{repo.Repo{}, ""},                                                // clean + synced
		{repo.Repo{ChangedFiles: 3}, "●3"},                               // working changes
		{repo.Repo{Stashes: 2}, "≡2"},                                    // stashes only
		{repo.Repo{Ahead: 3}, "↑3"},                                      // ahead
		{repo.Repo{Behind: 5}, "↓5"},                                     // behind
		{repo.Repo{Ahead: 1, Behind: 2}, "↑1 ↓2"},                        // diverged
		{repo.Repo{Ahead: 2, ChangedFiles: 24, Stashes: 1}, "↑2 ●24 ≡1"}, // mixed
	}
	for _, c := range cases {
		if got := join(gitState(c.r)); got != c.want {
			t.Errorf("gitState(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
	// a clean repo renders a single dim dot, sized to width
	if got := ansi.Strip(gitStateCell(repo.Repo{}, 14, bg, false)); strings.TrimSpace(got) != "·" {
		t.Errorf("clean gitStateCell = %q, want a single dot", strings.TrimSpace(got))
	}
}

func TestGroupWorktree(t *testing.T) {
	lines := []string{
		" M src/a.go",
		"?? new.txt",
		" D gone.go",
		"R  old.go -> new.go",
		"M  staged.go",
	}
	groups := groupWorktree(lines)
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.label] = len(g.files)
	}
	if counts["Modified"] < 1 {
		t.Errorf("expected Modified group, got %+v", counts)
	}
	if counts["New"] != 1 {
		t.Errorf("New count = %d, want 1", counts["New"])
	}
	if counts["Deleted"] != 1 {
		t.Errorf("Deleted count = %d, want 1", counts["Deleted"])
	}
	if counts["Renamed"] != 1 {
		t.Errorf("Renamed count = %d, want 1", counts["Renamed"])
	}
}

// gridLayout columns + gaps must sum to the width, so rows never overflow/wrap.
func TestGridLayoutSumsToWidth(t *testing.T) {
	for _, w := range []int{100, 120, 150, 200, 260} {
		c := gridLayout(w)
		const gaps = 18 // 10 columns, 9 gaps of 2
		sum := c.sel + c.st + c.name + c.branch + c.lang + c.changes + c.synced + c.claude + c.activity + c.info + gaps
		if sum != w {
			t.Errorf("gridLayout(%d) columns sum to %d, want %d", w, sum, w)
		}
	}
}

func TestSparklineWidthAndDormant(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	cases := [][]int{
		{0, 0, 1, 2, 3, 5, 8, 4, 2, 1, 0, 6},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		nil,
		{1},
	}
	for _, c := range cases {
		s := sparkline(c, 12, bg, false)
		if w := ansi.StringWidth(s); w != 12 {
			t.Errorf("sparkline(%v) width = %d, want 12", c, w)
		}
		if strings.Contains(s, "\x1b[0m ") {
			t.Errorf("sparkline(%v) has an unstyled gap (banding)", c)
		}
	}
	// an all-zero week history renders only dormant dots, no bars
	plain := ansi.Strip(sparkline([]int{0, 0, 0}, 12, bg, false))
	if strings.ContainsAny(plain, "▁▂▃▄▅▆▇█") {
		t.Errorf("dormant sparkline should have no bars, got %q", plain)
	}
}

func TestDisplayRootAbbreviatesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := displayRoot(home + "/Documents/GitHub"); got != "~/Documents/GitHub" {
		t.Errorf("displayRoot = %q, want ~/Documents/GitHub", got)
	}
}
