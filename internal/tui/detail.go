package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"strings"
	"time"
)

type detailState struct {
	repo     repo.Repo
	info     orchardgit.DetailInfo
	langs    []lang.Stat
	sessions []claude.Session // recent Claude Code sessions in this repo
	err      string
}

func (m model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "enter", "backspace":
		m.mode = modeList
		m.detail = nil
		m.status = ""
		return m, nil
	case "up", "k":
		m.detailVP.ScrollUp(1)
	case "down", "j":
		m.detailVP.ScrollDown(1)
	case "pgup":
		m.detailVP.ScrollUp(m.detailVP.Height)
	case "pgdown":
		m.detailVP.ScrollDown(m.detailVP.Height)
	case "p":
		r := m.repoByPath(m.detailRepo)
		m.mode = modeList
		cmd := m.startPull([]repo.Repo{r})
		m.syncRows()
		return m, cmd
	case "O":
		r := m.repoByPath(m.detailRepo)
		return m, openCmd(r)
	case "c":
		return m.openClaude([]repo.Repo{m.repoByPath(m.detailRepo)})
	case "C":
		return m.openClaudeResume(m.repoByPath(m.detailRepo))
	case "H":
		return m.openSessions(m.repoByPath(m.detailRepo))
	case "M":
		return m.openCommitMessage(m.repoByPath(m.detailRepo))
	case "I":
		return m.requestWire([]repo.Repo{m.repoByPath(m.detailRepo)})
	case "d":
		return m.openDiff(m.repoByPath(m.detailRepo))
	case "b":
		return m.openBranchSwitcher(m.repoByPath(m.detailRepo))
	case "e":
		return m.openEditor(m.repoByPath(m.detailRepo), false)
	case "E":
		return m.openEditor(m.repoByPath(m.detailRepo), true)
	}
	return m, nil
}

func (m model) openDetail() (tea.Model, tea.Cmd) {
	r, ok := m.currentRepo()
	if !ok {
		return m, nil
	}
	m.mode = modeDetail
	m.detailRepo = r.Path
	m.detail = nil
	m.status = "loading " + r.Name
	m.detailVP.SetContent(subtleStyle.Render("  loading…"))
	m.detailVP.GotoTop()
	return m, detailCmd(r)
}

func detailCmd(r repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg {
			return detailMsg{path: r.Path, info: demoDetail(r), langs: demoDetailLangs(r.Path), sessions: demoSessions()}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := orchardgit.Detail(ctx, r)
		return detailMsg{path: r.Path, info: info, langs: lang.Detect(ctx, r.Path), sessions: claude.Sessions(r.Path, 10), err: err}
	}
}

func (m *model) setDetailContent() {
	m.detailVP.SetContent(m.detailBody(m.detailVP.Width))
	m.detailVP.GotoTop()
}

