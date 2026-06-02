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
	"strconv"
	"strings"
	"time"
)

// staleCommitThreshold: commits since the last Claude session before the detail
// view warns that the session context may be stale.
const staleCommitThreshold = 10

// touchMapSessions is how many recent transcripts the touch map scans;
// touchMapShow is how many files it lists before collapsing the rest.
const (
	touchMapSessions = 8
	touchMapShow     = 6
)

const detailSectionIndent = "    "

type detailState struct {
	repo         repo.Repo
	info         orchardgit.DetailInfo
	langs        []lang.Stat
	sessions     []claude.Session     // recent Claude Code sessions in this repo
	commitsSince int                  // commits since Claude last ran here (stale-context hint)
	touched      []claude.TouchedFile // files Claude read/edited here (touch map)
	err          string
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
	case "y":
		m.status = copyToClipboard(m.repoByPath(m.detailRepo).Path, "path")
		return m, nil
	case "c":
		return m.openClaude([]repo.Repo{m.repoByPath(m.detailRepo)})
	case "C":
		return m.openClaudeResume(m.repoByPath(m.detailRepo))
	case "H":
		return m.openSessions(m.repoByPath(m.detailRepo))
	case "f":
		return m.openTouched(m.repoByPath(m.detailRepo))
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
	m.setDetailContent() // animated loading line (m.detail is nil); no gray band
	return m, tea.Batch(detailCmd(r), m.spinner.Tick)
}

func detailCmd(r repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg {
			return detailMsg{path: r.Path, info: demoDetail(r), langs: demoDetailLangs(r.Path), sessions: demoSessions(), commitsSince: 14, touched: demoTouched()}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := orchardgit.Detail(ctx, r)
		sessions := claude.Sessions(r.Path, 10)
		return detailMsg{path: r.Path, info: info, langs: lang.Detect(ctx, r.Path), sessions: sessions, commitsSince: commitsSinceClaude(ctx, r.Path, sessions), touched: claude.TouchMap(r.Path, touchMapSessions), err: err}
	}
}

