package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

// commitMsgPromptHeadless asks for a clean, paste-ready message with no extras,
// since the output is shown verbatim in a window (not a chat).
const commitMsgPromptHeadless = "Write a single git commit message for the diff below. " +
	"Use conventional-commit style and imperative mood. First line: a focused subject under 72 characters; use a scope only when obvious from the diff. " +
	"If a body adds value, add one blank line and then 2-4 concise bullet points starting with '- '; no paragraph body. " +
	"Do not mention details that are not evident in the diff. " +
	"Output ONLY the commit message text: no code fences, no quotes, no preamble."

type commitMsgMsg struct {
	path string
	text string
	err  error
}

// commitTickMsg advances the "drafting" animation while claude -p runs.
type commitTickMsg struct{}

func commitTick() tea.Cmd {
	return tea.Tick(110*time.Millisecond, func(time.Time) tea.Msg { return commitTickMsg{} })
}

// openCommitMessage drafts a commit message for a repo. With Claude Code it runs
// headlessly (claude -p) and shows the result in a window; other assistants fall
// back to a terminal session with the same prompt.
func (m model) openCommitMessage(r repo.Repo) (tea.Model, tea.Cmd) {
	if m.assistantCmd == "" {
		m.status = "no AI assistant found (install claude or set ORCHARD_AI_CMD)"
		return m, nil
	}
	if !m.assistantIsClaude() {
		return m.runAssistant(r.Path, []string{commitMsgPrompt}, nil, "drafting a commit message · "+r.Name, nil)
	}
	m.commitMsgRepo = r
	m.commitMsg = ""
	m.commitMsgErr = ""
	m.commitMsgCopied = false
	m.commitMsgLoading = true
	m.commitMsgFrame = 0
	m.returnMode = m.mode
	m.mode = modeCommitMsg
	return m, tea.Batch(commitMsgCmd(m.assistantCmd, r), commitTick())
}

func commitMsgCmd(assistantCmd string, r repo.Repo) tea.Cmd {
	if demoMode() {
		// simulate the headless draft delay so the drafting animation is visible
		return tea.Tick(1600*time.Millisecond, func(time.Time) tea.Msg {
			return commitMsgMsg{path: r.Path, text: demoCommitMsg()}
		})
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		diff, _ := orchardgit.Diff(ctx, r.Path)
		if strings.TrimSpace(diff) == "" {
			return commitMsgMsg{path: r.Path, err: errors.New("no changes to describe")}
		}
		if len(diff) > 12000 { // cap to a sane token budget
			diff = diff[:12000] + "\n…(diff truncated)…"
		}
		fields := strings.Fields(assistantCmd)
		args := append(append([]string{}, fields[1:]...), "-p", commitMsgPromptHeadless+"\n\n"+diff, "--output-format", "json")
		cmd := exec.CommandContext(ctx, fields[0], args...)
		cmd.Dir = r.Path
		out, err := cmd.Output()
		if err != nil {
			return commitMsgMsg{path: r.Path, err: err}
		}
		return commitMsgMsg{path: r.Path, text: parseAssistantOutput(string(out))}
	}
}

// parseAssistantOutput reads the message from `claude -p --output-format json`
// ({"result": "..."}), falling back to cleaning the raw text if it is not JSON.
func parseAssistantOutput(out string) string {
	var r struct {
		Result string `json:"result"`
	}
	if json.Unmarshal([]byte(out), &r) == nil && strings.TrimSpace(r.Result) != "" {
		return cleanCommitMsg(r.Result)
	}
	return cleanCommitMsg(out)
}

// wrapText soft-wraps plain text to width columns, breaking on spaces and
// preserving blank lines (so a subject/body split stays readable).
func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// cleanCommitMsg trims whitespace and strips any stray code-fence lines so only
// the message itself is shown.
func cleanCommitMsg(s string) string {
	var keep []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			continue
		}
		keep = append(keep, ln)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

func (m model) handleCommitMsgKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = m.returnMode
		return m, nil
	case "y", "c":
		if m.commitMsg != "" {
			_ = clipboard.WriteAll(m.commitMsg)
			m.commitMsgCopied = true
		}
	case "r":
		if !m.commitMsgLoading {
			m.commitMsg, m.commitMsgErr, m.commitMsgCopied, m.commitMsgLoading = "", "", false, true
			m.commitMsgFrame = 0
			return m, tea.Batch(commitMsgCmd(m.assistantCmd, m.commitMsgRepo), commitTick())
		}
	}
	return m, nil
}

// draftBuddy renders one frame of the drafting indicator: a Claude-orange
// sparkle sweeping back and forth across a short track, with a fading tail.
func draftBuddy(frame int) string {
	const slots = 7
	cycle := frame % (2 * (slots - 1))
	pos, dir := cycle, 1
	if cycle >= slots {
		pos = 2*(slots-1) - cycle
	}
	if cycle >= slots-1 {
		dir = -1
	}
	var b strings.Builder
	for i := 0; i < slots; i++ {
		switch {
		case i == pos:
			b.WriteString(panelFG(claudeC).Bold(true).Render("✦"))
		case i == pos-dir:
			b.WriteString(panelFG(orange).Render("✧"))
		default:
			b.WriteString(panelFG(muted).Render("·"))
		}
	}
	return b.String()
}

func (m model) commitMsgView(width int) string {
	fg := panelFG
	inner := clamp(width-16, 44, 84)
	return modalBox(inner, func(add func(string)) {
		add(fg(claudeC).Bold(true).Render("✦ Commit message") + fg(muted).Render("  · "+m.commitMsgRepo.Name))
		add("")
		switch {
		case m.commitMsgLoading:
			add(fg(muted).Render("  ") + draftBuddy(m.commitMsgFrame) + fg(muted).Render("  drafting with Claude"))
		case m.commitMsgErr != "":
			add(fg(red).Render("  " + fit(m.commitMsgErr, inner-4)))
		default:
			for i, ln := range wrapText(m.commitMsg, inner-4) {
				st := fg(ice)
				if i == 0 {
					st = fg(ice).Bold(true) // the subject line leads, like a real commit
				}
				add(st.Render("  " + ln))
			}
		}
		add("")
		switch {
		case m.commitMsgLoading:
			add(fg(muted).Render("esc cancel"))
		case m.commitMsgCopied:
			add(fg(green).Render("  ✓ copied") + fg(muted).Render("    r regenerate · esc close"))
		case m.commitMsgErr == "":
			add(fg(muted).Render("y copy · r regenerate · esc close"))
		default:
			add(fg(muted).Render("r retry · esc close"))
		}
	})
}
