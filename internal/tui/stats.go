package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

// statsWeeks is how many weeks the in-TUI stats heatmaps span.
const statsWeeks = 20

type statsMsg struct {
	harvest map[string]int // commit day -> count
	claude  map[string]int // Claude session day -> turns
	codex   map[string]int // Codex session day -> turns
}

// openStats opens the in-TUI stats page (reusing the detail viewport for scroll).
// The fast parts render from already-loaded model data; the heatmaps load async.
func (m model) openStats() (tea.Model, tea.Cmd) {
	m.returnMode = m.mode
	m.mode = modeStats
	m.statsLoading = true
	m.detailVP.SetContent(m.statsBody(m.detailVP.Width))
	m.detailVP.GotoTop()
	return m, statsCmd(m.repos)
}

func statsCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg {
			h, c, cx := demoStatsData()
			return statsMsg{harvest: h, claude: c, codex: cx}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		since := fmt.Sprintf("%d weeks ago", statsWeeks+1)
		harvest := map[string]int{}
		for _, r := range repos {
			for _, d := range orchardgit.AuthoredDays(ctx, r.Path, since) {
				harvest[d]++
			}
		}
		cutoff := time.Now().AddDate(0, 0, -7*statsWeeks)
		cl := map[string]int{}
		for _, r := range repos {
			for _, s := range claude.Sessions(r.Path, 0) {
				if s.Modified.Before(cutoff) {
					continue
				}
				cl[s.Modified.Format("2006-01-02")] += s.Assistant
			}
		}
		cx := map[string]int{}
		for _, r := range repos {
			for _, s := range codex.Sessions(r.Path, 0) {
				if s.Modified.Before(cutoff) {
					continue
				}
				cx[s.Modified.Format("2006-01-02")] += s.Assistant
			}
		}
		return statsMsg{harvest: harvest, claude: cl, codex: cx}
	}
}

func (m model) handleStatsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "T":
		m.mode = m.returnMode
		return m, nil
	}
	return m, nil
}

func (m model) statsView(width int) string {
	title := titleStyle.Render(" Orchard stats") + subtleStyle.Render("  · "+displayRoot(m.root))
	rule := hrule(width)
	hints := fillLine(strings.Join([]string{
		cmdHint("↑↓", "scroll"), cmdHint("g/G", "top/bottom"), cmdHint("esc", "back"),
	}, ""), width, bg)
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg),
		rule,
		m.detailVP.View(),
		rule,
		hints,
	)
}

