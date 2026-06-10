package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/agentcfg"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/termlaunch"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// wireGraphMCP registers orchard's code-graph MCP server into configRepo's
// agent config (.mcp.json for Claude Code, .codex/config.toml for Codex) before
// a launch, serving the graphs of graphRepos (one repo normally; several for a
// cross-repo `A` session). Idempotent; set ORCHARD_GRAPH_MCP=0 to disable.
func (m model) wireGraphMCP(configRepo string, graphRepos []string) error {
	if len(graphRepos) == 0 || !m.graphWiringEnabled() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch { // never block a launch; the caller surfaces any failure in the status
	case m.assistantIsClaude():
		_, err = agentcfg.EnsureClaudeMCP(configRepo, graphRepos, exe)
	case m.assistantIsCodex():
		_, err = agentcfg.EnsureCodexMCP(configRepo, graphRepos, exe)
	}
	return err
}

// graphWiringEnabled reports whether a launch should auto-wire the code-graph
// MCP: the assistant must be an MCP consumer (Claude or Codex) and wiring must
// not be suppressed.
func (m model) graphWiringEnabled() bool {
	return (m.assistantIsClaude() || m.assistantIsCodex()) && !m.graphWireSuppressed()
}

// graphWireSuppressed reports whether the user has opted out of auto-wiring,
// either for the session (the m toggle) or durably via ORCHARD_GRAPH_MCP=0.
func (m model) graphWireSuppressed() bool {
	if m.graphWireOff {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ORCHARD_GRAPH_MCP"))) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// graphSuffix is a short status note shown when a launch wires the code graph,
// reflecting whether the config write actually succeeded.
func (m model) graphSuffix(err error) string {
	if !m.graphWiringEnabled() {
		return ""
	}
	if err != nil {
		return " · graph wiring failed"
	}
	return " · graph wired"
}

// assistantIsCodex reports whether the resolved assistant is OpenAI Codex.
func (m model) assistantIsCodex() bool {
	return strings.Contains(strings.ToLower(m.assistantCmd), "codex")
}

// Opt-in env (Claude Code 2.1.20+) that loads added --add-dir repos' CLAUDE.md.
// See github.com/anthropics/claude-code issues/21138.
const claudeAddDirMemoryEnv = "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1"

// addDirMemoryEnv enables shared CLAUDE.md on a cross-repo launch; ORCHARD_ADDDIR_MEMORY=0 disables.
func addDirMemoryEnv() []string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ORCHARD_ADDDIR_MEMORY"))) {
	case "0", "false", "no", "off":
		return nil
	}
	return []string{claudeAddDirMemoryEnv}
}

// envPrefix builds a `env K=V ...` shell prefix; the in-place fallback uses cmd.Env.
func envPrefix(env []string) string {
	if len(env) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("env")
	for _, kv := range env {
		b.WriteString(" " + shQuote(kv))
	}
	b.WriteString(" ")
	return b.String()
}

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
		m.status = "no AI assistant found (install claude or codex, or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if len(targets) == 0 {
		m.status = "nothing to open"
		return m, nil
	}
	var wireErr error
	for _, r := range targets {
		if e := m.wireGraphMCP(r.Path, []string{r.Path}); e != nil {
			wireErr = e
		}
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
	suffix := m.graphSuffix(wireErr)
	m.status = fmt.Sprintf("opening %s in %d new tab(s)%s", label, len(targets), suffix)
	return m, func() tea.Msg {
		opened := 0
		for _, dir := range dirs {
			if cmd, ok := termlaunch.NewTab(dir, prog); ok && cmd != nil {
				if err := cmd.Run(); err == nil {
					opened++
				}
			}
		}
		return statusMsg{text: fmt.Sprintf("opened %s in %d tab(s)%s", label, opened, suffix)}
	}
}

// assistantIsClaude reports whether the resolved assistant is Claude Code.
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
func (m model) runAssistant(cwd string, args, env []string, status string, graphRepos []string) (tea.Model, tea.Cmd) {
	wireErr := m.wireGraphMCP(cwd, graphRepos)
	if len(graphRepos) > 0 {
		status += m.graphSuffix(wireErr)
	}
	prog := m.assistantCmd
	for _, a := range args {
		prog += " " + shQuote(a)
	}
	prog = envPrefix(env) + prog
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
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}
	return m, tea.ExecProcess(c, func(error) tea.Msg {
		return statusMsg{text: "returned from " + m.assistantLabel}
	})
}

// openClaudeResume continues the most recent session in a repo (`c`'s sibling
// `C`), with whichever assistant is resolved. Assistants without a resume
// command just open normally.
func (m model) openClaudeResume(r repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or codex, or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if !m.agentSupportsSessions() {
		return m.openClaude([]repo.Repo{r})
	}
	return m.runAssistant(r.Path, m.agentResumeLastArgs(), nil, "resuming "+m.assistantLabel+" · "+r.Name, []string{r.Path})
}

