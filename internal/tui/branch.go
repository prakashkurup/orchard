package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

func (m model) openBranchSwitcher(r repo.Repo) (tea.Model, tea.Cmd) {
	if r.Path == "" {
		return m, nil
	}
	m.branchRepo = r.Path
	m.branchAll = nil
	m.branchCursor = 0
	m.branchLoading = true
	m.branchBusy = false
	m.branchErr = ""
	m.branchTarget = ""
	m.branchInput.SetValue("")
	m.returnMode = m.mode
	m.mode = modeBranch
	return m, tea.Batch(m.branchInput.Focus(), branchesCmd(r))
}

func branchesCmd(r repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return branchesMsg{path: r.Path, branches: demoBranches()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		bs, err := orchardgit.Branches(ctx, r)
		return branchesMsg{path: r.Path, branches: bs, err: err}
	}
}

func checkoutCmd(r repo.Repo, branch string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := orchardgit.Checkout(ctx, r.Path, branch); err != nil {
			return checkoutMsg{repo: r, branch: branch, err: err}
		}
		updated, _ := orchardgit.Status(ctx, r) // refresh branch/state; preserves CC fields
		return checkoutMsg{repo: updated, branch: branch}
	}
}

// stashCheckoutCmd stashes the working tree, then switches branch (used after a
// checkout is blocked by uncommitted changes).
func stashCheckoutCmd(r repo.Repo, branch string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := orchardgit.Stash(ctx, r.Path); err != nil {
			return checkoutMsg{repo: r, branch: branch, err: err}
		}
		if err := orchardgit.Checkout(ctx, r.Path, branch); err != nil {
			return checkoutMsg{repo: r, branch: branch, err: err, stashed: true}
		}
		updated, _ := orchardgit.Status(ctx, r)
		return checkoutMsg{repo: updated, branch: branch, stashed: true}
	}
}

func (m model) filteredBranches() []orchardgit.Branch {
	q := strings.ToLower(strings.TrimSpace(m.branchInput.Value()))
	if q == "" {
		return m.branchAll
	}
	var out []orchardgit.Branch
	for _, b := range m.branchAll {
		if strings.Contains(strings.ToLower(b.Name), q) {
			out = append(out, b)
		}
	}
	return out
}

func (m model) handleBranchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.branchBusy { // a switch is in flight; ignore input until it resolves
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.mode = m.returnMode
		m.branchInput.Blur()
		m.branchErr = ""
		m.branchTarget = ""
		return m, nil
	case "s":
		// when a switch is blocked by uncommitted changes, 's' stashes and retries
		if m.branchErr != "" && m.branchTarget != "" {
			m.branchBusy = true
			m.branchErr = ""
			return m, stashCheckoutCmd(m.repoByPath(m.branchRepo), m.branchTarget)
		}
	case "up", "ctrl+p":
		m.branchCursor = clamp(m.branchCursor-1, 0, max(0, len(m.filteredBranches())-1))
		return m, nil
	case "down", "ctrl+n":
		m.branchCursor = clamp(m.branchCursor+1, 0, max(0, len(m.filteredBranches())-1))
		return m, nil
	case "enter":
		fb := m.filteredBranches()
		if m.branchCursor < 0 || m.branchCursor >= len(fb) {
			return m, nil
		}
		b := fb[m.branchCursor]
		r := m.repoByPath(m.branchRepo)
		if b.Current {
			m.mode = m.returnMode
			m.branchInput.Blur()
			m.status = "already on " + b.Name
			return m, nil
		}
		// keep the modal open; the result (success or error) is handled in Update
		m.branchBusy = true
		m.branchTarget = b.Name
		m.branchErr = ""
		m.branchInput.Blur()
		return m, checkoutCmd(r, b.Name)
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	m.branchCursor = clamp(m.branchCursor, 0, max(0, len(m.filteredBranches())-1))
	return m, cmd
}

func (m model) branchView(width int) string {
	// every fragment carries the panel background so there is no internal banding
	fg := panelFG
	inner := clamp(width-16, 40, 68)
	repoName := ""
	if r := m.repoByPath(m.branchRepo); r.Path != "" {
		repoName = r.Name
	}

	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render("⎇ Switch branch") + fg(muted).Render("  · "+repoName))
		add("")
		add(fg(accent).Bold(true).Render(" / ") + lipgloss.NewStyle().Background(lipgloss.Color(panel)).Render(m.branchInput.View()))
		add("")

		switch {
		case m.branchLoading:
			add(fg(muted).Render("  loading branches…"))
		case len(m.filteredBranches()) == 0:
			add(fg(muted).Render("  no matching branches"))
		default:
			fb := m.filteredBranches()
			const maxRows = 12
			start := 0
			if m.branchCursor >= maxRows {
				start = m.branchCursor - maxRows + 1
			}
			end := start + maxRows
			if end > len(fb) {
				end = len(fb)
			}
			// fixed columns so name / type / time line up across every row
			const relW, typeW = 13, 6
			nameW := max(10, inner-4-1-typeW-3-relW)
			for i := start; i < end; i++ {
				br := fb[i]
				cursor := fg(panel).Render("  ")
				nameC := blue // local branch
				switch {
				case br.Current:
					nameC = green
				case br.Remote:
					nameC = ice
				}
				nameStyle := fg(nameC)
				if i == m.branchCursor {
					cursor = fg(accent).Bold(true).Render("▌ ")
					nameStyle = nameStyle.Bold(true)
				}
				mark := fg(panel).Render("  ")
				if br.Current {
					mark = fg(green).Render("● ")
				}
				tag := ""
				if br.Remote {
					tag = "remote"
				}
				add(cursor + mark +
					nameStyle.Render(padRight(fit(br.Name, nameW), nameW)) +
					fg(muted).Render(" "+padRight(tag, typeW)+" · "+fit(br.Rel, relW)))
			}
			if len(fb) > maxRows {
				add(fg(muted).Render(fmt.Sprintf("  … %d more (type to filter)", len(fb)-maxRows)))
			}
		}
		add("")
		switch {
		case m.branchBusy:
			add(fg(muted).Render("  switching to " + m.branchTarget + "…"))
		case m.branchErr != "":
			add(fg(red).Render("  " + m.branchErr))
			add(fg(muted).Render("  [s] stash & switch   ↑↓ pick another   esc cancel"))
		default:
			add(fg(muted).Render("↑↓ move · ⏎ checkout · esc cancel"))
		}
	})
}
