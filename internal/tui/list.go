package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"strings"
	"time"
)

func (m model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Konami code makes the orchard bloom; arrows still move the cursor normally.
	if m.pushKonami(msg.String()) {
		m.bloomFrames = bloomFrames
		return m, bloomTickCmd()
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "Z":
		// hidden: start the idle screensaver now (any key dismisses it)
		m.ssActive, m.ssFrame, m.idle = true, 0, 0
		m.idleGen++
		return m, idleTickCmd(ssFrameDur, m.idleGen)
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-max(1, m.viewport.Height))
	case "pgdown":
		m.moveCursor(max(1, m.viewport.Height))
	case "g", "home":
		m.cursorToEdge(true)
	case "G", "end":
		m.cursorToEdge(false)
	case "enter":
		return m.openDetail()
	case " ":
		m.toggleCurrent()
	case "a":
		m.selectAllVisible()
	case "x":
		m.selected = map[string]bool{}
		m.status = "selection cleared"
	case "r":
		m.loading = true
		m.err = ""
		m.status = tendingLine()
		return m, scanCmd(m.root, m.concurrency)
	case "w":
		m.autoRefresh = !m.autoRefresh
		if m.autoRefresh {
			m.status = "auto-refresh on"
		} else {
			m.status = "auto-refresh off"
		}
	case "n":
		m.jumpToNextNew()
	case "+":
		m.mode = modeClone
		m.cloneInput.SetValue("")
		m.status = ""
		return m, m.cloneInput.Focus()
	case "L":
		m.mode = modeWorklog
		if m.worklogWindow == "" {
			m.worklogWindow = "1 day ago"
		}
		m.detailVP.SetContent(fillLine(subtleStyle.Render("  building worklog…"), m.detailVP.Width, bg))
		m.detailVP.GotoTop()
		return m, worklogCmd(m.repos, m.worklogWindow)
	case "?":
		m.mode = modeHelp
		m.detailVP.SetContent(m.helpBody(m.detailVP.Width))
		m.detailVP.GotoTop()
		return m, nil
	case "/":
		m.filtering = true
		m.filterInput.SetValue(m.filterText)
		m.filterInput.CursorEnd()
		m.syncRows()
		return m, m.filterInput.Focus()
	case "tab":
		m.quick = (m.quick + 1) % filterCount
		m.status = "filter: " + m.quick.String()
		m.rebuildView()
	case "s":
		m.sortMode = (m.sortMode + 1) % sortModeCount
		m.status = "sort: " + m.sortMode.String()
		m.rebuildView()
	case "o":
		m.grouped = !m.grouped
		if m.grouped {
			m.status = "grouped by state"
		} else {
			m.status = "flat list"
		}
		m.rebuildView()
	case "p":
		cmd := m.startPull(m.pullTargets())
		m.syncRows()
		return m, cmd
	case "P":
		cmd := m.startPull(m.eligibleAll())
		m.syncRows()
		return m, cmd
	case "f":
		cmd := m.startFetch(m.pullTargets())
		m.syncRows()
		return m, cmd
	case "F":
		cmd := m.startFetch(m.eligibleAll())
		m.syncRows()
		return m, cmd
	case "O":
		return m.requestBrowser(m.selectionTargets())
	case "c":
		return m.requestClaude(m.selectionTargets())
	case "C":
		if r, ok := m.currentRepo(); ok {
			return m.openClaudeResume(r)
		}
	case "A":
		if len(m.selected) < 2 {
			m.status = "select 2+ repos with space, then A for one Claude session across them"
			return m, nil
		}
		return m.openClaudeCombined(m.selectionTargets())
	case "H":
		if r, ok := m.currentRepo(); ok {
			return m.openSessions(r)
		}
	case "M":
		if r, ok := m.currentRepo(); ok {
			return m.openClaudeCommitMessage(r)
		}
	case "d":
		if r, ok := m.currentRepo(); ok {
			return m.openDiff(r)
		}
	case "b":
		if r, ok := m.currentRepo(); ok {
			return m.openBranchSwitcher(r)
		}
	case "e":
		if r, ok := m.currentRepo(); ok {
			return m.openEditor(r, false)
		}
	case "E":
		if r, ok := m.currentRepo(); ok {
			return m.openEditor(r, true)
		}
	case "S":
		m.mode = modeSearch
		m.searchFocus = true
		m.searchResults = nil
		m.searchFlat = nil
		m.searchCursor = 0
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.setSearchContent()
		return m, m.searchInput.Focus()
	}
	m.syncRows()
	return m, nil
}

