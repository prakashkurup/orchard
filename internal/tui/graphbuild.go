package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/graph"
	"github.com/prakashkurup/orchard/internal/repo"
)

// startGraphBuild kicks off a sequential, animated build of the given repos
// (one at a time so the dashboard can show N/M progress). Shared by the dashboard
// (B over the selection) and the detail page (B for the focused repo).
func (m model) startGraphBuild(targets []repo.Repo) (model, tea.Cmd) {
	m.graphBuilding = true
	m.graphBuildingPath = targets[0].Path
	m.status = buildProgressLabel(targets, 0)
	return m, tea.Batch(buildGraphStepCmd(targets, 0, buildAccum{}), m.spinner.Tick)
}

// deleteGraph removes the code graph for each target. A graph is a cache served
// to the agent, so deleting it is safe — rebuild with B. Shared by the dashboard
// and the detail page.
func (m model) deleteGraph(targets []repo.Repo) (model, tea.Cmd) {
	removed := 0
	for _, r := range targets {
		if ok, _ := graph.RemoveForRepo(r.Path); ok {
			removed++
		}
	}
	switch {
	case removed == 0:
		m.status = "no code graph to delete"
	case len(targets) == 1:
		m.status = "deleted code graph · " + targets[0].Name
	default:
		m.status = fmt.Sprintf("deleted %d code graphs", removed)
	}
	return m, graphStatesCmd(m.repos)
}

// toggleGraphWiring flips the session opt-out for auto-wiring the graph MCP on
// launch (the visible counterpart to ORCHARD_GRAPH_MCP=0).
func (m model) toggleGraphWiring() (tea.Model, tea.Cmd) {
	m.graphWireOff = !m.graphWireOff
	if m.graphWireOff {
		m.status = "code-graph auto-wiring OFF · launches won't wire the MCP (this session)"
	} else {
		m.status = "code-graph auto-wiring ON · launches wire the graph MCP"
	}
	return m, nil
}

// buildAccum accumulates results across a multi-repo build so progress and the
// final summary can be reported as each repo finishes.
type buildAccum struct {
	built    int
	files    int
	symbols  int
	edges    int
	last     buildResult
	failures []buildFailure
}

type buildResult struct {
	name    string
	files   int
	symbols int
	edges   int
	err     error
}

type buildFailure struct {
	name string
	err  error
}

// graphProgressMsg reports that targets[idx] finished building; the Update loop
// either kicks off the next repo (with the spinner animating) or, on the last
// one, shows the summary and refreshes the per-repo badges.
type graphProgressMsg struct {
	targets []repo.Repo
	idx     int
	acc     buildAccum
}

// buildGraphStepCmd builds (or refreshes) the code graph for one target off the
// UI thread and reports progress. Building one repo per command (rather than all
// in a single command) is what lets the dashboard show "N/M · repo" while the
// spinner animates. The graph DBs live under orchard's config dir and are what
// `orchard mcp` serves to the agent.
func buildGraphStepCmd(targets []repo.Repo, idx int, acc buildAccum) tea.Cmd {
	return func() tea.Msg {
		r := targets[idx]
		res := buildResult{name: r.Name}
		if g, err := graph.OpenForRepo(r.Path); err != nil {
			res.err = err
		} else {
			st, berr := g.Build(context.Background(), r.Path, graph.DefaultRegistry())
			g.Close()
			if berr != nil {
				res.err = berr
			} else {
				res.files = st.Files
				res.symbols = st.Symbols
				res.edges = st.Edges
				acc.built++
				acc.files += st.Files
				acc.symbols += st.Symbols
				acc.edges += st.Edges
			}
		}
		if res.err != nil {
			acc.failures = append(acc.failures, buildFailure{name: r.Name, err: res.err})
		}
		acc.last = res
		return graphProgressMsg{targets: targets, idx: idx, acc: acc}
	}
}

// buildProgressLabel is the status shown while a build is in flight; it includes
// N/M and the current repo when more than one is selected.
func buildProgressLabel(targets []repo.Repo, idx int) string {
	name := targets[idx].Name
	if len(targets) == 1 {
		return "tending orchard · building code graph · " + name + "…"
	}
	return fmt.Sprintf("tending orchard · %d/%d · %s…", idx+1, len(targets), name)
}

func buildQueueLabel(targets []repo.Repo, next int, acc buildAccum) string {
	if acc.last.err != nil {
		return fmt.Sprintf("tending orchard · %s failed · next %d/%d · %s…",
			acc.last.name, next+1, len(targets), targets[next].Name)
	}
	return fmt.Sprintf("tending orchard · %s: %d files / %d symbols · next %d/%d · %s…",
		acc.last.name, acc.last.files, acc.last.symbols, next+1, len(targets), targets[next].Name)
}

// buildSummary is the status shown once every target has been built.
func buildSummary(acc buildAccum) string {
	failCount := len(acc.failures)
	switch {
	case acc.built == 0 && failCount > 0:
		return "graph build failed · " + failureNames(acc.failures) + " · retry failed with B"
	case failCount > 0:
		return fmt.Sprintf("tended %d/%d graph%s · %d files / %d symbols · failed: %s · retry failed with B",
			acc.built, acc.built+failCount, pluralSuffix(acc.built), acc.files, acc.symbols, failureNames(acc.failures))
	case acc.built == 1:
		return fmt.Sprintf("tended code graph · %d files / %d symbols / %d edges", acc.files, acc.symbols, acc.edges)
	default:
		return fmt.Sprintf("tended %d code graphs · %d files / %d symbols / %d edges", acc.built, acc.files, acc.symbols, acc.edges)
	}
}

func failureNames(failures []buildFailure) string {
	if len(failures) == 0 {
		return ""
	}
	names := make([]string, 0, min(len(failures), 3))
	for i, f := range failures {
		if i >= 3 {
			break
		}
		names = append(names, f.name)
	}
	if len(failures) > len(names) {
		names = append(names, fmt.Sprintf("+%d more", len(failures)-len(names)))
	}
	return strings.Join(names, ", ")
}
