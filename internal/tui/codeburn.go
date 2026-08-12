package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/prakashkurup/orchard/internal/codeburn"
)

const codeburnRefreshAge = time.Minute

var codeburnPeriods = []codeburn.Period{
	codeburn.PeriodToday,
	codeburn.PeriodWeek,
	codeburn.PeriodThirtyDay,
	codeburn.PeriodMonth,
	codeburn.PeriodSixMonths,
	codeburn.PeriodLifetime,
}

type codeburnMsg struct {
	period  codeburn.Period
	payload codeburn.Payload
	fetched time.Time
	err     error
}

func codeburnCmd(client *codeburn.Client, root string, period codeburn.Period) tea.Cmd {
	if demoMode() {
		return func() tea.Msg {
			return codeburnMsg{period: period, payload: demoCodeburn(period), fetched: time.Now()}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		payload, err := client.Query(ctx, period, root)
		return codeburnMsg{period: period, payload: payload, fetched: time.Now(), err: err}
	}
}

func (m model) applyCodeburnMsg(msg codeburnMsg) model {
	if msg.period != m.codeburnPeriod {
		return m // a slower previous-period request arrived after the user moved on
	}
	m.codeburnLoading = false
	m.codeburnFetched = msg.fetched
	if msg.err != nil {
		m.codeburnErr = firstLine(msg.err.Error())
		if m.mode == modeCodeburn {
			m.setCodeburnContent()
		}
		return m
	}
	m.codeburnErr = ""
	m.codeburnPayload = &msg.payload
	m.codeburnPayloadPeriod = msg.period
	if m.mode == modeCodeburn {
		m.setCodeburnContent()
	}
	m.resize() // the two-row cost strip may have appeared for the first time
	return m
}

func (m model) openCodeburn() (tea.Model, tea.Cmd) {
	m.returnMode = m.mode
	m.mode = modeCodeburn
	if m.codeburnPeriod == "" {
		m.codeburnPeriod = codeburn.PeriodToday
	}
	m.setCodeburnContent()
	m.detailVP.GotoTop()
	if m.codeburnPayload != nil && m.codeburnPayloadPeriod == m.codeburnPeriod && time.Since(m.codeburnFetched) < codeburnRefreshAge {
		return m, nil
	}
	m.codeburnLoading = true
	m.setCodeburnContent()
	return m, tea.Batch(codeburnCmd(m.codeburnClient, m.root, m.codeburnPeriod), m.spinner.Tick)
}

func (m model) handleCodeburnKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	reload := func(m model, period codeburn.Period) (tea.Model, tea.Cmd) {
		m.codeburnPeriod = period
		m.codeburnLoading = true
		m.codeburnErr = ""
		m.setCodeburnContent()
		m.detailVP.GotoTop()
		return m, tea.Batch(codeburnCmd(m.codeburnClient, m.root, period), m.spinner.Tick)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "U":
		m.mode = m.returnMode
		m.status = ""
		return m, nil
	case "r":
		return reload(m, m.codeburnPeriod)
	case "left", "h":
		return reload(m, adjacentCodeburnPeriod(m.codeburnPeriod, -1))
	case "right", "l":
		return reload(m, adjacentCodeburnPeriod(m.codeburnPeriod, 1))
	case "1", "2", "3", "4", "5", "6":
		idx := int(msg.Runes[0] - '1')
		return reload(m, codeburnPeriods[idx])
	}
	return m, nil
}

func adjacentCodeburnPeriod(current codeburn.Period, delta int) codeburn.Period {
	idx := 0
	for i, period := range codeburnPeriods {
		if period == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(codeburnPeriods)) % len(codeburnPeriods)
	return codeburnPeriods[idx]
}

func (m *model) setCodeburnContent() {
	m.detailVP.SetContent(m.codeburnBody(m.detailVP.Width))
}

func (m model) codeburnView(width int) string {
	title := segB(orange, " CodeBurn") + seg(muted, "  · "+displayRoot(m.root))
	tabs := codeburnTabs(m.codeburnPeriod)
	header := fillLine(fitStyled(title+seg(muted, "    ")+tabs, width), width, bg)
	hints := fillLine(strings.Join([]string{
		cmdHint("←→ / 1-6", "period"), cmdHint("r", "refresh"),
		cmdHint("↑↓", "scroll"), cmdHint("esc", "back"),
	}, ""), width, bg)
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		hrule(width),
		m.detailVP.View(),
		hrule(width),
		hints,
	)
}