func (m model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filtering = false
		m.filterText = ""
		m.filterInput.SetValue("")
		m.filterInput.Blur()
		m.rebuildView()
		m.syncRows()
		return m, nil
	case "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.syncRows()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filterText = m.filterInput.Value()
	m.rebuildView()
	m.syncRows()
	return m, cmd
}

func (m *model) syncRows() {
	m.rebuildView()
	m.viewport.SetContent(m.renderGrid(m.viewport.Width))
}

func (m model) headerView(width int) string {
	top := m.wordmark()

	avail := max(10, width-lipgloss.Width(top)-1)
	var statusStyled string
	switch {
	case m.err != "":
		statusStyled = errorStyle.Render(fit(iconWarn+" "+m.err, avail))
	case m.loading:
		statusStyled = lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).
			Render(fit(m.spinnerOrSync()+" "+m.status, avail))
	case m.status != "":
		statusStyled = statusStyle.Render(fit(m.status, avail))
	}
	gap := max(1, width-lipgloss.Width(top)-lipgloss.Width(statusStyled))
	topLine := top + fillLine("", gap, bg) + statusStyled

	root := subtleStyle.Render(iconFolder + " " + fit(displayRoot(m.root), max(10, width/2)))
	modes := subtleStyle.Render(m.modeIndicators())
	gap2 := max(1, width-lipgloss.Width(root)-lipgloss.Width(modes))
	rootLine := root + fillLine("", gap2, bg) + modes

	rule := hrule(width)
	if m.bloomFrames > 0 {
		rule = bloom(width, bloomFrames-m.bloomFrames)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(topLine, width, bg),
		fillLine(rootLine, width, bg),
		rule,
	)
}

func (m model) spinnerOrSync() string {
	if len(m.pulling) > 0 {
		return m.spinner.View()
	}
	return iconSync
}

func (m model) modeIndicators() string {
	parts := []string{"sort " + m.sortMode.String()}
	if m.quick != filterAll {
		parts = append(parts, "filter "+m.quick.String())
	}
	if m.filterText != "" {
		parts = append(parts, "/"+m.filterText)
	}
	if m.grouped {
		parts = append(parts, "grouped")
	}
	// shown/total when a filter is hiding repos
	shown := 0
	for _, it := range m.view {
		if !it.header {
			shown++
		}
	}
	if shown != len(m.repos) {
		parts = append([]string{fmt.Sprintf("%d/%d shown", shown, len(m.repos))}, parts...)
	}
	return strings.Join(parts, "  ·  ")
}

func (m model) metricsView(width int) string {
	stats := countStates(m.repos)
	gap := seg(muted, "    ")
	cards := strings.Join([]string{
		metric(iconFolder, "REPOS", len(m.repos), blue),
		metric(iconCheck, "CLEAN", stats.clean, green),
		metric(iconWarn, "DIRTY", stats.dirty, yellow),
		metric(iconArrowDn, "BEHIND", stats.behind, red),
		metric(iconBolt, "SELECTED", m.selectedCount(), accent),
	}, gap)

	var parts []string
	if g := groveLabel(len(m.repos)); g != "" {
		parts = append(parts, seg(muted, g))
	}
	if m.autoRefresh {
		parts = append(parts, seg(muted, iconSync+" live"))
	}
	if m.isThriving() {
		parts = append(parts, seg(green, "🍎 thriving"))
	}
	right := strings.Join(parts, seg(muted, "  ·  "))
	line := cards
	if right != "" && lipgloss.Width(cards)+lipgloss.Width(right)+2 <= width {
		spacer := fillLine("", width-lipgloss.Width(cards)-lipgloss.Width(right), bg)
		line = cards + spacer + right
	}
	return fillLine(line, width, bg)
}

