package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveAssistant chooses the AI coding assistant launched by `c`: an explicit
// $ORCHARD_AI_CMD if set (e.g. "claude", "copilot", or "gh copilot"), otherwise
// the first of claude / copilot found on PATH. label is the short name shown in
// the footer; ok is false when nothing is available (the `c` hint then hides).
func resolveAssistant() (cmd, label string, ok bool) {
	if env := strings.TrimSpace(os.Getenv("ORCHARD_AI_CMD")); env != "" {
		return env, assistantLabel(env), true
	}
	for _, c := range []string{"claude", "copilot"} {
		if _, err := exec.LookPath(c); err == nil {
			return c, c, true
		}
	}
	return "", "", false
}

func assistantLabel(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return cmd
	}
	return filepath.Base(fields[len(fields)-1])
}
