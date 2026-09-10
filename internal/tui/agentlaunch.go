package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/termlaunch"
)

type agentPlacement int

const (
	agentNewTab agentPlacement = iota
	agentThisTab
)

type agentAccess int

const (
	agentAccessDefault agentAccess = iota
	agentAccessPlan
	agentAccessWorkspace
	agentAccessCount
)

const (
	agentRowProvider = iota
	agentRowPlacement
	agentRowAccess
	agentRowModel
	agentRowPrompt
	agentRowCount
)

func (m model) openAgentLauncher(targets []repo.Repo) (tea.Model, tea.Cmd) {
	choices := availableAssistants()
	if len(choices) == 0 {
		m.status = "no AI assistant found (install claude or codex, or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if len(targets) == 0 {
		m.status = "nothing to open"
		return m, nil
	}
	m.agentChoices = choices
	m.agentChoice = 0
	for i, choice := range choices {
		if choice.cmd == m.assistantCmd {
			m.agentChoice = i
			break
		}
	}
	m.agentTargets = append([]repo.Repo(nil), targets...)
	m.agentPlacement = agentNewTab
	m.agentAccess = agentAccessDefault
	m.agentLaunchRow = 0
	m.agentLaunchEditing = false
	m.agentModelInput.SetValue("")
	m.agentPromptInput.SetValue("")
	m.returnMode = m.mode
	m.mode = modeAgentLaunch
	return m, nil
}

func (m model) handleAgentLaunchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.agentLaunchEditing {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "enter":
			m.agentLaunchEditing = false
			m.agentModelInput.Blur()
			m.agentPromptInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		if m.agentLaunchRow == agentRowModel {
			m.agentModelInput, cmd = m.agentModelInput.Update(msg)
		} else {
			m.agentPromptInput, cmd = m.agentPromptInput.Update(msg)
		}
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = m.returnMode
		m.agentTargets = nil
		return m, nil
	case "up", "k", "shift+tab":
		m.agentLaunchRow = (m.agentLaunchRow - 1 + agentRowCount) % agentRowCount
	case "down", "j", "tab":
		m.agentLaunchRow = (m.agentLaunchRow + 1) % agentRowCount
	case "left", "h":
		m.cycleAgentOption(-1)
	case "right", "l":
		m.cycleAgentOption(1)
	case "e", "i":
		if m.agentLaunchRow == agentRowModel || m.agentLaunchRow == agentRowPrompt {
			return m.beginAgentLaunchEdit()
		}
	case "x", "backspace", "delete":
		if m.agentLaunchRow == agentRowModel {
			m.agentModelInput.SetValue("")
		} else if m.agentLaunchRow == agentRowPrompt {
			m.agentPromptInput.SetValue("")
		}
	case "enter":
		return m.launchAgentSelection()
	default:
		if msg.Type == tea.KeyRunes && (m.agentLaunchRow == agentRowModel || m.agentLaunchRow == agentRowPrompt) {
			next, cmd := m.beginAgentLaunchEdit()
			m = next.(model)
			if m.agentLaunchRow == agentRowModel {
				m.agentModelInput, cmd = m.agentModelInput.Update(msg)
			} else {
				m.agentPromptInput, cmd = m.agentPromptInput.Update(msg)
			}
			return m, cmd
		}
	}
	return m, nil
}

func (m model) beginAgentLaunchEdit() (tea.Model, tea.Cmd) {
	m.agentLaunchEditing = true
	if m.agentLaunchRow == agentRowModel {
		m.agentPromptInput.Blur()
		return m, m.agentModelInput.Focus()
	}
	m.agentModelInput.Blur()
	return m, m.agentPromptInput.Focus()
}

func (m *model) cycleAgentOption(delta int) {
	switch m.agentLaunchRow {
	case agentRowProvider:
		if len(m.agentChoices) > 1 {
			m.agentChoice = (m.agentChoice + delta + len(m.agentChoices)) % len(m.agentChoices)
		}
	case agentRowPlacement:
		if len(m.agentTargets) == 1 {
			m.agentPlacement = agentPlacement((int(m.agentPlacement) + delta + 2) % 2)
		}
	case agentRowAccess:
		m.agentAccess = agentAccess((int(m.agentAccess) + delta + int(agentAccessCount)) % int(agentAccessCount))
	}
}

func (m model) selectedAgentChoice() assistantChoice {
	if m.agentChoice >= 0 && m.agentChoice < len(m.agentChoices) {
		return m.agentChoices[m.agentChoice]
	}
	return assistantChoice{cmd: m.assistantCmd, label: m.assistantLabel}
}

func agentAccessLabel(access agentAccess) string {
	switch access {
	case agentAccessPlan:
		return "plan / read-only"
	case agentAccessWorkspace:
		return "workspace edits"
	default:
		return "use agent default"
	}
}

func agentOptionArgs(choice assistantChoice, access agentAccess, modelName, prompt string) []string {
	var args []string
	isCodex := strings.Contains(strings.ToLower(choice.cmd), "codex")
	switch access {
	case agentAccessPlan:
		if isCodex {
			args = append(args, "--sandbox", "read-only")
		} else {
			args = append(args, "--permission-mode", "plan")
		}
	case agentAccessWorkspace:
		if isCodex {
			args = append(args, "--sandbox", "workspace-write")
		} else {
			args = append(args, "--permission-mode", "acceptEdits")
		}
	}
	if modelName = strings.TrimSpace(modelName); modelName != "" {
		args = append(args, "--model", modelName)
	}
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		args = append(args, prompt)
	}
	return args
}