func codeburnTabs(active codeburn.Period) string {
	var out []string
	for i, period := range codeburnPeriods {
		label := fmt.Sprintf("%d %s", i+1, period.Label())
		if period == active {
			out = append(out, segB(orange, "[ "+label+" ]"))
		} else {
			out = append(out, seg(muted, label))
		}
	}
	return strings.Join(out, seg(muted, "  "))
}

func (m model) codeburnBody(width int) string {
	line := func(s string) string { return fillLine(s, width, bg) }
	if m.codeburnLoading && (m.codeburnPayload == nil || m.codeburnPayloadPeriod != m.codeburnPeriod) {
		return strings.Join([]string{
			line(""),
			line(seg(orange, "  "+m.spinner.View()) + seg(muted, " loading CodeBurn "+m.codeburnPeriod.Label()+" usage…")),
		}, "\n")
	}
	if m.codeburnErr != "" && (m.codeburnPayload == nil || m.codeburnPayloadPeriod != m.codeburnPeriod) {
		rows := []string{
			line(""),
			line(segB(red, "  CodeBurn unavailable")),
			line(seg(muted, "  "+m.codeburnErr)),
		}
		rows = append(rows,
			line(""),
			line(seg(ice, "  Install: ")+seg(orange, "npm install -g codeburn")),
			line(seg(muted, "  Or set ORCHARD_CODEBURN=/path/to/codeburn (off disables integration).")),
		)
		return strings.Join(rows, "\n")
	}
	if m.codeburnPayload == nil {
		return strings.Join([]string{line(""), line(seg(muted, "  Waiting for CodeBurn usage…"))}, "\n")
	}

	payload := m.codeburnPayload
	rows := []string{line("")}
	rows = append(rows, burnOverview(payload, width)...)
	rows = append(rows, line(""))
	rows = append(rows, burnPanelGrid(payload, width)...)
	if len(payload.Current.UnpricedModels) > 0 {
		rows = append(rows, line(""), line(
			segB(yellow, fmt.Sprintf("  ⚠ %d unpriced model%s", len(payload.Current.UnpricedModels), pluralSuffix(len(payload.Current.UnpricedModels))))+
				seg(muted, " · their usage is excluded from cost; configure a CodeBurn model alias or price override"),
		))
	}
	if m.codeburnLoading {
		rows = append(rows, line(""), line(seg(orange, "  "+m.spinner.View())+seg(muted, " refreshing…")))
	}
	return strings.Join(rows, "\n")
}

func (m model) codeburnSummary(width int) string {
	if m.codeburnPayload == nil {
		return ""
	}
	p := m.codeburnPayload
	c := p.Current
	marker := segB(orange, "▌ ")
	content := marker + segB(orange, "CODEBURN") + seg(muted, "  "+c.Label+"   ") +
		segB(yellow, burnCost(p, c.Cost)) + seg(muted, " cost    ") +
		segB(ice, fmt.Sprintf("%d", c.Calls)) + seg(muted, " calls    ") +
		segB(ice, fmt.Sprintf("%d", c.Sessions)) + seg(muted, " sessions    ") +
		segB(green, fmt.Sprintf("%.1f%%", c.CacheHitPercent)) + seg(muted, " cache hit    ") +
		seg(blue, "U full usage")
	return lipgloss.JoinVertical(lipgloss.Left,
		hrule(width),
		fillLine(fitStyled(content, width), width, bg),
	)
}

func (m model) codeburnSummaryRows() int {
	if m.codeburnPayload == nil {
		return 0
	}
	return 2
}

func burnCost(payload *codeburn.Payload, usd float64) string {
	rate := payload.Currency.Rate
	if rate <= 0 {
		rate = 1
	}
	symbol := payload.Currency.Symbol
	if symbol == "" {
		symbol = "$"
	}
	return fmt.Sprintf("%s%.2f", symbol, usd*rate)
}

