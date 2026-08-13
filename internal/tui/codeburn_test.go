package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/codeburn"
)

func TestCodeburnDashboardFitsResponsiveWidths(t *testing.T) {
	for _, width := range []int{56, 96, 140, 180} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(strings.Repeat("w", width/10)), " ", "-"), func(t *testing.T) {
			m := newModel("/repo", 4)
			payload := demoCodeburn(codeburn.PeriodToday)
			m.codeburnPayload = &payload
			m.codeburnPayloadPeriod = codeburn.PeriodToday
			m.codeburnPeriod = codeburn.PeriodToday
			out := m.codeburnBody(width)
			for _, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("line width = %d, want <= %d\n%s", got, width, line)
				}
			}
		})
	}
}

func TestCodeburnSummaryIsTwoRowsAndShowsCost(t *testing.T) {
	m := newModel("/repo", 4)
	payload := demoCodeburn(codeburn.PeriodWeek)
	m.codeburnPayload = &payload
	out := ansiPattern.ReplaceAllString(m.codeburnSummary(120), "")
	if m.codeburnSummaryRows() != 2 {
		t.Fatalf("summary rows = %d, want 2", m.codeburnSummaryRows())
	}
	for _, want := range []string{"AGENT USAGE", "via CodeBurn", "$42.73", "cache hit", "U full usage"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q\n%s", want, out)
		}
	}
}

func TestCodeburnReplacesLegacyUsageSummary(t *testing.T) {
	m := newModel("/repo", 4)
	claudeUsage := demoClaude()
	m.claudeUsage = &claudeUsage
	if !m.showLegacyUsageSummary() || m.legacyUsageSummaryRows() == 0 {
		t.Fatal("legacy usage should show while CodeBurn has no data")
	}
	payload := demoCodeburn(codeburn.PeriodToday)
	m.codeburnPayload = &payload
	if m.showLegacyUsageSummary() || m.legacyUsageSummaryRows() != 0 {
		t.Fatal("CodeBurn data should replace the differently scoped legacy summary")
	}
}

func TestCodeburnShowsUnpricedModelOnceInsideModelPanel(t *testing.T) {
	m := newModel("/repo", 4)
	payload := demoCodeburn(codeburn.PeriodToday)
	payload.Current.UnpricedModels = []codeburn.UnpricedModel{{Model: "Codex Auto Review", Calls: 28, Tokens: 5_910_602}}
	m.codeburnPayload = &payload
	m.codeburnPayloadPeriod = codeburn.PeriodToday
	m.codeburnPeriod = codeburn.PeriodToday
	out := strings.ToLower(ansiPattern.ReplaceAllString(m.codeburnBody(140), ""))
	if got := strings.Count(out, "unpriced"); got != 1 {
		t.Fatalf("unpriced note count = %d, want 1\n%s", got, out)
	}
	for _, want := range []string{"codex auto review", "28 calls"} {
		if !strings.Contains(out, want) {
			t.Fatalf("unpriced note missing %q\n%s", want, out)
		}
	}
}

func TestApplyCodeburnMsgIgnoresStalePeriod(t *testing.T) {
	m := newModel("/repo", 4)
	m.codeburnPeriod = codeburn.PeriodMonth
	week := demoCodeburn(codeburn.PeriodWeek)
	m = m.applyCodeburnMsg(codeburnMsg{period: codeburn.PeriodWeek, payload: week, fetched: time.Now()})
	if m.codeburnPayload != nil {
		t.Fatal("stale period response should not replace current payload")
	}
}

func TestAdjacentCodeburnPeriodWraps(t *testing.T) {
	if got := adjacentCodeburnPeriod(codeburn.PeriodToday, -1); got != codeburn.PeriodLifetime {
		t.Fatalf("previous from today = %q", got)
	}
	if got := adjacentCodeburnPeriod(codeburn.PeriodLifetime, 1); got != codeburn.PeriodToday {
		t.Fatalf("next from lifetime = %q", got)
	}
}
