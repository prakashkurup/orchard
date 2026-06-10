package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/update"
)

type updateMsg struct {
	tag       string
	available bool
}

// demoNextTag fabricates a plausible "newer" tag for demo-mode screenshots: the
// running version's next minor, so the update nudge never goes stale.
func demoNextTag(version string) string {
	v := strings.TrimPrefix(update.Current(version), "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		if major, err1 := strconv.Atoi(parts[0]); err1 == nil {
			if minor, err2 := strconv.Atoi(parts[1]); err2 == nil {
				return fmt.Sprintf("v%d.%d.0", major, minor+1)
			}
		}
	}
	return "v0.7.0"
}

func updateCheckCmd(version string) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return updateMsg{tag: demoNextTag(version), available: true} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tag, available := update.Check(ctx, update.Current(version))
		return updateMsg{tag: tag, available: available}
	}
}