func (m model) detailBody(width int) string {
	// seg/line ensure every character carries the app background, so there are
	// no unstyled spaces showing the terminal default colour (no gray bands).
	line := func(s string) string { return fillLine(s, width, bg) }

	if m.detail == nil {
		return line(seg(muted, "  loading…"))
	}
	if m.detail.err != "" {
		return line(segB(red, "  "+m.detail.err))
	}
	d := m.detail
	blank := line("")
	var rows []string
	header := func(icon, title string) {
		rows = append(rows, blank, line(segB(blue, "  "+icon+"  "+title)))
	}

	// languages (dominant first, with icon + share)
	if len(d.langs) > 0 {
		parts := "  "
		for i, l := range d.langs {
			if i >= 4 {
				break
			}
			glyph := l.Icon
			if glyph == "" {
				glyph = "●"
			}
			if i > 0 {
				parts += seg(muted, "  ")
			}
			parts += seg(l.Color, glyph+" ") + seg(ice, l.Name) + seg(muted, fmt.Sprintf(" %d%%", l.Pct))
		}
		rows = append(rows, line(segB(blue, "  "+iconCommit+"  Languages")), line(parts))
	}

	// Instructions - whether Claude has project context here (CLAUDE.md / AGENTS.md)
	if s, ok := m.instructionsByPath[m.detailRepo]; ok {
		rows = append(rows, blank, line(segB(blue, "  "+iconCommit+"  Instructions")))
		mark := func(have bool) string {
			if have {
				return seg(green, iconCheck)
			}
			return seg(muted, "·")
		}
		row := seg(muted, "    ") + mark(s.hasClaude) + seg(ice, " CLAUDE.md") + seg(muted, "    ") + mark(s.hasAgents) + seg(ice, " AGENTS.md")
		rows = append(rows, line(row))
		switch {
		case s.canWire():
			rows = append(rows, line(seg(orange, "    AGENTS.md not loaded by Claude")+seg(muted, " · press ")+seg(blue, "I")+seg(muted, " to wire @AGENTS.md")))
		case s.hasClaude && s.hasAgents && !s.imports:
			rows = append(rows, line(seg(orange, "    CLAUDE.md does not import @AGENTS.md")+seg(muted, " · add it to load AGENTS.md")))
		case s.blind():
			rows = append(rows, line(seg(orange, "    no CLAUDE.md")+seg(muted, " · Claude runs here with no project context")))
		}
	}

	// GitHub - open PRs + CI status (only when fetched for this repo)
	if st, ok := m.ghStatus[m.detailRepo]; ok && (st.OpenPRs > 0 || st.CIState != "") {
		rows = append(rows, blank, line(segB(blue, "  "+iconRemote+"  GitHub")))
		ciColor, ciText := muted, "no CI"
		switch st.CIState {
		case "passing":
			ciColor, ciText = green, iconCheck+" CI passing"
		case "failing":
			ciColor, ciText = red, "× CI failing"
		case "pending":
			ciColor, ciText = yellow, "● CI running"
		}
		rows = append(rows, line(seg(ciColor, "    "+ciText)+
			seg(muted, fmt.Sprintf("    ·    %d open PR%s", st.OpenPRs, pluralSuffix(st.OpenPRs)))))
		for _, pr := range st.PRs {
			rows = append(rows, line(seg(muted, "      #")+segB(ice, fmt.Sprintf("%d ", pr.Number))+
				seg(muted, fit(pr.Title, max(10, width-14)))))
		}
	}

	// Claude Code - this repo's footprint (last used, totals, recent sessions)
	if len(d.sessions) > 0 {
		var turns, tokens int
		var last time.Time
		for _, s := range d.sessions {
			turns += s.Assistant
			tokens += s.Tokens
			if s.Modified.After(last) {
				last = s.Modified
			}
		}
		rows = append(rows, blank, line(segB(claudeC, "  ✦  Claude Code")))
		rows = append(rows, line(seg(muted, "    ")+
			seg(claudeC, fmt.Sprintf("%d sessions", len(d.sessions)))+
			seg(muted, fmt.Sprintf(" · %d turns · %s tokens · last %s", turns, humanTokens(tokens), relTime(last)))))
		for i, s := range d.sessions {
			if i >= 3 {
				break
			}
			rows = append(rows, line(seg(muted, "      "+relTime(s.Modified)+"  ")+seg(ice, fit(s.DisplayTitle(), max(10, width-18)))))
		}
		rows = append(rows, line(seg(muted, "      press ")+seg(blue, "H")+seg(muted, " to browse and resume sessions")))
	}

	// working tree - grouped by change type, with file-type icons
	dirtyCount := len(d.info.StatusLines)
	header(iconWarn, fmt.Sprintf("Working tree  ·  %d change%s", dirtyCount, pluralSuffix(dirtyCount)))
	if dirtyCount == 0 {
		rows = append(rows, line(seg(green, "    "+iconCheck+"  clean - nothing to commit")))
	} else {
		for _, grp := range groupWorktree(d.info.StatusLines) {
			rows = append(rows, line(segB(grp.color, fmt.Sprintf("    %s  %s  (%d)", grp.badge, grp.label, len(grp.files)))))
			shown := 0
			for _, f := range grp.files {
				if shown >= 30 {
					rows = append(rows, line(seg(muted, fmt.Sprintf("        … and %d more", len(grp.files)-shown))))
					break
				}
				dir, base := splitDirBase(f)
				icon := seg(grp.color, "      "+fileIcon(f)+"  ")
				body := seg(muted, fit(dir, max(8, width-len(base)-14))) + segB(ice, base)
				rows = append(rows, line(icon+body))
				shown++
			}
		}
	}

	// commit graph - real branch/merge topology (vscode-style)
	header(iconCommit, "Commit graph")
	for _, gr := range d.info.Graph {
		rail, railW := colorizeRail(gr.Rail, seg)
		if !gr.IsCommit {
			rows = append(rows, line("  "+rail))
			continue
		}
		subjW := max(10, width-2-railW-1-8-1-13-1-15-1)
		rows = append(rows, line(
			"  "+rail+" "+
				seg(accent, fit(gr.Hash, 8))+
				seg(muted, " "+fit(gr.Rel, 13))+
				seg(green, " "+fit(gr.Author, 15))+
				seg(ice, " "+fit(gr.Subject, subjW))))
	}

	// remotes
	header(iconRemote, "Remotes")
	for _, rem := range d.info.Remotes {
		rows = append(rows, line(seg(blue, "    "+fit(rem, max(10, width-6)))))
	}
	return strings.Join(rows, "\n")
}

type wtGroup struct {
	label string
	badge string
	color string
	files []string
}