// commitsSinceClaude counts commits landed after the most recent Claude session.
func commitsSinceClaude(ctx context.Context, path string, sessions []claude.Session) int {
	var last time.Time
	for _, s := range sessions {
		if s.Modified.After(last) {
			last = s.Modified
		}
	}
	if last.IsZero() {
		return 0
	}
	return orchardgit.CountCommitsSince(ctx, path, last)
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
		return line(seg(claudeC, m.spinner.View()) + seg(muted, "  loading…"))
	}
	if m.detail.err != "" {
		return line(segB(red, "  "+m.detail.err))
	}
	d := m.detail
	blank := line("")
	var rows []string
	sectionHeading := func(color, icon, title, suffix string) string {
		head := detailSectionIndent
		if icon != "" {
			head += icon + "  "
		}
		head += title
		if suffix != "" {
			head += suffix
		}
		return segB(color, head)
	}
	header := func(icon, title string) {
		rows = append(rows, blank, line(sectionHeading(blue, icon, title, "")))
	}

	// languages (dominant first, with icon + share)
	if len(d.langs) > 0 {
		parts := detailSectionIndent
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
		rows = append(rows, line(sectionHeading(blue, iconCommit, "Languages", "")), line(parts))
	}

	// Claude Code - everything about the agent in one place, laid out as labeled
	// rows (activity / context / sessions / files) with whitespace between the
	// clusters, so a newcomer can scan what each line means and what to act on.
	instr, hasInstr := m.instructionsByPath[m.detailRepo]
	if len(d.sessions) > 0 || hasInstr {
		const labelW = 9
		label := func(name string) string { return segB(teal, fmt.Sprintf("%s%-*s ", detailSectionIndent, labelW, name)) }
		indent := fmt.Sprintf("%s%*s ", detailSectionIndent, labelW, "") // continuation indent under the label column
		note := func(s string) string { return line(seg(muted, indent+s)) }
		warn := func(s string) string { return line(seg(muted, indent) + seg(yellow, s)) }
		metricSep := seg(muted, "  ·  ")

		rows = append(rows, blank,
			line(sectionHeading(claudeC, "", "Claude Code", "")+seg(muted, "   what the AI assistant has done in this repo")))

		// activity: how much the assistant has run here
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
			rows = append(rows, line(label("activity")+
				seg(ice, fmt.Sprintf("%d sessions", len(d.sessions)))+metricSep+
				seg(ice, fmt.Sprintf("%d turns", turns))+metricSep+
				seg(ice, fmt.Sprintf("%s tokens", humanTokens(tokens)))+metricSep+
				seg(ice, fmt.Sprintf("last %s ago", relTime(last)))))
			if d.commitsSince >= staleCommitThreshold {
				rows = append(rows, warn(fmt.Sprintf("%d commits since it last ran here · its context may be stale", d.commitsSince)))
			}
		} else {
			rows = append(rows, line(label("activity")+seg(ice, "not used in this repo yet")))
		}

		// context: the project instructions the agent loads on launch
		if hasInstr {
			rows = append(rows, line(label("context")+contextStatusValue(instr)))
			switch {
			case instr.canWire():
				rows = append(rows, warn("AGENTS.md is not loaded by Claude · press I to wire @AGENTS.md"))
			case instr.hasClaude && instr.hasAgents && !instr.imports:
				rows = append(rows, warn("CLAUDE.md does not import @AGENTS.md · add it to load AGENTS.md"))
			case instr.blind():
				rows = append(rows, note("the agent starts cold here, with no project notes to load"))
			}
			if instr.claudeBytes > claudeMDLargeBytes {
				rows = append(rows, warn(fmt.Sprintf("CLAUDE.md is large (%dKB) · it spends a lot of context every session", instr.claudeBytes/1000)))
			}
		}

		// sessions: the most recent transcripts (resume them with H)
		if len(d.sessions) > 0 {
			rows = append(rows, blank)
			for i, s := range d.sessions {
				if i >= 3 {
					break
				}
				name := ""
				if i == 0 {
					name = "sessions"
				}
				rows = append(rows, line(label(name)+seg(muted, relTime(s.Modified)+"   ")+seg(ice, fit(s.DisplayTitle(), max(10, width-len(indent)-8)))))
			}
			rows = append(rows, line(seg(muted, indent+"press ")+seg(blue, "H")+seg(muted, " to browse or resume")))
		}

		// files: the touch map - what the agent read or edited, edited first, with
		// files it changed but has not committed flagged.
		if len(d.touched) > 0 {
			rows = append(rows, blank)
			dirty := dirtyPathSet(d.info.StatusLines)
			shown := d.touched
			if len(shown) > touchMapShow {
				shown = shown[:touchMapShow]
			}
			edited, uncommitted, countW := 0, 0, 0
			for _, t := range d.touched {
				if t.Wrote() {
					edited++
					if dirty[t.Path] {
						uncommitted++
					}
				}
			}
			for _, t := range shown {
				if w := lipgloss.Width(touchCountLabel(t.Touches())); w > countW {
					countW = w
				}
			}
			summary := segB(ice, fmt.Sprintf("%d touched", len(d.touched))) + seg(muted, fmt.Sprintf("  ·  %d edited", edited))
			if uncommitted > 0 {
				summary += seg(yellow, fmt.Sprintf("  ·  %d uncommitted", uncommitted))
			}
			rows = append(rows, line(label("files")+summary))

			// fixed columns: action | path | count | age | flag, so it reads as a table
			const actionW, ageW, tagW = 5, 4, 11
			pathW := max(10, width-len(indent)-actionW-2-countW-2-ageW-2-tagW)
			for _, t := range shown {
				action, actionC, pathC := "read", muted, muted
				if t.Wrote() {
					action, actionC, pathC = "edit", claudeC, ice
				}
				tag := ""
				if t.Wrote() && dirty[t.Path] {
					tag = "uncommitted"
				}
				row := seg(muted, indent) +
					seg(actionC, fmt.Sprintf("%-*s", actionW, action)) +
					renderTouchedPath(t.Path, pathC, pathW) +
					seg(muted, fmt.Sprintf("  %*s  %*s  ", countW, touchCountLabel(t.Touches()), ageW, relTime(t.Last))) +
					seg(yellow, tag)
				rows = append(rows, line(row))
			}
			if len(d.touched) > touchMapShow {
				rows = append(rows, line(seg(muted, indent+fmt.Sprintf("… and %d more", len(d.touched)-touchMapShow))))
			}
			rows = append(rows, line(seg(muted, indent+"press ")+seg(blue, "f")+seg(muted, " to open or diff these files")))
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
			rows = append(rows, line(detailSectionIndent+rail))
			continue
		}
		subjW := max(10, width-lipgloss.Width(detailSectionIndent)-railW-1-8-1-13-1-15-1)
		rows = append(rows, line(
			detailSectionIndent+rail+" "+
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

// contextStatusValue describes which instruction files the agent loads here, as
// the value for the detail page's "context" label (no leading label of its own).
func contextStatusValue(instr instrState) string {
	switch {
	case instr.hasClaude && instr.hasAgents && instr.imports:
		return segB(green, "ready") + seg(muted, " · ") + seg(ice, "CLAUDE.md + AGENTS.md") + seg(muted, "  (full project notes)")
	case instr.hasClaude && instr.hasAgents:
		return segB(yellow, "partial") + seg(muted, " · ") + seg(ice, "CLAUDE.md loaded") + seg(muted, " · ") + seg(yellow, "AGENTS.md not loaded")
	case instr.hasClaude:
		return segB(green, "ready") + seg(muted, " · ") + seg(ice, "CLAUDE.md") + seg(muted, "  (no AGENTS.md)")
	case instr.hasAgents:
		return segB(yellow, "none") + seg(muted, " · AGENTS.md exists but Claude does not read it")
	default:
		return segB(yellow, "none") + seg(muted, " · no CLAUDE.md or AGENTS.md")
	}
}

func compactTouchedPath(path string) string {
	path = strings.ReplaceAll(cleanText(path), "\\", "/")
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

// renderTouchedPath renders a touched file path padded to exactly width, with a
// dim directory and a bright basename, so the touch-map rows line up as columns.
func renderTouchedPath(path, color string, width int) string {
	p := fitLeft(compactTouchedPath(path), width)
	dir, base := splitDirBase(p)
	out := seg(color, p)
	if base != "" {
		out = seg(muted, dir) + segB(color, base)
	}
	if pad := width - lipgloss.Width(p); pad > 0 {
		out += seg(muted, strings.Repeat(" ", pad))
	}
	return out
}

func touchCountLabel(n int) string {
	if n == 1 {
		return "1 touch"
	}
	return fmt.Sprintf("%d touches", n)
}

type wtGroup struct {
	label string
	badge string
	color string
	files []string
}

// dirtyPathSet is the set of repo-relative paths with uncommitted changes, taken
// from `git status --porcelain`, so the touch map can flag files Claude edited
// that are not yet committed.
func dirtyPathSet(lines []string) map[string]bool {
	set := make(map[string]bool, len(lines))
	// git C-quotes paths with spaces, tabs, quotes, backslashes or non-ASCII bytes
	// ("a\tb.txt", "utf\303\251.txt"); strconv.Unquote reverses that so the key
	// matches the raw path TouchMap reads from the transcript.
	unq := func(p string) string {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) {
			if uq, err := strconv.Unquote(p); err == nil {
				return uq
			}
			return strings.Trim(p, `"`)
		}
		return p
	}
	for _, l := range lines {
		if len(l) < 4 {
			continue
		}
		p := strings.TrimSpace(l[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			set[unq(p[:i])] = true   // rename source (the agent may have edited the old name)
			set[unq(p[i+4:])] = true // rename destination (the live path)
			continue
		}
		set[unq(p)] = true
	}
	return set
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
		cmdHint("c", "claude"), cmdHint("C", "resume"), cmdHint("H", "sessions"), cmdHint("f", "files"),
		cmdHint("M", "commit msg"), cmdHint("I", "wire md"), cmdHint("d", "diff"), cmdHint("b", "branch"),
		cmdHint("p", "pull"), cmdHint("e", "editor"), cmdHint("O", "browser"), cmdHint("y", "copy path"),
	}
	joined := strings.Join(full, "")
	if lipgloss.Width(joined) > width {
		joined = strings.Join([]string{
			cmdHint("esc", "back"), cmdHint("c", "claude"), cmdHint("d", "diff"), cmdHint("y", "copy path"),
			cmdHint("M", "commit msg"), cmdHint("b", "branch"),
		}, "")
	}
	hints := fillLine(joined, width, bg)

	rows := []string{
		topLine,
		rule,
		m.detailVP.View(),
		rule,
	}
	if m.status != "" {
		rows = append(rows, fillLine(statusStyle.Render("  "+m.status), width, bg))
	}
	rows = append(rows, hints)
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