func burnOverview(payload *codeburn.Payload, width int) []string {
	c := payload.Current
	tokens := fmt.Sprintf("%s in   %s out   %s cached   %s written",
		humanTokens(c.InputTokens), humanTokens(c.OutputTokens),
		humanTokens(c.CacheReadTokens), humanTokens(c.CacheWriteTokens))
	coverage := ""
	if c.PricingCoverage != nil {
		coverage = fmt.Sprintf("   %.0f%% priced", *c.PricingCoverage*100)
	}
	body := []string{
		segB(yellow, burnCost(payload, c.Cost)) + seg(muted, " cost   ") +
			segB(ice, fmt.Sprintf("%d", c.Calls)) + seg(muted, " calls   ") +
			segB(ice, fmt.Sprintf("%d", c.Sessions)) + seg(muted, " sessions   ") +
			segB(green, fmt.Sprintf("%.1f%%", c.CacheHitPercent)) + seg(muted, " cache hit") + seg(muted, coverage),
		seg(muted, tokens),
	}
	return burnBox("CodeBurn  "+c.Label, orange, body, width, len(body))
}

type burnPanel struct {
	title string
	color string
	body  func(int) []string
}

type burnEntry struct {
	label  string
	metric string
	value  float64
	color  string
}

func burnPanelGrid(payload *codeburn.Payload, width int) []string {
	panels := burnPanels(payload)
	cols := 1
	if width >= 138 {
		cols = 3
	} else if width >= 88 {
		cols = 2
	}
	gap := 1
	panelWidth := (width - gap*(cols-1)) / cols
	var rows []string
	for start := 0; start < len(panels); start += cols {
		end := min(start+cols, len(panels))
		group := panels[start:end]
		bodies := make([][]string, len(group))
		maxBody := 1
		for i, panel := range group {
			bodies[i] = panel.body(max(1, panelWidth-4))
			maxBody = max(maxBody, len(bodies[i]))
		}
		boxes := make([][]string, len(group))
		for i, panel := range group {
			boxes[i] = burnBox(panel.title, panel.color, bodies[i], panelWidth, maxBody)
		}
		for lineIdx := range boxes[0] {
			var joined strings.Builder
			for i := range boxes {
				if i > 0 {
					joined.WriteString(strings.Repeat(" ", gap))
				}
				joined.WriteString(boxes[i][lineIdx])
			}
			rows = append(rows, fillLine(joined.String(), width, bg))
		}
		if end < len(panels) {
			rows = append(rows, fillLine("", width, bg))
		}
	}
	return rows
}

