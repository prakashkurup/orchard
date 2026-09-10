package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/codex"
	"github.com/prakashkurup/orchard/internal/repo"
)

const agentMonitorInterval = 5 * time.Second

type agentMonitorTickMsg time.Time

type agentFootprint struct {
	claudeSessions int
	claudeLast     time.Time
	codexSessions  int
	codexLast      time.Time
}

type agentMonitorMsg map[string]agentFootprint

func agentMonitorTickCmd() tea.Cmd {
	return tea.Tick(agentMonitorInterval, func(t time.Time) tea.Msg { return agentMonitorTickMsg(t) })
}

func agentMonitorCmd(repos []repo.Repo) tea.Cmd {
	snapshot := append([]repo.Repo(nil), repos...)
	if demoMode() {
		return func() tea.Msg {
			out := make(agentMonitorMsg, len(snapshot))
			for _, r := range snapshot {
				out[r.Path] = agentFootprint{claudeSessions: r.CCSessions, claudeLast: r.CCLast, codexSessions: r.CodexSessions, codexLast: r.CodexLast}
			}
			return out
		}
	}
	return func() tea.Msg {
		out := make(agentMonitorMsg, len(snapshot))
		for _, r := range snapshot {
			cn, cl := claude.Summary(r.Path)
			xn, xl := codex.Summary(r.Path)
			out[r.Path] = agentFootprint{claudeSessions: cn, claudeLast: cl, codexSessions: xn, codexLast: xl}
		}
		return out
	}
}

func (m *model) applyAgentMonitor(msg agentMonitorMsg) {
	for i := range m.repos {
		if state, ok := msg[m.repos[i].Path]; ok {
			m.repos[i].CCSessions = state.claudeSessions
			m.repos[i].CCLast = state.claudeLast
			m.repos[i].CodexSessions = state.codexSessions
			m.repos[i].CodexLast = state.codexLast
		}
	}
	m.syncRows()
}