func metric(icon, label string, value int, color string) string {
	return seg(color, icon+" ") + seg(muted, label+" ") + segB(color, fmt.Sprintf("%d", value))
}

func (m model) gridView(width int) string {
	header := m.gridHeader(width)
	content := m.viewport.View()
	if len(m.view) == 0 && !m.loading && m.err == "" {
		if len(m.repos) == 0 {
			return header + "\n" + m.emptyOrchard(width)
		}
		content = fillLine(subtleStyle.Render("  nothing matches this filter"), width, bg)
	}
	return header + "\n" + content
}

// emptyOrchard is the friendly empty state shown when no repos are found.
func (m model) emptyOrchard(width int) string {
	line := func(s string) string { return fillLine(s, width, bg) }
	leaf := func(s string) string { return seg(green, s) }
	return strings.Join([]string{
		line(""),
		line("     " + leaf(`\ | /`)),
		line("     " + leaf(` \|/`)),
		line("     " + leaf(`  |`)),
		line("   " + seg(muted, "~~~~~~~~~")),
		line(""),
		line("   " + seg(ice, "nothing planted here yet")),
		line("   " + seg(muted, "point orchard at a folder of repos:  orchard --root <folder>")),
	}, "\n")
}

func (m model) gridHeader(width int) string {
	layout := gridLayout(width)
	cells := []string{
		padRight("SEL", layout.sel),
		padRight("ST", layout.st),
		padRight("REPOSITORY", layout.name),
		padRight("BRANCH", layout.branch),
		padRight("LANG", layout.lang),
		padRight("CHANGES", layout.changes),
		padRight("SYNCED", layout.synced),
		padRight("CLAUDE", layout.claude),
		padRight("ACTIVITY", layout.activity),
		padRight("LAST COMMIT / RESULT", layout.info),
	}
	line := strings.Join(cells, "  ")
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ice)).
		Background(lipgloss.Color(panelDark)).
		Bold(true).
		Width(width).
		MaxWidth(width).
		Render(line)
}

func (m model) footerView(width int) string {
	rule := hrule(width)
	var line string
	if m.filtering {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).Render(" / ")
		line = fillLine(prompt+lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(ice)).Render(m.filterInput.View())+subtleStyle.Render("   enter apply · esc clear"), width, bg)
	} else {
		// essentials only - the full keymap + legend lives in the ? help overlay
		hints := []string{
			cmdHint("⏎", "detail"),
			cmdHint("space", "select"),
			cmdHint("x", "clear"),
			cmdHint("p", "pull"),
			cmdHint("b", "branch"),
		}
		if m.assistantCmd != "" {
			hints = append(hints, cmdHint("c", m.assistantLabel))
		}
		hints = append(hints,
			cmdHint("O", "browser"),
			cmdHint("+", "clone"),
			cmdHint("S", "search"),
			cmdHint("/", "filter"),
			cmdHint("?", "help"),
			cmdHint("q", "quit"),
		)
		joined := strings.Join(hints, "")
		if lipgloss.Width(joined) > width {
			joined = strings.Join([]string{
				cmdHint("⏎", "detail"), cmdHint("p", "pull"), cmdHint("?", "help"), cmdHint("q", "quit"),
			}, "")
		}
		line = fillLine(joined, width, bg)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rule, line)
}

func cmdHint(keyName, label string) string {
	key := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true)
	return subtleStyle.Render("[") + key.Render(keyName) + subtleStyle.Render("] "+label+"   ")
}