func burnPanels(payload *codeburn.Payload) []burnPanel {
	c := payload.Current
	return []burnPanel{
		{title: "Daily Activity", color: blue, body: func(width int) []string {
			var entries []burnEntry
			for i := len(payload.History.Daily) - 1; i >= 0 && len(entries) < 8; i-- {
				d := payload.History.Daily[i]
				if d.Calls == 0 && d.Cost == 0 {
					continue
				}
				entries = append(entries, burnEntry{label: d.Date, metric: fmt.Sprintf("%s %dc", burnCost(payload, d.Cost), d.Calls), value: d.Cost, color: blue})
			}
			return burnRanked(entries, width, "No daily activity")
		}},
		{title: "By Project", color: green, body: func(width int) []string {
			entries := make([]burnEntry, 0, len(c.TopProjects))
			for _, p := range c.TopProjects {
				entries = append(entries, burnEntry{label: p.Name, metric: fmt.Sprintf("%s %ds", burnCost(payload, p.Cost), p.Sessions), value: p.Cost, color: green})
			}
			return burnRanked(entries, width, "No project usage")
		}},
		{title: "By Activity", color: yellow, body: func(width int) []string {
			entries := make([]burnEntry, 0, len(c.TopActivities))
			for _, a := range c.TopActivities {
				entries = append(entries, burnEntry{label: a.Name, metric: fmt.Sprintf("%s %dt", burnCost(payload, a.Cost), a.Turns), value: a.Cost, color: yellow})
			}
			return burnRanked(limitBurnEntries(entries, 8), width, "No classified activity")
		}},
		{title: "By Model", color: accent, body: func(width int) []string {
			entries := make([]burnEntry, 0, len(c.TopModels))
			for _, model := range c.TopModels {
				metric := fmt.Sprintf("%s %dc", burnCost(payload, model.Cost), model.Calls)
				if model.EstimatedCostUSD > 0 {
					metric += "~"
				}
				entries = append(entries, burnEntry{label: model.Name, metric: metric, value: model.Cost, color: accent})
			}
			return burnRanked(limitBurnEntries(entries, 8), width, "No model usage")
		}},
		{title: "Providers", color: orange, body: func(width int) []string {
			providers := append([]codeburn.Provider(nil), c.ProviderDetails...)
			if len(providers) == 0 {
				for name, cost := range c.Providers {
					providers = append(providers, codeburn.Provider{Label: name, Cost: cost})
				}
				sort.Slice(providers, func(i, j int) bool { return providers[i].Cost > providers[j].Cost })
			}
			var entries []burnEntry
			for _, p := range providers {
				if p.Cost == 0 {
					continue
				}
				entries = append(entries, burnEntry{label: p.Label, metric: burnCost(payload, p.Cost), value: p.Cost, color: orange})
			}
			return burnRanked(entries, width, "No provider usage")
		}},
		{title: "Core Tools", color: teal, body: func(width int) []string {
			entries := make([]burnEntry, 0, len(c.Tools))
			for _, tool := range c.Tools {
				entries = append(entries, burnEntry{label: tool.Name, metric: fmt.Sprintf("%d", tool.Calls), value: float64(tool.Calls), color: teal})
			}
			return burnRanked(limitBurnEntries(entries, 8), width, "No tool usage")
		}},
		{title: "MCP Servers", color: cyan, body: func(width int) []string {
			entries := make([]burnEntry, 0, len(c.MCPServers))
			for _, server := range c.MCPServers {
				entries = append(entries, burnEntry{label: server.Name, metric: fmt.Sprintf("%d", server.Calls), value: float64(server.Calls), color: cyan})
			}
			return burnRanked(limitBurnEntries(entries, 8), width, "No MCP usage")
		}},
		{title: "Skills & Agents", color: accent, body: func(width int) []string {
			var entries []burnEntry
			for _, skill := range c.Skills {
				entries = append(entries, burnEntry{label: "/" + skill.Name, metric: fmt.Sprintf("%dt", skill.Turns), value: float64(skill.Turns), color: accent})
			}
			for _, agent := range c.Subagents {
				entries = append(entries, burnEntry{label: agent.Name, metric: fmt.Sprintf("%dc", agent.Calls), value: float64(agent.Calls), color: codexC})
			}
			return burnRanked(limitBurnEntries(entries, 8), width, "No skill or agent usage")
		}},
		{title: "Workflow", color: blue, body: func(width int) []string {
			workflow := c.Workflow
			correctionRate := "-"
			if workflow.CorrectionRate != nil {
				correctionRate = fmt.Sprintf("%.1f%%", *workflow.CorrectionRate*100)
			}
			firstEdit := "-"
			if workflow.MedianTimeToFirstEditMS != nil {
				firstEdit = burnDuration(*workflow.MedianTimeToFirstEditMS)
			}
			coverage := "-"
			if c.PricingCoverage != nil {
				coverage = fmt.Sprintf("%.1f%%", *c.PricingCoverage*100)
			}
			return []string{
				burnKV("Corrections", fmt.Sprintf("%d", workflow.Corrections), width),
				burnKV("Correction rate", correctionRate, width),
				burnKV("First edit", firstEdit, width),
				burnKV("Pricing coverage", coverage, width),
				burnKV("Unpriced models", fmt.Sprintf("%d", len(c.UnpricedModels)), width),
			}
		}},
	}
}

func limitBurnEntries(entries []burnEntry, n int) []burnEntry {
	if len(entries) > n {
		return entries[:n]
	}
	return entries
}

