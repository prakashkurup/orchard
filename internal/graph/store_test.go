package graph

import (
	"path/filepath"
	"testing"
)

func newTestGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func TestStoreLoadAndQuery(t *testing.T) {
	g := newTestGraph(t)
	files := []FileGraph{
		{
			File: fileMeta{Path: "auth.go", Lang: "go", SHA: "x", Bytes: 10, Tier: TierPrecise},
			Symbols: []Symbol{
				{Name: "Login", Kind: KindFunc, StartLine: 1, EndLine: 3},
				{Name: "validate", Kind: KindFunc, StartLine: 5, EndLine: 6},
			},
			Edges: []Edge{{SrcName: "Login", DstName: "validate", Kind: EdgeCall, Line: 2}},
		},
		{
			File:    fileMeta{Path: "api.go", Lang: "go", SHA: "y", Bytes: 10, Tier: TierPrecise},
			Symbols: []Symbol{{Name: "Handle", Kind: KindFunc, StartLine: 1, EndLine: 3}},
			Edges:   []Edge{{SrcName: "Handle", DstName: "Login", Kind: EdgeCall, Line: 2}},
		},
	}
	if err := g.store.replace(files); err != nil {
		t.Fatal(err)
	}

	defs, err := g.FindDef("Login", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Path != "auth.go" {
		t.Errorf("FindDef(Login) = %+v, want one def in auth.go", defs)
	}

	callers, err := g.WhoCalls("Login", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].Caller != "Handle" || callers[0].Path != "api.go" {
		t.Errorf("WhoCalls(Login) = %+v, want Handle in api.go", callers)
	}

	// Changing validate transitively affects Login and Handle.
	impact, err := g.BlastRadius("validate", 5, 1000)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range impact {
		names[r.Name] = true
	}
	if !names["Login"] || !names["Handle"] {
		t.Errorf("BlastRadius(validate) = %+v, want it to include Login and Handle", impact)
	}
}
