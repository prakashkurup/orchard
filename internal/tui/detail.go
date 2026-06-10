package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/graph"
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
	graph        graph.GraphState     // code-graph snapshot (zero when graphOK is false)
	graphOK      bool                 // a non-empty code graph has been built
	graphMap     []graph.MapRow       // top-ranked symbols (repo map), for the detail view
	err          string
}

// loadGraph reads the code-graph snapshot and top symbols for a repo (read-only;
// builds nothing). Returns the state, ok, and the repo map.
func loadGraph(repoAbs string) (graph.GraphState, bool, []graph.MapRow) {
	st, ok := graph.StateFor(repoAbs)
	if !ok {
		return graph.GraphState{}, false, nil
	}
	var top []graph.MapRow
	if g, err := graph.OpenForRepo(repoAbs); err == nil {
		top, _ = g.RepoMap(6)
		if stale, changed, err := g.Stale(context.Background(), repoAbs); err == nil {
			st.Stale = stale
			st.Changed = changed
		}
		g.Close()
	}
	return st, true, top
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
	case "B":
		nm, cmd := m.startGraphBuild([]repo.Repo{m.repoByPath(m.detailRepo)})
		nm.setDetailContent() // show "building code graph…" in the section immediately
		return nm, cmd
	case "D":
		nm, cmd := m.deleteGraph([]repo.Repo{m.repoByPath(m.detailRepo)})
		if nm.detail != nil {
			nm.detail.graph, nm.detail.graphOK, nm.detail.graphMap = loadGraph(nm.detailRepo)
			nm.setDetailContent() // section reverts to "not built"
		}
		return nm, cmd
	case "m":
		return m.toggleGraphWiring()
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
		gst, gok, gmap := loadGraph(r.Path)
		return detailMsg{path: r.Path, info: info, langs: lang.Detect(ctx, r.Path), sessions: sessions, commitsSince: commitsSinceClaude(ctx, r.Path, sessions), touched: claude.TouchMap(r.Path, touchMapSessions), graph: gst, graphOK: gok, graphMap: gmap, err: err}
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

	instr, hasInstr := m.instructionsByPath[m.detailRepo]
	rows = append(rows, m.aiReadinessRows(d, instr, hasInstr, width, line, sectionHeading)...)

	// Code graph - the queryable symbol/edge graph orchard serves to Claude/Codex
	// over MCP (build or refresh with B); fresh means built at the current HEAD on
	// a clean tree, stale means HEAD moved or the tree is dirty.
	{
		sep := seg(muted, "  ·  ")
		rows = append(rows, blank, line(sectionHeading(blue, iconCommit, "Code graph", "")))
		switch {
		case m.graphBuilding && m.graphBuildingPath == d.repo.Path:
			rows = append(rows, line(seg(accent, detailSectionIndent+m.spinner.View()+" building code graph…")))
		case !d.graphOK:
			rows = append(rows, line(seg(muted, detailSectionIndent+"not built — press ")+seg(blue, "B")+seg(muted, " to build (served to Claude / Codex over MCP)")))
			if nudge := graphSetupNudge(d); nudge != "" {
				rows = append(rows, line(seg(muted, detailSectionIndent)+seg(yellow, nudge)))
			}
		default:
			g := d.graph
			reasons := graphStaleReasons(d)
			stale := len(reasons) > 0
			freshTag := segB(green, "● fresh")
			if stale {
				freshTag = segB(yellow, "◐ "+strings.Join(reasons, " · "))
			}
			rows = append(rows, line(seg(muted, detailSectionIndent)+
				seg(ice, fmt.Sprintf("%d files", g.Files))+sep+
				seg(ice, fmt.Sprintf("%d symbols", g.Symbols))+sep+
				seg(ice, fmt.Sprintf("%d edges", g.Edges))))
			if trust := graphTrustSummary(g.Trust); trust != "" {
				rows = append(rows, line(seg(muted, detailSectionIndent+"trust ")+seg(ice, trust)))
			} else if quality := graphQualitySummary(g.Tiers); quality != "" {
				rows = append(rows, line(seg(muted, detailSectionIndent+"trust ")+seg(ice, quality)))
			}
			if nudge := graphSetupNudge(d); nudge != "" {
				rows = append(rows, line(seg(muted, detailSectionIndent)+seg(yellow, nudge)))
			}
			meta := seg(muted, detailSectionIndent) + freshTag
			if !g.BuiltAt.IsZero() {
				meta += seg(muted, fmt.Sprintf("   built %s ago", relTime(g.BuiltAt)))
			}
			if len(g.HeadCommit) >= 7 {
				meta += seg(muted, "   @ "+g.HeadCommit[:7])
			}
			rows = append(rows, line(meta))
			if stale {
				rows = append(rows, line(seg(muted, detailSectionIndent+"press ")+seg(blue, "B")+seg(muted, " to rebuild at the current HEAD")))
			}
			if len(d.graphMap) > 0 {
				rows = append(rows, line(seg(muted, detailSectionIndent+"top symbols")))
				for _, e := range d.graphMap {
					rows = append(rows, line(seg(muted, detailSectionIndent+"  ")+
						seg(teal, fmt.Sprintf("%-6s", e.Kind))+
						seg(ice, " "+e.Name)+
						seg(muted, "  "+fit(e.Path, max(10, width-len(detailSectionIndent)-30)))))
				}
			}
		}
	}

	// Claude Code - everything about the agent in one place, laid out as labeled
	// rows (activity / context / sessions / files) with whitespace between the
	// clusters, so a newcomer can scan what each line means and what to act on.
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

type aiReadyLevel uint8

const (
	aiReadyOK aiReadyLevel = iota
	aiReadyWarn
	aiReadyBlock
)

type aiReadyCard struct {
	level   aiReadyLevel
	label   string
	signals []string
	fixes   []string
}

func (m model) aiReadinessRows(d *detailState, instr instrState, instrKnown bool, width int, line func(string) string, heading func(string, string, string, string) string) []string {
	card := m.aiReadyCard(d, instr, instrKnown)
	color := aiReadyColor(card.level)
	indent := detailSectionIndent
	contentW := max(10, width-lipgloss.Width(indent))

	rows := []string{
		line(""),
		line(heading(color, "", "AI readiness", "   "+aiReadyBadge(card.level)+" "+card.label)),
		line(seg(muted, indent) + seg(ice, fit(strings.Join(card.signals, "  ·  "), contentW))),
	}
	if len(card.fixes) == 0 {
		rows = append(rows, line(seg(muted, indent+"next ")+seg(green, "launch safely with c")))
		return rows
	}
	for i, fix := range card.fixes {
		if i >= 4 {
			rows = append(rows, line(seg(muted, indent+"next ")+seg(yellow, fmt.Sprintf("and %d more checks", len(card.fixes)-i))))
			break
		}
		rows = append(rows, line(seg(muted, indent+"next ")+seg(yellow, fit(fix, contentW-5))))
	}
	return rows
}

func (m model) aiReadyCard(d *detailState, instr instrState, instrKnown bool) aiReadyCard {
	card := aiReadyCard{level: aiReadyOK, label: "ready to launch"}
	add := func(signal string) { card.signals = append(card.signals, signal) }
	fix := func(level aiReadyLevel, signal, action string) {
		add(signal)
		card.fixes = append(card.fixes, action)
		if level > card.level {
			card.level = level
		}
	}

	switch {
	case m.graphBuilding && m.graphBuildingPath == d.repo.Path:
		fix(aiReadyWarn, "graph building", "wait for graph build to finish")
	case !d.graphOK:
		fix(aiReadyWarn, "graph never built", "press B to build the code graph")
	case len(graphStaleReasons(d)) == 0:
		add("graph fresh")
	default:
		reasons := strings.Join(graphStaleReasons(d), " · ")
		fix(aiReadyWarn, "graph "+reasons, "press B to rebuild at the current HEAD")
	}

	if d.graphOK {
		add("trust " + graphTrustForReadiness(d.graph))
	} else {
		add("trust pending")
	}
	if nudge := graphSetupNudge(d); nudge != "" {
		fix(aiReadyWarn, "ast-grep missing", nudge)
	}

	switch {
	case m.assistantCmd == "":
		fix(aiReadyBlock, "MCP unavailable", "install Claude/Codex or set ORCHARD_AI_CMD")
	case m.assistantIsClaude() || m.assistantIsCodex():
		name := m.assistantLabel
		if name == "" {
			name = "agent"
		}
		if m.graphWireSuppressed() {
			fix(aiReadyWarn, "MCP wiring off", "press m to enable graph MCP wiring")
		} else {
			add("MCP auto-wires " + name)
		}
	default:
		name := m.assistantLabel
		if name == "" {
			name = "assistant"
		}
		fix(aiReadyWarn, "MCP not supported by "+name, "use Claude or Codex for graph-aware launches")
	}

	switch {
	case !instrKnown:
		add("context checking")
	case instr.canWire():
		fix(aiReadyWarn, "AGENTS.md not loaded", "press I to create CLAUDE.md importing @AGENTS.md")
	case instr.hasClaude && instr.hasAgents && !instr.imports:
		fix(aiReadyWarn, "AGENTS.md not loaded", "add @AGENTS.md to CLAUDE.md")
	case instr.blind():
		fix(aiReadyWarn, "no project notes", "add CLAUDE.md or AGENTS.md before launching")
	case instr.claudeBytes > claudeMDLargeBytes:
		fix(aiReadyWarn, "context large", "trim or split CLAUDE.md to reduce launch context")
	default:
		add("context ready")
	}
	if d.commitsSince >= staleCommitThreshold {
		fix(aiReadyWarn, "session context old", fmt.Sprintf("review %d commits since Claude last ran", d.commitsSince))
	}

	if n := dirtyAITouchedCount(d); n > 0 {
		fix(aiReadyWarn, fmt.Sprintf("%d AI edit%s uncommitted", n, pluralSuffix(n)), "review or commit Claude-edited dirty files")
	} else if len(d.touched) > 0 {
		add("AI edits clean")
	} else {
		add("AI edits none")
	}

	if card.level == aiReadyWarn {
		card.label = "needs attention"
	} else if card.level == aiReadyBlock {
		card.label = "blocked"
	}
	return card
}

func graphStaleReasons(d *detailState) []string {
	if !d.graphOK {
		return []string{"never built"}
	}
	var reasons []string
	if d.graph.HeadCommit != "" && d.repo.Head != "" && d.graph.HeadCommit != d.repo.Head {
		reasons = append(reasons, "HEAD moved")
	}
	if d.repo.Dirty {
		reasons = append(reasons, "dirty tree")
	} else if d.graph.DirtyFiles > 0 {
		reasons = append(reasons, "built from dirty tree")
	}
	if d.graph.Changed > 0 {
		reasons = append(reasons, fmt.Sprintf("%d file%s changed", d.graph.Changed, pluralSuffix(d.graph.Changed)))
	} else if d.graph.Stale {
		reasons = append(reasons, "files changed")
	}
	return reasons
}

func dirtyAITouchedCount(d *detailState) int {
	dirty := dirtyPathSet(d.info.StatusLines)
	var n int
	for _, t := range d.touched {
		if t.Wrote() && dirty[t.Path] {
			n++
		}
	}
	return n
}

func graphTrustForReadiness(st graph.GraphState) string {
	if s := graphTrustSummary(st.Trust); s != "" {
		return s
	}
	if s := graphQualitySummary(st.Tiers); s != "" {
		return s
	}
	return "unknown"
}

func graphSetupNudge(d *detailState) string {
	if graph.ASTGrepAvailable() || !repoNeedsASTGrep(d.langs) {
		return ""
	}
	if repoHasLang(d.langs, "Go") {
		return "ast-grep missing · press B to build Go only · run orchard graph install-ast-grep for full graph"
	}
	return "ast-grep missing · run orchard graph install-ast-grep for full graph"
}

func aiReadyColor(level aiReadyLevel) string {
	switch level {
	case aiReadyBlock:
		return red
	case aiReadyWarn:
		return yellow
	default:
		return green
	}
}

func aiReadyBadge(level aiReadyLevel) string {
	switch level {
	case aiReadyBlock:
		return "×"
	case aiReadyWarn:
		return "◐"
	default:
		return "●"
	}
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

func graphQualitySummary(tiers map[graph.Tier]int) string {
	if len(tiers) == 0 {
		return ""
	}
	var parts []string
	for _, tier := range []graph.Tier{graph.TierPrecise, graph.TierGood, graph.TierBestEffort, graph.TierUnsupported} {
		if n := tiers[tier]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", tier, n))
		}
	}
	return strings.Join(parts, " · ")
}

func graphTrustSummary(trust []graph.LangTrust) string {
	if len(trust) == 0 {
		return ""
	}
	shown := trust
	if len(shown) > 5 {
		shown = shown[:5]
	}
	parts := make([]string, 0, len(shown)+1)
	for _, t := range shown {
		parts = append(parts, fmt.Sprintf("%s: %s", displayGraphLang(t.Lang), t.Tier))
	}
	if len(trust) > len(shown) {
		parts = append(parts, fmt.Sprintf("+%d more", len(trust)-len(shown)))
	}
	return strings.Join(parts, " · ")
}

func displayGraphLang(lang string) string {
	switch lang {
	case "go":
		return "Go"
	case "python":
		return "Python"
	case "ruby":
		return "Ruby"
	case "java":
		return "Java"
	case "kotlin":
		return "Kotlin"
	case "csharp":
		return "C#"
	case "typescript", "tsx":
		return "TypeScript"
	case "javascript":
		return "JavaScript"
	case "c":
		return "C"
	case "cpp":
		return "C++"
	default:
		return lang
	}
}

func repoNeedsASTGrep(langs []lang.Stat) bool {
	for _, l := range langs {
		if graph.ASTGrepSupports(graphLangLabel(l.Name)) {
			return true
		}
	}
	return false
}

func graphLangLabel(name string) string {
	switch name {
	case "C#":
		return "csharp"
	case "C++":
		return "cpp"
	case "TypeScript":
		return "typescript"
	case "JavaScript":
		return "javascript"
	default:
		return strings.ToLower(name)
	}
}

func repoHasLang(langs []lang.Stat, name string) bool {
	for _, l := range langs {
		if l.Name == name {
			return true
		}
	}
	return false
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
		cmdHint("c", "claude"), cmdHint("C", "resume"), cmdHint("H", "sessions"),
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
	if m.graphBuilding {
		spin := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).
			Render("  " + m.spinner.View() + " " + m.status)
		rows = append(rows, fillLine(spin, width, bg))
	} else if m.status != "" {
		rows = append(rows, fillLine(statusStyle.Render("  "+m.status), width, bg))
	}
	rows = append(rows, hints)
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