// statsBody renders the page content (counts, languages, freshest/thirstiest,
// Claude usage, and the harvest + Claude heatmaps), every line painted on bg.
func (m model) statsBody(width int) string {
	line := func(s string) string { return fillLine(s, width, bg) }
	var rows []string

	var healthy, untended, needsWater, wild int
	for _, r := range m.repos {
		switch r.Display {
		case repo.DisplayClean, repo.DisplayAhead, repo.DisplayFeature:
			healthy++
		case repo.DisplayDirty:
			untended++
		case repo.DisplayBehind, repo.DisplayDiverged:
			needsWater++
		default:
			wild++
		}
	}
	gap := seg(muted, "    ")
	rows = append(rows, line(""), line(
		metric(iconFolder, "REPOS", len(m.repos), blue)+gap+
			metric(iconCheck, "HEALTHY", healthy, green)+gap+
			metric(iconWarn, "UNTENDED", untended, yellow)+gap+
			metric(iconArrowDn, "NEED WATER", needsWater, red)))

	// languages (repos per dominant language)
	type lc struct {
		name, color string
		n           int
	}
	tally := map[string]*lc{}
	for _, st := range m.langByPath {
		if st.Name == "" {
			continue
		}
		if tally[st.Name] == nil {
			tally[st.Name] = &lc{name: st.Name, color: st.Color}
		}
		tally[st.Name].n++
	}
	if len(tally) > 0 {
		ls := make([]*lc, 0, len(tally))
		for _, v := range tally {
			ls = append(ls, v)
		}
		sort.Slice(ls, func(i, j int) bool {
			if ls[i].n != ls[j].n {
				return ls[i].n > ls[j].n
			}
			return ls[i].name < ls[j].name
		})
		rows = append(rows, line(""), line(segB(blue, "  Languages")))
		const barW = 18
		maxN := ls[0].n
		for i, l := range ls {
			if i >= 8 {
				break
			}
			f := l.n * barW / maxN
			if l.n > 0 && f < 1 {
				f = 1
			}
			bar := seg(l.color, strings.Repeat("█", f)) + seg(muted, strings.Repeat("░", barW-f))
			rows = append(rows, line(seg(ice, "    "+padRight(l.name, 12))+" "+bar+seg(muted, fmt.Sprintf("  %d", l.n))))
		}
	}

	// freshest / thirstiest by last-fetched time
	var fresh, thirst repo.Repo
	for _, r := range m.repos {
		if r.LastFetched.IsZero() {
			continue
		}
		if fresh.Path == "" || r.LastFetched.After(fresh.LastFetched) {
			fresh = r
		}
		if thirst.Path == "" || r.LastFetched.Before(thirst.LastFetched) {
			thirst = r
		}
	}
	if fresh.Path != "" {
		rows = append(rows, line(""))
		rows = append(rows, line(seg(muted, "  "+padRight("freshest", 11))+seg(ice, padRight(fit(fresh.Name, 28), 28))+seg(freshnessColor(fresh.LastFetched), relTime(fresh.LastFetched))))
		if thirst.Path != "" && thirst.Path != fresh.Path {
			rows = append(rows, line(seg(muted, "  "+padRight("thirstiest", 11))+seg(ice, padRight(fit(thirst.Name, 28), 28))+seg(freshnessColor(thirst.LastFetched), relTime(thirst.LastFetched))))
		}
	}

	// Claude usage
	if u := m.claudeUsage; u != nil && u.TotalSessions > 0 {
		rows = append(rows, line(""), line(segB(claudeC, "  Claude Code")+
			seg(muted, fmt.Sprintf("    %d sessions · %d turns · %s tokens", u.TotalSessions, u.TotalTurns, humanTokens(u.TotalTokens)))))
	}
	if u := m.codexUsage; u != nil && u.TotalSessions > 0 {
		rows = append(rows, line(segB(codexC, "  Codex")+
			seg(muted, fmt.Sprintf("          %d sessions · %d turns · %s tokens", u.TotalSessions, u.TotalTurns, humanTokens(u.TotalTokens)))))
	}
	if days, set := claude.CleanupPeriodDays(); set && days == 0 {
		rows = append(rows, line(seg(orange, "  ⚠ Claude cleanupPeriodDays=0")+seg(muted, " · sessions auto-delete; raise it to keep your history")))
	}

	// heatmaps
	if m.statsLoading {
		rows = append(rows, line(""), line(seg(muted, "  computing harvest + agent heatmaps…")))
	} else {
		rows = append(rows, heatGrid("harvest",
			fmt.Sprintf("%d commits in the last %d weeks", sumMap(m.statsHarvest), statsWeeks),
			m.statsHarvest, [3]int{2, 5, 9}, [4]string{"#356E3F", "#5FA052", "#8FD15A", "#B6F36A"}, width)...)
		rows = append(rows, heatGrid("claude",
			fmt.Sprintf("%d turns in the last %d weeks", sumMap(m.statsClaude), statsWeeks),
			m.statsClaude, [3]int{20, 50, 100}, [4]string{"#7A4A1E", "#B5742E", "#E0973F", "#FF9E64"}, width)...)
		rows = append(rows, heatGrid("codex",
			fmt.Sprintf("%d turns in the last %d weeks", sumMap(m.statsCodex), statsWeeks),
			m.statsCodex, [3]int{20, 50, 100}, [4]string{"#14532D", "#15803D", "#16A34A", "#19C37D"}, width)...)
	}
	return strings.Join(rows, "\n")
}

// heatGrid renders a bg-painted contribution grid (weeks as columns, weekdays as
// rows) from per-day counts; the empty case shows a quiet "no activity" line.
func heatGrid(label, sub string, counts map[string]int, thresholds [3]int, ramp [4]string, width int) []string {
	line := func(s string) string { return fillLine(s, width, bg) }
	if sumMap(counts) == 0 {
		return []string{line(""), line(segB(blue, "  "+label) + seg(muted, "   no activity yet"))}
	}
	const empty = "#3A3F58"
	cellOf := func(n int) string {
		switch {
		case n <= 0:
			return seg(empty, "·")
		case n <= thresholds[0]:
			return seg(ramp[0], "■")
		case n <= thresholds[1]:
			return seg(ramp[1], "■")
		case n <= thresholds[2]:
			return seg(ramp[2], "■")
		default:
			return seg(ramp[3], "■")
		}
	}
	now := time.Now()
	first := now.AddDate(0, 0, -int(now.Weekday())).AddDate(0, 0, -7*(statsWeeks-1))
	labels := []string{"", "Mon", "", "Wed", "", "Fri", ""}

	rows := []string{line(""), line(segB(blue, "  "+label) + seg(muted, "   "+sub))}
	for row := 0; row < 7; row++ {
		s := seg(muted, "  "+padRight(labels[row], 4))
		for col := 0; col < statsWeeks; col++ {
			day := first.AddDate(0, 0, col*7+row)
			if day.After(now) {
				s += seg(bg, " ")
				continue
			}
			s += cellOf(counts[day.Format("2006-01-02")])
		}
		rows = append(rows, line(s))
	}
	rows = append(rows, line(seg(muted, "      less ")+seg(empty, "·")+
		seg(ramp[0], "■")+seg(ramp[1], "■")+seg(ramp[2], "■")+seg(ramp[3], "■")+seg(muted, " more")))
	return rows
}

func sumMap(m map[string]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}
