package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestAgentResumeArgsDispatch(t *testing.T) {
	m := newModel("root", 4)

	m.assistantCmd = "claude"
	if got := strings.Join(m.agentResumeLastArgs(), " "); got != "--continue" {
		t.Errorf("claude resume-last = %q", got)
	}
	if got := strings.Join(m.agentResumeArgs("abc123"), " "); got != "--resume abc123" {
		t.Errorf("claude resume = %q", got)
	}

	m.assistantCmd = "codex"
	if got := strings.Join(m.agentResumeLastArgs(), " "); got != "resume --last" {
		t.Errorf("codex resume-last = %q", got)
	}
	if got := strings.Join(m.agentResumeArgs("abc123"), " "); got != "resume abc123" {
		t.Errorf("codex resume = %q", got)
	}
}

func TestHeadlessArgs(t *testing.T) {
	if got := strings.Join(claudeHeadlessArgs("p"), " "); got != "-p p --output-format json" {
		t.Errorf("claude headless = %q", got)
	}
	got := strings.Join(codexHeadlessArgs("p", "/tmp/out"), " ")
	for _, want := range []string{"exec", "--sandbox read-only", "--output-last-message /tmp/out"} {
		if !strings.Contains(got, want) {
			t.Errorf("codex headless missing %q: %q", want, got)
		}
	}
}

func TestSessionsOpenForCodex(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	m := newModel("root", 4)
	m.width, m.height = 120, 40
	m.resize()
	m.assistantCmd = "codex"

	mm, cmd := m.openSessions(repo.Repo{Name: "acme-web", Path: "/x/acme-web"})
	m = mm.(model)
	if m.mode != modeSessions {
		t.Fatalf("codex session history should open, mode = %v", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected a sessions load cmd")
	}
	msg := cmd().(sessionsMsg)
	if len(msg.sessions) == 0 {
		t.Fatal("expected demo codex sessions")
	}
	// an assistant without history still gets the status note
	m2 := newModel("root", 4)
	m2.assistantCmd = "aider"
	mm2, _ := m2.openSessions(repo.Repo{Name: "x", Path: "/x"})
	if mm2.(model).mode == modeSessions {
		t.Error("unknown assistant should not open session history")
	}
}

func TestPanelRowsMirrorRender(t *testing.T) {
	mk := func(cl, cx int) model {
		m := newModel("root", 4)
		u := claude.Usage{TotalSessions: cl, TotalTurns: cl * 10, Models: map[string]int{"opus-4.8": 5}, Repos: []claude.RepoUsage{{Name: "a", Turns: 5}}}
		c := claude.Usage{TotalSessions: cx, TotalTurns: cx * 7, Models: map[string]int{"gpt-5.5": 3}}
		m.claudeUsage, m.codexUsage = &u, &c
		return m
	}
	for _, tc := range []struct {
		cl, cx, want int
	}{
		{3, 2, 4}, // claude + codex
		{3, 0, 3}, // claude only
		{0, 2, 2}, // codex only
		{0, 0, 0}, // neither (panel hidden)
	} {
		m := mk(tc.cl, tc.cx)
		if got := m.claudePanelRows(); got != tc.want {
			t.Errorf("claudePanelRows(cl=%d,cx=%d) = %d, want %d", tc.cl, tc.cx, got, tc.want)
		}
		if tc.want == 0 {
			if m.showClaudePanel() {
				t.Error("panel should be hidden with no usage")
			}
			continue
		}
		rendered := len(strings.Split(m.claudePanel(100), "\n"))
		if rendered != tc.want {
			t.Errorf("claudePanel renders %d rows, claudePanelRows says %d (cl=%d,cx=%d)", rendered, tc.want, tc.cl, tc.cx)
		}
		for _, ln := range strings.Split(m.claudePanel(100), "\n") {
			if w := lipgloss.Width(ln); w != 100 {
				t.Fatalf("panel line width %d, want 100", w)
			}
		}
	}
}

func TestDetailShowsCodexSection(t *testing.T) {
	t.Setenv("ORCHARD_DEMO", "1")
	m := newModel("root", 4)
	m.width, m.height = 140, 40
	m.resize()
	m.repos = demoRepos()
	m.detailRepo = "/orchard-demo/payments-api"
	msg := detailCmd(repo.Repo{Name: "payments-api", Path: "/orchard-demo/payments-api"})().(detailMsg)
	m.detail = &detailState{repo: m.repoByPath(msg.path), info: msg.info, langs: msg.langs, sessions: msg.sessions,
		commitsSince: msg.commitsSince, touched: msg.touched, codexSessions: msg.codexSessions, codexTouched: msg.codexTouched}
	out := ansiPattern.ReplaceAllString(m.detailBody(140), "")
	if !strings.Contains(out, "Codex") || !strings.Contains(out, "what Codex has done in this repo") {
		t.Fatal("detail body should include the Codex section")
	}
	if !strings.Contains(out, "Claude Code") {
		t.Fatal("the Claude section must still render alongside Codex")
	}
}