// openClaudeCombined opens one session spanning the selected repos via
// --add-dir (Claude Code and Codex both support it), so a single conversation
// can work across services.
func (m model) openClaudeCombined(targets []repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or codex, or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if len(targets) == 0 {
		m.status = "nothing to open"
		return m, nil
	}
	if len(targets) == 1 || !m.agentSupportsSessions() {
		return m.openClaude(targets) // one repo, or an assistant without --add-dir
	}
	var args []string
	for _, r := range targets[1:] {
		args = append(args, "--add-dir", r.Path)
	}
	var mem []string
	if m.assistantIsClaude() {
		mem = addDirMemoryEnv() // the shared-CLAUDE.md opt-in is Claude-specific
	}
	status := fmt.Sprintf("opening %s in %s + %d more via --add-dir", m.assistantLabel, targets[0].Name, len(targets)-1)
	if len(mem) > 0 {
		status += " (shared CLAUDE.md)"
	}
	// Cross-repo: wire one orchard MCP into the primary repo serving the graphs
	// of all session repos, so the agent can query across them.
	graphRepos := make([]string, len(targets))
	for i, r := range targets {
		graphRepos[i] = r.Path
	}
	return m.runAssistant(targets[0].Path, args, mem, status, graphRepos)
}

// commitMsgPrompt is the prompt used when drafting a commit message in a terminal
// session (the fallback for non-Claude assistants; see commitMsgPromptHeadless).
const commitMsgPrompt = "Write a concise git commit message for the staged changes " +
	"(or the whole working tree if nothing is staged). Use conventional-commit style and imperative mood. " +
	"First line: a focused subject under 72 characters; use a scope only when obvious. " +
	"If a body adds value, add one blank line and then 2-4 concise bullet points starting with '- '; no paragraph body. " +
	"Do not mention details that are not evident in the diff. Show only the message in a code block; do not commit anything."

func claudeStatsCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return claudeStatsMsg{usage: demoClaude(), codex: demoCodex()} }
	}
	return func() tea.Msg {
		targets := make([]claude.Target, 0, len(repos))
		for _, r := range repos {
			targets = append(targets, claude.Target{Name: r.Name, Path: r.Path})
		}
		return claudeStatsMsg{usage: claude.Aggregate(targets), codex: codex.Aggregate(targets)}
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
			fillLine(fitStyled(marker+title+content, width), width, bg),
			fillLine(marker, width, bg))
	}
	if m.claudeUsage == nil {
		return one(seg(muted, "   analyzing usage…"))
	}
	u := m.claudeUsage
	if u.TotalSessions == 0 {
		if cx := m.codexUsage; cx != nil && cx.TotalSessions > 0 {
			return lipgloss.JoinVertical(lipgloss.Left, rule,
				fillLine(fitStyled(m.codexPanelLine(), width), width, bg))
		}
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
		busiest += seg(ice, fit(r.Name, 18)+" ") + miniBar(r.Turns, u.Repos[0].Turns, 6, green) + seg(muted, fmt.Sprintf(" %d   ", r.Turns))
	}
	if len(u.Repos) > topRepos {
		busiest += seg(muted, fmt.Sprintf("+%d more", len(u.Repos)-topRepos))
	}
	l2 := marker + segB(ice, "MODELS ") + models + seg(muted, " │  ") + segB(ice, "BUSIEST ") + busiest

	lines := []string{rule,
		fillLine(fitStyled(l1, width), width, bg),
		fillLine(fitStyled(l2, width), width, bg)}
	if cx := m.codexUsage; cx != nil && cx.TotalSessions > 0 {
		lines = append(lines, fillLine(fitStyled(m.codexPanelLine(), width), width, bg))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// codexPanelLine is the one-line Codex usage summary appended under the Claude
// panel when Codex has run in these repos, so both agents' footprints are pinned.
func (m model) codexPanelLine() string {
	cx := m.codexUsage
	marker := lipgloss.NewStyle().Foreground(lipgloss.Color(codexC)).Background(lipgloss.Color(bg)).Bold(true).Render("▌ ")
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(codexC)).Background(lipgloss.Color(bg)).Bold(true).Render("❖ CODEX      ")
	chip := func(icon, value, label, color string) string {
		return seg(color, icon+" ") + segB(color, value) + seg(muted, " "+label)
	}
	sp := seg(muted, "    ")
	chips := chip(iconBolt, fmt.Sprintf("%d", cx.TotalSessions), "sessions", blue) + sp +
		chip(iconCommit, fmt.Sprintf("%d", cx.TotalTurns), "turns", green) + sp +
		chip(iconCommit, humanTokens(cx.TotalTokens), "tokens", teal) + sp +
		chip(iconFolder, fmt.Sprintf("%d", cx.ReposUsed), "repos", accent) + sp +
		chip(iconClock, relTime(cx.Last), "ago", codexC)
	top := ""
	for name := range cx.Models {
		if top == "" || cx.Models[name] > cx.Models[top] {
			top = name
		}
	}
	if top != "" {
		chips += sp + seg(codexC, top)
	}
	return marker + title + seg(bg, " ") + chips
}

// showClaudePanel reports whether the pinned usage panel has real data to show.
// When false the panel is hidden and the repo list reclaims its rows, so people
// who use neither agent never see an empty panel.
func (m model) showClaudePanel() bool {
	return (m.claudeUsage != nil && m.claudeUsage.TotalSessions > 0) ||
		(m.codexUsage != nil && m.codexUsage.TotalSessions > 0)
}

// claudePanelRows is the panel's height in rows, mirroring claudePanel's output
// exactly so resize can budget the list's viewport.
func (m model) claudePanelRows() int {
	cl := m.claudeUsage != nil && m.claudeUsage.TotalSessions > 0
	cx := m.codexUsage != nil && m.codexUsage.TotalSessions > 0
	switch {
	case cl && cx:
		return 4 // rule + two Claude lines + the Codex line
	case cl:
		return 3
	case cx:
		return 2 // rule + the Codex line
	default:
		return 0
	}
}
