package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/termlaunch"
	"os/exec"
	"sort"
	"strings"
)

// selectionTargets is the set of repos an action applies to: the multi-select
// if any, otherwise the repo under the cursor.
func (m model) selectionTargets() []repo.Repo {
	if len(m.selected) > 0 {
		var out []repo.Repo
		for _, r := range m.repos {
			if m.selected[r.Path] {
				out = append(out, r)
			}
		}
		return out
	}
	if r, ok := m.currentRepo(); ok {
		return []repo.Repo{r}
	}
	return nil
}

// openClaude launches Claude Code for each target in a new terminal tab/window,
// so orchard keeps running. If no new-tab mechanism is available it falls back
// to suspending into the first repo.
func (m model) openClaude(targets []repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or copilot, or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if len(targets) == 0 {
		m.status = "nothing to open"
		return m, nil
	}
	prog, label := m.assistantCmd, m.assistantLabel
	if _, ok := termlaunch.NewTab(targets[0].Path, prog); !ok {
		fields := strings.Fields(prog)
		c := exec.Command(fields[0], fields[1:]...)
		c.Dir = targets[0].Path
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return statusMsg{text: "returned from " + label + " · " + targets[0].Name}
		})
	}
	dirs := make([]string, len(targets))
	for i, r := range targets {
		dirs[i] = r.Path
	}
	m.status = fmt.Sprintf("opening %s in %d new tab(s)", label, len(targets))
	return m, func() tea.Msg {
		opened := 0
		for _, dir := range dirs {
			if cmd, ok := termlaunch.NewTab(dir, prog); ok && cmd != nil {
				if err := cmd.Run(); err == nil {
					opened++
				}
			}
		}
		return statusMsg{text: fmt.Sprintf("opened %s in %d tab(s)", label, opened)}
	}
}

// assistantIsClaude reports whether the resolved assistant is Claude Code, which
// is the only one that supports --continue and --add-dir.
func (m model) assistantIsClaude() bool {
	return strings.Contains(strings.ToLower(m.assistantCmd), "claude")
}

// shQuote single-quotes a string for a POSIX shell, so a path with spaces stays
// one argument when termlaunch runs the assistant through a login shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runAssistant launches the assistant once in cwd with the given flag args, in a
// new terminal tab (falling back to running in place). The tab path goes through
// a shell so args are shell-quoted; the in-place fallback uses argv directly.
func (m model) runAssistant(cwd string, args []string, status string) (tea.Model, tea.Cmd) {
	prog := m.assistantCmd
	for _, a := range args {
		prog += " " + shQuote(a)
	}
	if cmd, ok := termlaunch.NewTab(cwd, prog); ok && cmd != nil {
		m.status = status
		return m, func() tea.Msg {
			if err := cmd.Run(); err != nil {
				return statusMsg{text: "launch failed: " + err.Error()}
			}
			return statusMsg{text: status}
		}
	}
	fields := append(strings.Fields(m.assistantCmd), args...)
	c := exec.Command(fields[0], fields[1:]...)
	c.Dir = cwd
	return m, tea.ExecProcess(c, func(error) tea.Msg {
		return statusMsg{text: "returned from " + m.assistantLabel}
	})
}

// openClaudeResume continues the most recent Claude session in a repo (`c`'s
// sibling `C`). Non-Claude assistants just open normally.
func (m model) openClaudeResume(r repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if !m.assistantIsClaude() {
		return m.openClaude([]repo.Repo{r})
	}
	return m.runAssistant(r.Path, []string{"--continue"}, "resuming "+m.assistantLabel+" · "+r.Name)
}

// openClaudeCombined opens one Claude session spanning the selected repos via
// --add-dir, so a single conversation can work across services.
func (m model) openClaudeCombined(targets []repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if len(targets) == 0 {
		m.status = "nothing to open"
		return m, nil
	}
	if len(targets) == 1 || !m.assistantIsClaude() {
		return m.openClaude(targets) // one repo, or non-Claude: just open it
	}
	var args []string
	for _, r := range targets[1:] {
		args = append(args, "--add-dir", r.Path)
	}
	status := fmt.Sprintf("opening %s in %s + %d more via --add-dir", m.assistantLabel, targets[0].Name, len(targets)-1)
	return m.runAssistant(targets[0].Path, args, status)
}

// commitMsgPrompt is the starting prompt for Claude when drafting a commit message.
const commitMsgPrompt = "Write a concise git commit message for the staged changes " +
	"(or the whole working tree if nothing is staged). Conventional style, imperative mood, " +
	"no body unless it adds value. Show the message in a code block; do not commit anything."

