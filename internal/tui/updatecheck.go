package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/update"
)

type updateMsg struct {
	tag       string
	available bool
}

func updateCheckCmd(version string) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return updateMsg{tag: "v0.7.0", available: true} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tag, available := update.Check(ctx, update.Current(version))
		return updateMsg{tag: tag, available: available}
	}
}