// groupWorktree buckets `git status --porcelain` lines by change type.
func groupWorktree(lines []string) []wtGroup {
	var modified, added, deleted, renamed, other []string
	for _, l := range lines {
		if len(l) < 3 {
			continue
		}
		code, path := l[:2], strings.TrimSpace(l[3:])
		x, y := code[0], code[1]
		switch {
		case code == "??":
			added = append(added, path)
		case y == 'M' || x == 'M':
			modified = append(modified, path)
		case x == 'D' || y == 'D':
			deleted = append(deleted, path)
		case x == 'A':
			added = append(added, path)
		case x == 'R' || x == 'C':
			renamed = append(renamed, path)
		default:
			other = append(other, path)
		}
	}
	groups := []wtGroup{
		{label: "Modified", badge: "●", color: yellow, files: modified},
		{label: "New", badge: "✚", color: green, files: added},
		{label: "Deleted", badge: "✖", color: red, files: deleted},
		{label: "Renamed", badge: "➜", color: blue, files: renamed},
		{label: "Other", badge: "•", color: muted, files: other},
	}
	out := groups[:0]
	for _, g := range groups {
		if len(g.files) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func splitDirBase(path string) (dir, base string) {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i+1], path[i+1:]
	}
	return "", path
}

// fileIcon returns a Nerd Font devicon for a path by extension.
func fileIcon(path string) string {
	_, base := splitDirBase(path)
	lower := strings.ToLower(base)
	switch lower {
	case "dockerfile":
		return ""
	case "makefile":
		return ""
	}
	ext := ""
	if i := strings.LastIndexByte(lower, '.'); i >= 0 {
		ext = lower[i+1:]
	}
	switch ext {
	case "kt", "kts":
		return ""
	case "go":
		return ""
	case "ts", "tsx":
		return ""
	case "js", "jsx", "mjs":
		return ""
	case "py":
		return ""
	case "java":
		return ""
	case "rb":
		return ""
	case "rs":
		return ""
	case "md", "markdown":
		return ""
	case "json":
		return ""
	case "yaml", "yml":
		return ""
	case "xml", "html":
		return ""
	case "css", "scss":
		return ""
	case "sql":
		return ""
	case "sh", "bash", "zsh":
		return ""
	case "gradle":
		return ""
	default:
		return ""
	}
}

// colorizeRail renders git's graph rail characters with neon colors, returning
// the styled string and its visible rune width.
func colorizeRail(rail string, seg func(string, string) string) (string, int) {
	var b strings.Builder
	w := 0
	for _, r := range rail {
		w++
		switch r {
		case '*':
			b.WriteString(seg(accent, "●"))
		case '|':
			b.WriteString(seg(muted, "│"))
		case '/':
			b.WriteString(seg(cyan, "╱"))
		case '\\':
			b.WriteString(seg(cyan, "╲"))
		case '_':
			b.WriteString(seg(muted, "─"))
		case ' ':
			b.WriteString(seg(muted, " "))
		default:
			b.WriteString(seg(muted, string(r)))
		}
	}
	return b.String(), w
}

func (m model) detailView(width int) string {
	r := m.repoByPath(m.detailRepo)
	title := titleStyle.Render(iconLogo + "  " + r.Name)
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor(r.Display))).Background(lipgloss.Color(bg)).Render(iconBranch + " " + r.Branch)
	up := ""
	if r.Upstream != "" {
		up = subtleStyle.Render("  →  " + r.Upstream)
	}
	stateChip := lipgloss.NewStyle().Foreground(lipgloss.Color(bg)).Background(lipgloss.Color(colorForState(r.Display))).Bold(true).Padding(0, 1).Render(r.Display.String())
	left := title + subtleStyle.Render("  ") + branch + up
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(stateChip))
	topLine := fillLine(left+fillLine("", gap, bg)+stateChip, width, bg)
	rule := hrule(width)
	full := []string{
		cmdHint("esc", "back"), cmdHint("↑↓", "scroll"),
		cmdHint("c", "claude"), cmdHint("C", "resume"), cmdHint("H", "sessions"),
		cmdHint("M", "commit msg"), cmdHint("I", "wire md"), cmdHint("d", "diff"), cmdHint("b", "branch"),
		cmdHint("p", "pull"), cmdHint("e", "editor"), cmdHint("O", "browser"),
	}
	joined := strings.Join(full, "")
	if lipgloss.Width(joined) > width {
		joined = strings.Join([]string{
			cmdHint("esc", "back"), cmdHint("c", "claude"), cmdHint("d", "diff"),
			cmdHint("M", "commit msg"), cmdHint("b", "branch"),
		}, "")
	}
	hints := fillLine(joined, width, bg)

	return lipgloss.JoinVertical(lipgloss.Left,
		topLine,
		rule,
		m.detailVP.View(),
		rule,
		hints,
	)
}
