package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSearchSymbolsSubstring exercises substring search (FTS5 trigram when
// available, LIKE otherwise) through the public API, including a match in the
// MIDDLE of a camelCase identifier.
func TestSearchSymbolsSubstring(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"),
		[]byte("package p\n\nfunc getUserName() string { return \"\" }\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)
	g := newTestGraph(t)
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	res, err := g.SearchSymbols("serna", 10) // middle of getUser·Na·me
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range res {
		if d.Name == "getUserName" {
			found = true
		}
	}
	if !found {
		t.Errorf("SearchSymbols(\"serna\") = %+v, want getUserName (middle substring)", res)
	}
}
