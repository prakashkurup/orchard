package graph

import (
	"os"
	"time"
)

// GraphState is a quick, read-only snapshot of a repo's built graph, for UI
// badges and the detail view. It parses and builds nothing.
type GraphState struct {
	HeadCommit string
	DirtyFiles int
	BuiltAt    time.Time
	Files      int
	Symbols    int
	Edges      int
	Tiers      map[Tier]int
	Trust      []LangTrust
	Stale      bool
	Changed    int
}

// RemoveForRepo deletes a repo's code-graph database and its WAL/SHM sidecars.
// It returns ok=true if a graph existed and was removed. A deleted graph is
// simply rebuilt on the next build, so this is a safe, reversible cleanup.
func RemoveForRepo(repoAbs string) (bool, error) {
	path, err := DBPath(repoAbs)
	if err != nil {
		return false, err
	}
	existed := false
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		err := os.Remove(p)
		switch {
		case err == nil:
			if p == path {
				existed = true
			}
		case !os.IsNotExist(err):
			return existed, err
		}
	}
	return existed, nil
}

// StateFor returns the stored graph snapshot for a repo, or ok=false if no
// (non-empty) graph has been built yet. It opens the existing DB read-only and
// never creates one, so it is cheap enough to call per-repo off the UI thread.
func StateFor(repoAbs string) (GraphState, bool) {
	path, err := DBPath(repoAbs)
	if err != nil {
		return GraphState{}, false
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return GraphState{}, false
	}
	g, err := Open(path)
	if err != nil {
		return GraphState{}, false
	}
	defer g.Close()
	files, symbols, edges, _ := g.Counts()
	if symbols == 0 {
		return GraphState{}, false // a graph with no symbols is not useful yet
	}
	f := g.Freshness()
	return GraphState{
		HeadCommit: f.HeadCommit,
		DirtyFiles: f.DirtyFiles,
		BuiltAt:    f.BuiltAt,
		Files:      files,
		Symbols:    symbols,
		Edges:      edges,
		Tiers:      g.TierCounts(),
		Trust:      g.TrustLabels(),
	}, true
}