func burnRanked(entries []burnEntry, width int, empty string) []string {
	if len(entries) == 0 {
		return []string{seg(muted, fit(empty, width))}
	}
	maxValue := 0.0
	metricWidth := 1
	for _, entry := range entries {
		maxValue = max(maxValue, entry.value)
		metricWidth = max(metricWidth, len(entry.metric))
	}
	barWidth := 7
	if width < 28 {
		barWidth = 4
	}
	labelWidth := width - barWidth - metricWidth - 2
	if labelWidth < 5 {
		barWidth = 0
		labelWidth = max(1, width-metricWidth-1)
	}
	var rows []string
	for _, entry := range entries {
		bar := ""
		if barWidth > 0 {
			filled := 0
			if maxValue > 0 {
				filled = int(entry.value / maxValue * float64(barWidth))
			}
			if entry.value > 0 && filled == 0 {
				filled = 1
			}
			filled = clamp(filled, 0, barWidth)
			bar = seg(entry.color, strings.Repeat("█", filled)) + seg(muted, strings.Repeat("░", barWidth-filled)) + " "
		}
		rows = append(rows, bar+seg(ice, padRight(entry.label, labelWidth))+" "+seg(yellow, padLeft(entry.metric, metricWidth)))
	}
	return rows
}

func burnKV(label, value string, width int) string {
	valueWidth := min(14, max(1, len(value)))
	return seg(muted, padRight(label, max(1, width-valueWidth-1))) + " " + seg(ice, padLeft(value, valueWidth))
}

func burnDuration(ms float64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func burnBox(title, color string, body []string, width, bodyHeight int) []string {
	width = max(width, 12)
	inner := width - 4
	title = fit(title, width-5)
	topFill := max(0, width-5-len(title))
	top := seg(color, "╭─ "+title+" "+strings.Repeat("─", topFill)+"╮")
	bottom := seg(color, "╰"+strings.Repeat("─", width-2)+"╯")
	rows := []string{top}
	for i := 0; i < bodyHeight; i++ {
		content := ""
		if i < len(body) {
			content = fitStyled(body[i], inner)
		}
		content += strings.Repeat(" ", max(0, inner-ansi.StringWidth(content)))
		rows = append(rows, seg(color, "│")+" "+content+" "+seg(color, "│"))
	}
	return append(rows, bottom)
}

func demoCodeburn(period codeburn.Period) codeburn.Payload {
	oneShot := 0.91
	coverage := 0.98
	correctionRate := 0.08
	firstEdit := float64((9 * time.Minute).Milliseconds())
	return codeburn.Payload{
		Current: codeburn.Current{
			Label: period.Label(), Cost: 42.73, Calls: 318, Sessions: 9,
			InputTokens: 2_300_000, OutputTokens: 184_000, CacheReadTokens: 31_400_000, CacheWriteTokens: 420_000,
			CacheHitPercent: 91.5, OneShotRate: &oneShot, PricingCoverage: &coverage,
			TopProjects:     []codeburn.Project{{Name: "payments-api", Cost: 18.40, Sessions: 3}, {Name: "data-pipeline", Cost: 14.20, Sessions: 4}, {Name: "web-checkout", Cost: 10.13, Sessions: 2}},
			TopActivities:   []codeburn.Activity{{Name: "Coding", Cost: 17.5, Turns: 12}, {Name: "Debugging", Cost: 12.1, Turns: 8}, {Name: "Exploration", Cost: 8.3, Turns: 21}},
			TopModels:       []codeburn.Model{{Name: "GPT-5.6 Sol", Cost: 31.2, Calls: 240}, {Name: "Claude Sonnet 4.6", Cost: 11.53, Calls: 78}},
			ProviderDetails: []codeburn.Provider{{ID: "codex", Label: "Codex", Cost: 31.2}, {ID: "claude", Label: "Claude", Cost: 11.53}},
			Tools:           []codeburn.CountRow{{Name: "Bash", Calls: 224}, {Name: "Read", Calls: 81}, {Name: "Edit", Calls: 37}},
			MCPServers:      []codeburn.CountRow{{Name: "github", Calls: 32}, {Name: "atlassian", Calls: 11}},
			Skills:          []codeburn.CostRow{{Name: "debugging", Turns: 5}}, Subagents: []codeburn.CostRow{{Name: "review", Calls: 3}},
			Workflow: codeburn.Workflow{Corrections: 2, CorrectionRate: &correctionRate, MedianTimeToFirstEditMS: &firstEdit},
		},
		History: codeburn.History{Daily: []codeburn.Daily{
			{Date: "2026-08-10", Cost: 18.40, Calls: 112}, {Date: "2026-08-11", Cost: 11.21, Calls: 94}, {Date: "2026-08-12", Cost: 13.12, Calls: 112},
		}},
		Currency: codeburn.Currency{Code: "USD", Symbol: "$", Rate: 1},
	}
}