// openClaudeCommitMessage launches Claude in a repo with a prompt to draft a
// commit message from the current changes (Claude only).
func (m model) openClaudeCommitMessage(r repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if !m.assistantIsClaude() {
		m.status = "commit-message drafting needs Claude Code"
		return m, nil
	}
	return m.runAssistant(r.Path, []string{commitMsgPrompt}, "drafting a commit message · "+r.Name)
}

func claudeStatsCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return claudeStatsMsg{usage: demoClaude()} }
	}
	return func() tea.Msg {
		targets := make([]claude.Target, 0, len(repos))
		for _, r := range repos {
			targets = append(targets, claude.Target{Name: r.Name, Path: r.Path})
		}
		return claudeStatsMsg{usage: claude.Aggregate(targets)}
	}
}

// claudePanel is the fixed Claude Code usage summary pinned below the repo list:
// a branded title, stat chips, and mini bars for models + busiest repos. It
// reads the cached aggregate, so it re-renders cheaply and never scrolls away.
func (m model) claudePanel(width int) string {
	rule := hrule(width)
	marker := lipgloss.NewStyle().Foreground(lipgloss.Color(claudeC)).Background(lipgloss.Color(bg)).Bold(true).Render("▌ ")
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(claudeC)).Background(lipgloss.Color(bg)).Bold(true).Render("✦ CLAUDE CODE")

	one := func(content string) string {
		return lipgloss.JoinVertical(lipgloss.Left, rule,
			fillLine(marker+title+content, width, bg),
			fillLine(marker, width, bg))
	}
	if m.claudeUsage == nil {
		return one(seg(muted, "   analyzing usage…"))
	}
	u := m.claudeUsage
	if u.TotalSessions == 0 {
		return one(seg(muted, "   no Claude Code sessions in these repos yet"))
	}

	// stat figures, painted on the background (no boxes) to match the footer
	chip := func(icon, value, label, color string) string {
		return seg(color, icon+" ") + segB(color, value) + seg(muted, " "+label)
	}
	sp := seg(muted, "    ")
	chips := chip(iconBolt, fmt.Sprintf("%d", u.TotalSessions), "sessions", blue) + sp +
		chip(iconCommit, fmt.Sprintf("%d", u.TotalTurns), "turns", green) + sp +
		chip(iconCommit, humanTokens(u.TotalTokens), "tokens", teal) + sp +
		chip(iconFolder, fmt.Sprintf("%d", u.ReposUsed), "repos", accent) + sp +
		chip(iconClock, relTime(u.Last), "ago", claudeC)
	l1 := marker + title + seg(bg, "   ") + chips

	// mini bar
	miniBar := func(n, mx, w int, color string) string {
		if mx <= 0 {
			mx = 1
		}
		f := n * w / mx
		if n > 0 && f < 1 {
			f = 1
		}
		if f > w {
			f = w
		}
		return seg(color, strings.Repeat("█", f)) + seg(muted, strings.Repeat("░", w-f))
	}

	// models (top 3) with bars
	type mv struct {
		name string
		c    int
	}
	var ms []mv
	for k, v := range u.Models {
		ms = append(ms, mv{k, v})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].c > ms[j].c })
	const topModels, topRepos = 3, 3
	models := ""
	for i, m2 := range ms {
		if i >= topModels {
			break
		}
		models += seg(claudeC, m2.name+" ") + miniBar(m2.c, ms[0].c, 6, accent) + seg(muted, fmt.Sprintf(" %d   ", m2.c))
	}
	if len(ms) > topModels {
		models += seg(muted, fmt.Sprintf("+%d more  ", len(ms)-topModels))
	}

	// busiest repos with bars (+N more for the rest)
	busiest := ""
	for i, r := range u.Repos {
		if i >= topRepos {
			break
		}
		busiest += seg(ice, r.Name+" ") + miniBar(r.Turns, u.Repos[0].Turns, 6, green) + seg(muted, fmt.Sprintf(" %d   ", r.Turns))
	}
	if len(u.Repos) > topRepos {
		busiest += seg(muted, fmt.Sprintf("+%d more", len(u.Repos)-topRepos))
	}
	l2 := marker + segB(ice, "MODELS ") + models + seg(muted, " │  ") + segB(ice, "BUSIEST ") + busiest

	return lipgloss.JoinVertical(lipgloss.Left, rule,
		fillLine(l1, width, bg),
		fillLine(l2, width, bg))
}

// showClaudePanel reports whether the pinned usage panel has real data to show.
// When false the panel is hidden and the repo list reclaims its rows, so people
// who do not use Claude Code never see an empty panel.
func (m model) showClaudePanel() bool {
	return m.claudeUsage != nil && m.claudeUsage.TotalSessions > 0
}
