package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
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

// touchMapSessions is how many recent transcripts the touch map scans.
const touchMapSessions = 8

const detailSectionIndent = "    "

type detailState struct {
	repo          repo.Repo
	info          orchardgit.DetailInfo
	langs         []lang.Stat
	sessions      []claude.Session     // recent Claude Code sessions in this repo
	commitsSince  int                  // commits since Claude last ran here (stale-context hint)
	touched       []claude.TouchedFile // files Claude read/edited here (touch map)
	codexSessions []claude.Session     // recent Codex sessions in this repo
	codexTouched  []claude.TouchedFile // files Codex edited here (patch map)
	err           string
}

func (m model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "enter", "backspace":
		m.rememberWorkspaceView()
		m.mode = modeList
		m.detail = nil
		m.status = ""
		return m, nil
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
		return m.openAgentLauncher([]repo.Repo{m.repoByPath(m.detailRepo)})
	case "C":
		return m.openClaudeResume(m.repoByPath(m.detailRepo))
	case "H":
		return m.openSessions(m.repoByPath(m.detailRepo))
	case "f":
		return m.openTouched(m.repoByPath(m.detailRepo))
	case "v":
		return m.openPreview(m.repoByPath(m.detailRepo))
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
	case "T":
		return m.openStats()
	case "L":
		return m.openWorklog()
	case "R":
		return m.openSessionSearch()
	case "S":
		return m.openSearch()
	case "?":
		return m.openHelp()
	}
	return m, nil
}

func (m model) openDetail() (tea.Model, tea.Cmd) {
	r, ok := m.currentRepo()
	if !ok {
		return m, nil
	}
	return m.activateWorkspaceRepo(r.Path)
}