func (m model) renderGrid(width int) string {
	layout := gridLayout(width)
	lines := make([]string, 0, len(m.view))
	rowIdx := 0
	for vi, it := range m.view {
		if it.header {
			lines = append(lines, groupHeaderLine(it.group, it.count, width))
			continue
		}
		r := m.repos[it.repoIdx]
		current := vi == m.cursor
		alt := rowIdx%2 == 1
		pulling := m.pulling[r.Path]
		lines = append(lines, renderRow(r, m.selected[r.Path], current, alt, pulling, m.spinner.View(), m.newByPath[r.Path], m.langByPath[r.Path], m.ghStatus[r.Path].CIState == "failing", layout))
		rowIdx++
	}
	return strings.Join(lines, "\n")
}

func groupHeaderLine(state repo.DisplayState, count, width int) string {
	label := fmt.Sprintf("%s  %s  (%d)", state.Glyph(), strings.ToUpper(groupTitle(state)), count)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorForState(state))).
		Background(lipgloss.Color(bg)).
		Bold(true).
		Width(width).
		MaxWidth(width).
		Render(" " + label)
}

// renderRows renders a flat list of repo rows (no group headers). Used only by
// tests; production rendering goes through renderGrid.
func renderRows(repos []repo.Repo, selected map[string]bool, cursor, width int) string {
	layout := gridLayout(width)
	lines := make([]string, 0, len(repos))
	for i, r := range repos {
		lines = append(lines, renderRow(r, selected[r.Path], i == cursor, i%2 == 1, false, "", 0, lang.Stat{}, false, layout))
	}
	return strings.Join(lines, "\n")
}

func renderRow(r repo.Repo, selected, current, alt, pulling bool, spin string, newCount int, lng lang.Stat, ghFailing bool, layout gridColumns) string {
	info := r.LastCommit
	if r.SkipReason != "" {
		info = r.SkipReason
	}

	bgColor := panelDark
	if alt {
		bgColor = rowAlt
	}
	if current {
		bgColor = rowHot
	}

	// status glyph (or spinner while pulling)
	stText := statusText(r.Display)
	stColor := colorForState(r.Display)
	if pulling {
		stText = strings.TrimSpace(spin)
		if stText == "" {
			stText = "⠿"
		}
		stColor = accent
		info = "pulling…"
	}

	syncedText := relTime(r.LastFetched)
	syncedColor := freshnessColor(r.LastFetched)
	if r.JustPulled {
		syncedText = "✓ now"
		syncedColor = green
	}

	// "since last visit" - accent dot + name, count badge in the info column
	nameText := r.Name
	nameColor := ice
	if newCount > 0 {
		nameText = "● " + r.Name
		nameColor = accent
	}

	// the selected row gets bright, crisp text so it pops on the indigo bar
	branchCol := branchColor(r.Display)
	if current {
		nameColor = selFg
		branchCol = selFg
	}

	langText, langColor := "·", muted
	if lng.Name != "" {
		glyph := lng.Icon
		if glyph == "" {
			glyph = "●" // fallback for languages without a devicon
		}
		langText, langColor = glyph, lng.Color
	}

	styles := []lipgloss.Style{
		cellStyle(selectionColor(selected), bgColor, current),
		cellStyle(stColor, bgColor, true),
		cellStyle(nameColor, bgColor, current || newCount > 0),
		cellStyle(branchCol, bgColor, current),
		cellStyle(langColor, bgColor, false),
		cellStyle(muted, bgColor, false), // merged git-state (rendered below)
		cellStyle(syncedColor, bgColor, r.JustPulled),
		cellStyle(muted, bgColor, false), // claude session age (rendered below)
		cellStyle(muted, bgColor, false), // activity sparkline (rendered below)
		cellStyle(infoColor(r, pulling), bgColor, false),
	}
	cells := []string{
		padRight(selectionText(selected), layout.sel),
		padRight(stText, layout.st),
		padRight(nameText, layout.name),
		padRight(r.Branch, layout.branch),
		padRight(langText, layout.lang),
		padRight("", layout.changes),
		padRight(syncedText, layout.synced),
		padRight("", layout.claude),
		padRight("", layout.activity),
		padRight(info, layout.info),
	}
	out := make([]string, len(cells))
	for i := range cells {
		out[i] = styles[i].Render(cells[i])
	}
	// merged CHANGES: ahead/behind + working-tree changes + stashes as tokens
	out[5] = gitStateCell(r, layout.changes, bgColor, current)
	// CLAUDE: last session age, flagged red when Claude-edited work is uncommitted
	out[7] = claudeCell(r, layout.claude, bgColor, current)
	// activity sparkline: weekly commit cadence, tinted by recency
	out[8] = sparkline(r.Activity, layout.activity, bgColor, current)
	// info column gets a "↑N new" badge when there are new commits since last
	// visit; otherwise a freshness-coloured commit age + subject (no washed-out
	// grey). The plain fallback (from the loop) covers pulling / skip / no-commit.
	switch {
	case ghFailing && !pulling:
		// failing CI is the most urgent at-a-glance signal, so it takes the info cell
		badge := "× CI "
		rest := fit("· "+info, max(2, layout.info-runewidth.StringWidth(badge)))
		pad := max(0, layout.info-runewidth.StringWidth(badge)-runewidth.StringWidth(rest))
		out[9] = cellStyle(red, bgColor, true).Render(badge) +
			cellStyle(infoColor(r, pulling), bgColor, false).Render(rest+strings.Repeat(" ", pad))
	case newCount > 0 && !pulling:
		badge := fmt.Sprintf("↑%d new ", newCount)
		rest := fit("· "+info, max(2, layout.info-runewidth.StringWidth(badge)))
		pad := max(0, layout.info-runewidth.StringWidth(badge)-runewidth.StringWidth(rest))
		out[9] = cellStyle(accent, bgColor, true).Render(badge) +
			cellStyle(infoColor(r, pulling), bgColor, false).Render(rest+strings.Repeat(" ", pad))
	case !pulling && r.SkipReason == "" && strings.IndexByte(r.LastCommit, '\t') >= 0:
		out[9] = renderInfoCell(r.LastCommit, layout.info, bgColor, current)
	}

	gap := lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render("  ")
	return strings.Join(out, gap)
}

