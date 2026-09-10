package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestAvailableAssistantsOffersClaudeAndCodex(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ORCHARD_AI_CMD", "")
	t.Setenv("PATH", dir)
	got := availableAssistants()
	if len(got) != 2 || got[0].label != "Claude Code" || got[1].label != "Codex" {
		t.Fatalf("choices = %#v", got)
	}
}

func TestExplicitAssistantCommandUsesExecutableLabel(t *testing.T) {
	t.Setenv("ORCHARD_AI_CMD", "/opt/tools/codex --profile orchard")
	got := availableAssistants()
	if len(got) != 1 || got[0].label != "Codex" {
		t.Fatalf("explicit choice = %#v", got)
	}
}

func TestAgentOptionArgsAreProviderSpecific(t *testing.T) {
	tests := []struct {
		name   string
		choice assistantChoice
		access agentAccess
		want   []string
	}{
		{"claude plan", assistantChoice{cmd: "claude"}, agentAccessPlan, []string{"--permission-mode", "plan", "--model", "sonnet", "start here"}},
		{"claude edits", assistantChoice{cmd: "claude"}, agentAccessWorkspace, []string{"--permission-mode", "acceptEdits", "--model", "sonnet", "start here"}},
		{"codex plan", assistantChoice{cmd: "codex"}, agentAccessPlan, []string{"--sandbox", "read-only", "--model", "sonnet", "start here"}},
		{"codex edits", assistantChoice{cmd: "codex"}, agentAccessWorkspace, []string{"--sandbox", "workspace-write", "--model", "sonnet", "start here"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentOptionArgs(tc.choice, tc.access, " sonnet ", " start here "); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("args = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestAgentLauncherSelectsProviderPlacementAndInputs(t *testing.T) {
	t.Setenv("ORCHARD_AI_CMD", "claude")
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.repos = []repo.Repo{{Name: "orchard", Path: t.TempDir()}}
	m.resize()
	next, _ := m.openAgentLauncher(m.repos)
	m = next.(model)
	if m.mode != modeAgentLaunch || len(m.agentTargets) != 1 {
		t.Fatalf("launcher state = mode %v, targets %d", m.mode, len(m.agentTargets))
	}

	m.agentChoices = []assistantChoice{{cmd: "claude", label: "Claude Code"}, {cmd: "codex", label: "Codex"}}
	m.agentLaunchRow = agentRowProvider
	next, _ = m.handleAgentLaunchKey(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	if m.selectedAgentChoice().cmd != "codex" {
		t.Fatal("right did not select Codex")
	}
	m.agentLaunchRow = agentRowPlacement
	next, _ = m.handleAgentLaunchKey(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	if m.agentPlacement != agentThisTab {
		t.Fatal("right did not select this-tab placement")
	}

	m.agentLaunchRow = agentRowPrompt
	next, _ = m.handleAgentLaunchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix tests")})
	m = next.(model)
	if m.agentPromptInput.Value() != "fix tests" || !m.agentLaunchEditing {
		t.Fatalf("prompt = %q editing=%v", m.agentPromptInput.Value(), m.agentLaunchEditing)
	}

	out := ansi.Strip(m.View())
	for _, want := range []string{"Launch agent", "Codex", "this terminal tab", "fix tests"} {
		if !strings.Contains(out, want) {
			t.Fatalf("launcher view missing %q\n%s", want, out)
		}
	}
}

func TestMultipleAgentTargetsStayInSeparateTabs(t *testing.T) {
	m := newModel("root", 4)
	m.agentChoices = []assistantChoice{{cmd: "claude", label: "Claude Code"}}
	m.agentTargets = []repo.Repo{{Name: "one", Path: "/one"}, {Name: "two", Path: "/two"}}
	m.agentLaunchRow = agentRowPlacement
	m.agentPlacement = agentNewTab
	m.cycleAgentOption(1)
	if m.agentPlacement != agentNewTab {
		t.Fatal("multiple independent launches cannot share Orchard's current tab")
	}
}

func TestAgentMonitorAppliesFreshActivity(t *testing.T) {
	m := model{repos: []repo.Repo{{Path: "/one"}, {Path: "/two"}}}
	now := time.Now()
	m.applyAgentMonitor(agentMonitorMsg{
		"/one": {claudeSessions: 2, claudeLast: now, codexSessions: 1, codexLast: now.Add(-time.Hour)},
	})
	if m.repos[0].CCSessions != 2 || !m.repos[0].CCLast.Equal(now) || m.repos[0].CodexSessions != 1 {
		t.Fatalf("monitor did not update repo: %+v", m.repos[0])
	}
	if m.repos[1].CCSessions != 0 {
		t.Fatal("monitor changed a repo absent from its result")
	}
}

func TestAgentMonitorTickDoesNotOverlap(t *testing.T) {
	m := newModel("root", 4)
	m.repos = []repo.Repo{{Path: "/one"}}
	m.loading = false
	next, cmd := m.Update(agentMonitorTickMsg(time.Now()))
	m = next.(model)
	if !m.agentMonitorBusy || cmd == nil {
		t.Fatal("first monitor tick should start a scan and re-arm")
	}
	next, cmd = m.Update(agentMonitorTickMsg(time.Now()))
	if !next.(model).agentMonitorBusy || cmd == nil {
		t.Fatal("overlapping tick should only re-arm while scan remains busy")
	}
	next, _ = next.(model).Update(agentMonitorMsg{"/one": {claudeSessions: 1}})
	if next.(model).agentMonitorBusy {
		t.Fatal("monitor result should clear the busy guard")
	}
}

func TestAgentMonitorPreservesDemoActivity(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	repos := demoRepos()
	msg := agentMonitorCmd(repos)().(agentMonitorMsg)
	for _, r := range repos {
		state := msg[r.Path]
		if state.claudeSessions != r.CCSessions || !state.claudeLast.Equal(r.CCLast) || state.codexSessions != r.CodexSessions || !state.codexLast.Equal(r.CodexLast) {
			t.Fatalf("demo activity changed for %s: %+v", r.Name, state)
		}
	}
}