func detailCmd(r repo.Repo, requests ...uint64) tea.Cmd {
	var request uint64
	if len(requests) > 0 {
		request = requests[0]
	}
	if demoMode() {
		return func() tea.Msg {
			return detailMsg{request: request, path: r.Path, info: demoDetail(r), langs: demoDetailLangs(r.Path), sessions: demoSessions(), commitsSince: 14, touched: demoTouched(), codexSessions: demoCodexSessions(), codexTouched: demoCodexTouched()}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := orchardgit.Detail(ctx, r)
		sessions := claude.Sessions(r.Path, 10)
		return detailMsg{request: request, path: r.Path, info: info, langs: lang.Detect(ctx, r.Path), sessions: sessions, commitsSince: commitsSinceClaude(ctx, r.Path, sessions), touched: claude.TouchMap(r.Path, touchMapSessions), codexSessions: codex.Sessions(r.Path, 10), codexTouched: codex.TouchMap(r.Path, touchMapSessions), err: err}
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
	if m.mode != modeDetail {
		return
	}
	offset := m.detailVP.YOffset
	m.detailVP.SetContent(m.detailBody(m.detailVP.Width))
	m.detailVP.SetYOffset(offset)
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
	const summaryLabelW = 13
	summaryLabel := func(name string) string {
		return segB(muted, fmt.Sprintf("%s%-*s", detailSectionIndent, summaryLabelW, name))
	}

	// Lead with the answers people most often need before they act. This keeps
	// repository health and the technology stack above the denser AI/history
	// sections, and avoids making a clean tree consume a whole section later.
	rows = append(rows, line(sectionHeading(blue, "", "Project", "")))
	dirtyCount := len(d.info.StatusLines)
	if dirtyCount == 0 {
		rows = append(rows, line(summaryLabel("working tree")+segB(green, "● clean")+seg(muted, "  ·  no uncommitted changes")))
	} else {
		rows = append(rows, line(summaryLabel("working tree")+segB(yellow, fmt.Sprintf("◐ %d change%s", dirtyCount, pluralSuffix(dirtyCount)))+seg(muted, "  ·  review before launching or switching branches")))
	}

	// Languages (dominant first, with icon + share) form the stack summary.
	if len(d.langs) > 0 {
		parts := summaryLabel("stack")
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
		rows = append(rows, line(parts))
	}

	instr, hasInstr := m.instructionsByPath[m.detailRepo]
	if attention := m.detailAttention(d, instr, hasInstr); len(attention) > 0 {
		rows = append(rows, blank, line(sectionHeading(yellow, "", fmt.Sprintf("Attention  ·  %d", len(attention)), "")))
		titleW := clamp(width/3, 22, 40)
		for i, item := range attention {
			prefix := fmt.Sprintf("%s%d.  ", detailSectionIndent, i+1)
			available := max(10, width-lipgloss.Width(prefix)-titleW-2)
			rows = append(rows, line(
				segB(yellow, prefix)+
					seg(ice, padRight(item.title, titleW))+seg(muted, "  ")+
					renderDetailAction(fit(item.action, available))))
		}
	}

	// Keep the landing page to one line per assistant. Session titles and touched
	// files already have dedicated H and f views, so repeating them here made the
	// page read like a transcript instead of a repository summary.
	rows = append(rows, blank, line(sectionHeading(blue, "", "AI activity", "")))
	metricSep := seg(muted, "  ·  ")
	activity := func(name, color string, sessions []claude.Session) string {
		var turns, tokens int
		var last time.Time
		for _, s := range sessions {
			turns += s.Assistant
			tokens += s.Tokens
			if s.Modified.After(last) {
				last = s.Modified
			}
		}
		return segB(color, fmt.Sprintf("%s%-*s", detailSectionIndent, summaryLabelW, name)) +
			seg(ice, fmt.Sprintf("%d session%s", len(sessions), pluralSuffix(len(sessions)))) + metricSep +
			seg(ice, fmt.Sprintf("%d turns", turns)) + metricSep +
			seg(muted, humanTokens(tokens)+" tokens") + metricSep +
			seg(muted, "last "+relTime(last)+" ago")
	}
	switch {
	case len(d.sessions) == 0 && len(d.codexSessions) == 0:
		rows = append(rows, line(seg(muted, detailSectionIndent+"No Claude or Codex sessions yet")))
	default:
		if len(d.sessions) > 0 {
			rows = append(rows, line(activity("Claude", claudeC, d.sessions)))
		}
		if len(d.codexSessions) > 0 {
			rows = append(rows, line(activity("Codex", codexC, d.codexSessions)))
		}
	}

	// A clean tree is already summarized above. Dirty files stay near the top,
	// immediately after the recommended actions, where they cannot be mistaken
	// for low-priority history.
	if dirtyCount > 0 {
		header(iconWarn, fmt.Sprintf("Working tree  ·  %d change%s", dirtyCount, pluralSuffix(dirtyCount)))
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

	// Commit history is omitted when unavailable so an empty heading does not
	// look like missing or still-loading data.
	if len(d.info.Graph) > 0 {
		header(iconCommit, "Recent commits")
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
	}

	// Remotes are useful metadata, but an empty section only adds visual noise.
	if len(d.info.Remotes) > 0 {
		header(iconRemote, "Remotes")
		for _, rem := range d.info.Remotes {
			rows = append(rows, line(seg(blue, "    "+fit(rem, max(10, width-6)))))
		}
	}
	return strings.Join(rows, "\n")
}

type detailAttentionItem struct {
	title  string
	action string
}

func (m model) detailAttention(d *detailState, instr instrState, instrKnown bool) []detailAttentionItem {
	var items []detailAttentionItem
	add := func(title, action string) {
		items = append(items, detailAttentionItem{title: title, action: action})
	}
	if m.assistantCmd == "" {
		add("No AI assistant configured", "install Claude/Codex or set ORCHARD_AI_CMD")
	}
	if instrKnown {
		switch {
		case instr.canWire():
			add("AGENTS.md isn't loaded by Claude", "press I to wire it automatically")
		case instr.hasClaude && instr.hasAgents && !instr.imports:
			add("AGENTS.md isn't loaded by Claude", "add @AGENTS.md to CLAUDE.md")
		case instr.blind():
			add("No project instructions", "add CLAUDE.md or AGENTS.md")
		}
		if instr.claudeBytes > claudeMDLargeBytes {
			add(fmt.Sprintf("CLAUDE.md is large (%dKB)", instr.claudeBytes/1000), "trim or split it to reduce launch context")
		}
	}
	if d.commitsSince >= staleCommitThreshold {
		add("Claude's session context may be stale", fmt.Sprintf("review %d commits since it last ran", d.commitsSince))
	}
	if n := dirtyAIEditsCount(d); n > 0 {
		add(fmt.Sprintf("%d AI-edited file%s uncommitted", n, pluralSuffix(n)), "press d to review the changes")
	}
	return items
}

func dirtyAIEditsCount(d *detailState) int {
	dirty := dirtyPathSet(d.info.StatusLines)
	paths := map[string]bool{}
	for _, t := range d.touched {
		if t.Wrote() && dirty[t.Path] {
			paths[t.Path] = true
		}
	}
	for _, t := range d.codexTouched {
		if dirty[t.Path] {
			paths[t.Path] = true
		}
	}
	return len(paths)
}

// renderDetailAction highlights a shortcut in instructions such as
// "press d to review…" while leaving the supporting text visually quiet.
func renderDetailAction(action string) string {
	const prefix = "press "
	i := strings.Index(action, prefix)
	if i < 0 {
		return seg(muted, action)
	}
	keyAt := i + len(prefix)
	if keyAt >= len(action) {
		return seg(muted, action)
	}
	return seg(muted, action[:keyAt]) + segB(blue, action[keyAt:keyAt+1]) + seg(muted, action[keyAt+1:])
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
			if i := strings.Index(path, " -> "); i >= 0 {
				path = path[i+4:] // show the destination, mirroring dirtyPathSet
			}
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
	// Pack as many hints as fit, in priority order, so commands are never dropped
	// wholesale and newly added keys stay visible. esc/scroll lead; least-used trail.
	hints := fillLine(packHints(width, []string{
		cmdHint("esc", "back"), cmdHint("↑↓", "scroll"),
		cmdHint("c", "launch"), cmdHint("C", "resume"), cmdHint("H", "sessions"),
		cmdHint("f", "files"), cmdHint("v", "docs"), cmdHint("d", "diff"),
		cmdHint("M", "commit msg"), cmdHint("I", "wire md"), cmdHint("b", "branch"),
		cmdHint("p", "pull"), cmdHint("e", "editor"), cmdHint("O", "browser"), cmdHint("y", "copy path"),
	}, []string{cmdHint("?", "help")}), width, bg)

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
