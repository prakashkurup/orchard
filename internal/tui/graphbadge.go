package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/graph"
	"github.com/prakashkurup/orchard/internal/repo"
)

// graphBadgeState is a repo's code-graph status shown as a row badge.
type graphBadgeState uint8

const (
	graphBadgeNone  graphBadgeState = iota // no graph built (default / absent)
	graphBadgeFresh                        // built at the current HEAD, clean tree
	graphBadgeStale                        // built, but HEAD moved or tree is dirty
)

// graphStatesMsg carries freshly-read per-repo code-graph states.
type graphStatesMsg struct{ states map[string]graphBadgeState }

// graphStatesCmd reads each repo's stored graph snapshot off the UI thread and
// classifies it fresh/stale against the repo's current HEAD and working tree. A
// repo with no graph is simply absent from the map (→ graphBadgeNone).
func graphStatesCmd(repos []repo.Repo) tea.Cmd {
	type want struct {
		path, head string
		dirty      bool
	}
	wants := make([]want, len(repos))
	for i, r := range repos {
		wants[i] = want{r.Path, r.Head, r.Dirty}
	}
	return func() tea.Msg {
		states := make(map[string]graphBadgeState, len(wants))
		for _, w := range wants {
			st, ok := graph.StateFor(w.path)
			if !ok {
				continue
			}
			if st.HeadCommit != "" && st.HeadCommit == w.head && !w.dirty && st.DirtyFiles == 0 {
				states[w.path] = graphBadgeFresh
			} else {
				states[w.path] = graphBadgeStale
			}
		}
		return graphStatesMsg{states: states}
	}
}