func (m model) launchAgentSelection() (tea.Model, tea.Cmd) {
	if len(m.agentTargets) == 0 {
		m.mode = m.returnMode
		return m, nil
	}
	choice := m.selectedAgentChoice()
	m.assistantCmd, m.assistantLabel = choice.cmd, choice.label
	args := agentOptionArgs(choice, m.agentAccess, m.agentModelInput.Value(), m.agentPromptInput.Value())
	targets := append([]repo.Repo(nil), m.agentTargets...)
	placement := m.agentPlacement
	if len(targets) > 1 {
		placement = agentNewTab
	}
	m.mode = m.returnMode
	m.agentTargets = nil
	if len(targets) == 1 {
		status := "opening " + choice.label + " · " + targets[0].Name
		if placement == agentThisTab {
			status = "running " + choice.label + " here · " + targets[0].Name
		}
		return m.runAssistantPlaced(targets[0].Path, args, nil, status, []string{targets[0].Path}, placement)
	}
	return m.launchAgentTabs(targets, args)
}

func (m model) launchAgentTabs(targets []repo.Repo, args []string) (tea.Model, tea.Cmd) {
	progs := make([]string, len(targets))
	var wireErr error
	for i, r := range targets {
		if err := m.wireGraphMCP(r.Path, []string{r.Path}); err != nil {
			wireErr = err
		}
		prog := m.assistantCmd
		for _, arg := range args {
			prog += " " + shQuote(arg)
		}
		progs[i] = prog
	}
	if _, ok := termlaunch.NewTab(targets[0].Path, progs[0]); !ok {
		m.status = "this terminal cannot open multiple agent tabs"
		return m, nil
	}
	suffix := m.graphSuffix(wireErr)
	m.status = fmt.Sprintf("opening %s in %d tabs%s", m.assistantLabel, len(targets), suffix)
	return m, func() tea.Msg {
		opened := 0
		for i, target := range targets {
			if cmd, ok := termlaunch.NewTab(target.Path, progs[i]); ok && cmd != nil && cmd.Run() == nil {
				opened++
			}
		}
		return statusMsg{text: fmt.Sprintf("opened %s in %d tabs%s", m.assistantLabel, opened, suffix)}
	}
}

func (m model) runAssistantPlaced(cwd string, args, env []string, status string, graphRepos []string, placement agentPlacement) (tea.Model, tea.Cmd) {
	if placement == agentNewTab {
		return m.runAssistant(cwd, args, env, status, graphRepos)
	}
	wireErr := m.wireGraphMCP(cwd, graphRepos)
	status += m.graphSuffix(wireErr)
	fields := append(strings.Fields(m.assistantCmd), args...)
	if len(fields) == 0 {
		m.status = "cannot launch agent"
		return m, nil
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	m.status = status
	label := m.assistantLabel
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return statusMsg{text: label + " exited: " + err.Error()}
		}
		return statusMsg{text: "returned from " + label}
	})
}

func (m model) agentLaunchView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 52, 82)
	choice := m.selectedAgentChoice()
	placement := "new terminal tab"
	placementNote := "same window when the terminal supports it"
	if m.agentPlacement == agentThisTab && len(m.agentTargets) == 1 {
		placement = "this terminal tab"
		placementNote = "Orchard returns when the agent exits"
	}
	if len(m.agentTargets) > 1 {
		placement = fmt.Sprintf("%d new terminal tabs", len(m.agentTargets))
		placementNote = "one tab per selected repo"
	}

	row := func(index int, label, value, note string) string {
		marker, labelColor, valueColor := "  ", muted, ice
		if m.agentLaunchRow == index {
			marker, labelColor, valueColor = "▌ ", accent, accent
		}
		line := fg(labelColor).Bold(index == m.agentLaunchRow).Render(marker + padRight(label, 12))
		line += fg(valueColor).Bold(index == m.agentLaunchRow).Render(fit(value, 28))
		if note != "" {
			line += fg(muted).Render("  " + fit(note, max(8, inner-46)))
		}
		return line
	}

	modelValue := strings.TrimSpace(m.agentModelInput.Value())
	if modelValue == "" {
		modelValue = "agent default"
	}
	promptValue := strings.TrimSpace(m.agentPromptInput.Value())
	if promptValue == "" {
		promptValue = "none"
	}
	if m.agentLaunchEditing && m.agentLaunchRow == agentRowModel {
		modelValue = m.agentModelInput.Value() + "▏"
	}
	if m.agentLaunchEditing && m.agentLaunchRow == agentRowPrompt {
		promptValue = m.agentPromptInput.Value() + "▏"
	}

	return modalBox(inner, func(add func(string)) {
		add(fg(accent).Bold(true).Render("✦ Launch agent") + fg(muted).Render(fmt.Sprintf("  · %d repo(s)", len(m.agentTargets))))
		add("")
		add(row(agentRowProvider, "Agent", choice.label, "← → select"))
		add(row(agentRowPlacement, "Open in", placement, placementNote))
		add(row(agentRowAccess, "Access", agentAccessLabel(m.agentAccess), "← → select"))
		add(row(agentRowModel, "Model", modelValue, "type or e to edit"))
		add(row(agentRowPrompt, "Prompt", promptValue, "type or e to edit"))
		add("")
		if m.agentLaunchEditing {
			add(fg(muted).Render("type value · enter done · esc done"))
		} else {
			add(fg(muted).Render("↑↓ option · ←→ change · enter launch · esc cancel"))
		}
	})
}
