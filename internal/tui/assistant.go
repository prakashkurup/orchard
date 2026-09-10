package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type assistantChoice struct {
	cmd   string
	label string
}

// availableAssistants returns every selectable built-in assistant. An explicit
// ORCHARD_AI_CMD remains authoritative and is represented as the only choice.
func availableAssistants() []assistantChoice {
	if env := strings.TrimSpace(os.Getenv("ORCHARD_AI_CMD")); env != "" {
		return []assistantChoice{{cmd: env, label: assistantDisplayName(env)}}
	}
	var out []assistantChoice
	for _, c := range []string{"claude", "codex"} {
		if _, err := exec.LookPath(c); err == nil {
			out = append(out, assistantChoice{cmd: c, label: assistantDisplayName(c)})
		}
	}
	return out
}

// resolveAssistant chooses the AI coding assistant launched by `c`: an explicit
// $ORCHARD_AI_CMD if set (e.g. "claude" or "codex"), otherwise the first of
// claude / codex found on PATH. label is the short name shown in the footer; ok
// is false when nothing is available (the `c` hint then hides).
func resolveAssistant() (cmd, label string, ok bool) {
	if choices := availableAssistants(); len(choices) > 0 {
		return choices[0].cmd, choices[0].label, true
	}
	return "", "", false
}

func assistantDisplayName(cmd string) string {
	label := strings.ToLower(assistantLabel(cmd))
	switch {
	case strings.Contains(label, "claude"):
		return "Claude Code"
	case strings.Contains(label, "codex"):
		return "Codex"
	default:
		return assistantLabel(cmd)
	}
}

func assistantLabel(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return cmd
	}
	return filepath.Base(fields[0])
}
