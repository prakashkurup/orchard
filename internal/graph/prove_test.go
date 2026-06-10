package graph

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestProveRealRepos builds the graph for each repo in ORCHARD_GRAPH_PROVE
// (comma-separated paths) and logs stats + sample queries. It is skipped in
// normal runs; it's the manual proof harness for the Phase-1 success gate.
//
//	ORCHARD_GRAPH_PROVE=/path/a,/path/b go test ./internal/graph -run TestProveRealRepos -v
func TestProveRealRepos(t *testing.T) {
	repos := os.Getenv("ORCHARD_GRAPH_PROVE")
	if repos == "" {
		t.Skip("set ORCHARD_GRAPH_PROVE=repo1,repo2,… to run the real-repo proof")
	}
	for _, repo := range strings.Split(repos, ",") {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		g := newTestGraph(t)
		start := time.Now()
		stats, err := g.Build(context.Background(), repo, DefaultRegistry())
		dur := time.Since(start)
		if err != nil {
			t.Errorf("%s: build failed: %v", repo, err)
			continue
		}
		t.Logf("── %s  (built in %s)", repo, dur.Round(time.Millisecond))
		t.Logf("   files=%d  symbols=%d  edges=%d  resolved=%d (%.0f%%)  unsupported=%d  diagnostics=%d",
			stats.Files, stats.Symbols, stats.Edges, stats.ResolvedEdges,
			pctOf(stats.ResolvedEdges, stats.Edges), stats.Unsupported, stats.Diagnostics)
		for tier, n := range stats.ByTier {
			t.Logf("   tier %-11s %d files", tier, n)
		}
		if rm, _ := g.RepoMap(5); len(rm) > 0 {
			names := make([]string, len(rm))
			for i, m := range rm {
				names[i] = m.Name
			}
			t.Logf("   repo_map top-5 (PageRank): %v", names)
		}
		for _, name := range g.store.topByInbound(3) {
			t0 := time.Now()
			callers, _ := g.WhoCalls(name, 50, 0)
			dWho := time.Since(t0)
			t1 := time.Now()
			impact, _ := g.BlastRadius(name, 6, 1000)
			dBlast := time.Since(t1)
			t.Logf("   query %-22q callers=%d (%s)  blast-radius=%d (%s)",
				name, len(callers), dWho.Round(time.Microsecond), len(impact), dBlast.Round(time.Microsecond))
		}
	}
}

func pctOf(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
