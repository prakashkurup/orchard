package tui

import (
	"context"
	"fmt"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
	"strings"
	"sync"
	"time"
)

func worklogCmd(repos []repo.Repo, window string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		groups := make([]worklogGroup, len(repos))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, r := range repos {
			wg.Add(1)
			go func(i int, r repo.Repo) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if cs := orchardgit.Worklog(ctx, r.Path, window); len(cs) > 0 {
					groups[i] = worklogGroup{repo: r.Name, commits: cs}
				}
			}(i, r)
		}
		wg.Wait()

		var out []worklogGroup
		total := 0
		var sb strings.Builder
		sb.WriteString("Worklog · since " + windowLabel(window) + "\n\n")
		for _, g := range groups {
			if len(g.commits) == 0 {
				continue
			}
			out = append(out, g)
			total += len(g.commits)
			sb.WriteString(g.repo + "\n")
			for _, c := range g.commits {
				sb.WriteString("  • " + c.Subject + "\n")
			}
			sb.WriteString("\n")
		}
		if total == 0 {
			sb.WriteString("No commits in this window.\n")
		}
		return worklogMsg{window: window, groups: out, total: total, text: sb.String()}
	}
}

func (m model) handleWorklogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	reload := func(m model, w string) (tea.Model, tea.Cmd) {
		m.worklogWindow = w
		m.status = ""
		m.detailVP.SetContent(fillLine(subtleStyle.Render("  building worklog…"), m.detailVP.Width, bg))
		return m, worklogCmd(m.repos, w)
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeList
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
	case "1":
		return reload(m, "1 day ago")
	case "2":
		return reload(m, "7 days ago")
	case "3":
		return reload(m, "30 days ago")
	case "y", "c":
		if m.worklogText != "" {
			_ = clipboard.WriteAll(m.worklogText)
			m.status = "worklog copied"
		}
	}
	return m, nil
}

func windowLabel(w string) string {
	switch w {
	case "1 day ago":
		return "last 24h"
	case "7 days ago":
		return "last 7 days"
	case "30 days ago":
		return "last 30 days"
	default:
		return w
	}
}

func (m model) worklogBody(width int, msg worklogMsg) string {
	fg := bgFG
	line := func(s string) string { return fillLine(s, width, bg) }
	var rows []string
	rows = append(rows, line(""))
	rows = append(rows, line("  "+fg(muted).Render(fmt.Sprintf("%d commits across %d repos · %s", msg.total, len(msg.groups), windowLabel(msg.window)))))
	rows = append(rows, line(""))
	if msg.total == 0 {
		rows = append(rows, line("  "+fg(muted).Render("No commits in this window - try a wider one (1 = 24h, 2 = 7d, 3 = 30d).")))
		return strings.Join(rows, "\n")
	}
	for _, g := range msg.groups {
		rows = append(rows, line("  "+fg(blue).Bold(true).Render(g.repo)+fg(muted).Render(fmt.Sprintf("  (%d)", len(g.commits)))))
		for _, c := range g.commits {
			rows = append(rows, line("    "+fg(accent).Render("• ")+fg(muted).Render(fit(c.Rel, 12)+"  ")+fg(ice).Render(fit(c.Subject, max(10, width-22)))))
		}
		rows = append(rows, line(""))
	}
	return strings.Join(rows, "\n")
}

func (m model) worklogView(width int) string {
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(bg)).Bold(true).Render("⊞ Worklog") +
		subtleStyle.Render("  · your commits · "+windowLabel(m.worklogWindow))
	rule := hrule(width)
	hint := strings.Join([]string{cmdHint("1/2/3", "24h/7d/30d"), cmdHint("y", "copy"), cmdHint("esc", "close")}, "")
	if m.status == "worklog copied" {
		hint = lipgloss.NewStyle().Foreground(lipgloss.Color(green)).Background(lipgloss.Color(bg)).Bold(true).Render("  ✓ copied to clipboard")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		fillLine(title, width, bg),
		rule,
		m.detailVP.View(),
		rule,
		fillLine(hint, width, bg),
	)
}