// claudeCell shows how long ago Claude Code last ran in a repo, freshness-tinted
// like SYNCED. A dim dot means no sessions. When the repo is dirty and Claude ran
// recently, it turns red with a leading "!" to flag Claude-edited work that has
// not been committed yet, so AI changes are not lost in an unstaged tree.
func claudeCell(r repo.Repo, width int, bgColor string, current bool) string {
	if r.CCSessions == 0 || r.CCLast.IsZero() {
		return cellStyle(muted, bgColor, false).Render(padRight("·", width))
	}
	text, col := relTime(r.CCLast), freshnessColor(r.CCLast)
	if r.Dirty && time.Since(r.CCLast) < 24*time.Hour {
		text, col = "!"+text, red
	}
	return cellStyle(col, bgColor, current).Render(padRight(text, width))
}

// sparkline renders weekly commit counts as a compact bar chart sized to width.
// Dormant weeks show as faint dots; the most recent third is brighter than older
// weeks so active repos stand out. Every cell is painted on bgColor (no banding).
func sparkline(act []int, width int, bgColor string, current bool) string {
	if width <= 0 {
		return ""
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	data := act
	if len(data) > width {
		data = data[len(data)-width:]
	}
	mx := 0
	for _, v := range data {
		if v > mx {
			mx = v
		}
	}
	var b strings.Builder
	used := 0
	for i, v := range data {
		if v <= 0 {
			b.WriteString(cellStyle(muted, bgColor, false).Render("·"))
			used++
			continue
		}
		idx := clamp((v*(len(bars)-1)+mx-1)/mx, 0, len(bars)-1)
		col := teal
		if i >= len(data)*2/3 { // most recent third
			col = green
		}
		b.WriteString(cellStyle(col, bgColor, current).Render(string(bars[idx])))
		used++
	}
	if used < width {
		b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

// renderInfoCell renders a git "%cr\t%s" last-commit string as a
// freshness-coloured age followed by the subject, padded to width. On the
// selected row the subject is brightened for a crisp highlight.
func renderInfoCell(lastCommit string, width int, bgColor string, current bool) string {
	rel, subject := lastCommit, ""
	if i := strings.IndexByte(lastCommit, '\t'); i >= 0 {
		rel, subject = lastCommit[:i], lastCommit[i+1:]
	}
	subjectCol := muted
	if current {
		subjectCol = selFg
	}
	relPart := fit(rel, width)
	used := runewidth.StringWidth(relPart)
	b := cellStyle(commitAgeColor(rel), bgColor, false).Render(relPart)
	if subject != "" && used+1 < width {
		b += lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(" ")
		used++
		subjPart := fit(subject, width-used)
		b += cellStyle(subjectCol, bgColor, false).Render(subjPart)
		used += runewidth.StringWidth(subjPart)
	}
	if used < width {
		b += lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(strings.Repeat(" ", width-used))
	}
	return b
}

type stateToken struct{ text, color string }

// gitState summarizes a repo's pending work as compact tokens: sync divergence
// (↑ahead ↓behind) then working tree (●changed ≡stashes). Empty when a repo is
// clean and in sync. This merges the old A/B and changes columns into one.
func gitState(r repo.Repo) []stateToken {
	var t []stateToken
	if r.Ahead > 0 {
		t = append(t, stateToken{fmt.Sprintf("↑%d", r.Ahead), green})
	}
	if r.Behind > 0 {
		t = append(t, stateToken{fmt.Sprintf("↓%d", r.Behind), red})
	}
	if r.ChangedFiles > 0 {
		t = append(t, stateToken{fmt.Sprintf("●%d", r.ChangedFiles), yellow})
	}
	if r.Stashes > 0 {
		t = append(t, stateToken{fmt.Sprintf("≡%d", r.Stashes), cyan})
	}
	return t
}

// gitStateCell renders gitState as a width-bounded, multi-colored cell painted on
// bgColor (no banding). A clean, in-sync repo shows a single dim dot.
func gitStateCell(r repo.Repo, width int, bgColor string, current bool) string {
	toks := gitState(r)
	if len(toks) == 0 {
		return cellStyle(muted, bgColor, false).Render(padRight("·", width))
	}
	var b strings.Builder
	used := 0
	for i, tk := range toks {
		w := runewidth.StringWidth(tk.text)
		sep := 0
		if i > 0 {
			sep = 1
		}
		if used+sep+w > width {
			break
		}
		if sep > 0 {
			b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(" "))
			used++
		}
		b.WriteString(cellStyle(tk.color, bgColor, current).Render(tk.text))
		used += w
	}
	if used < width {
		b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(bgColor)).Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

type gridColumns struct {
	sel      int
	st       int
	name     int
	branch   int
	lang     int
	changes  int
	synced   int
	claude   int
	activity int
	info     int
}

func gridLayout(width int) gridColumns {
	// 10 columns => 9 gaps of 2 spaces = 18. CLAUDE shows the last Claude Code
	// session age per repo (and flags Claude-edited-but-uncommitted work).
	const sel, st, lang, changes, synced, claude, activity, gaps = 4, 2, 5, 11, 6, 6, 10, 18
	fixed := sel + st + lang + changes + synced + claude + activity + gaps
	avail := max(24, width-fixed)
	name := clamp(avail*42/100, 16, 36)
	branch := clamp(avail*30/100, 12, 26)
	info := max(10, width-fixed-name-branch)
	return gridColumns{
		sel: sel, st: st, name: name, branch: branch, lang: lang,
		changes: changes, synced: synced, claude: claude, activity: activity, info: info,
	}
}
