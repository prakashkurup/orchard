package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildHubRepo creates a repo where `helper` is called by n functions, so it has
// the highest PageRank, and returns a built graph over it.
func buildHubRepo(t *testing.T, n int) *Graph {
	t.Helper()
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		write(fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package p\n\nfunc F%02d() { helper() }\n", i))
	}
	write("helper.go", "package p\n\nfunc helper() {}\n")
	gitInit(t, repo)

	g := newTestGraph(t)
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestRepoMapRankingAndCap(t *testing.T) {
	g := buildHubRepo(t, 10)

	rm, err := g.RepoMap(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rm) > 3 {
		t.Errorf("RepoMap(3) returned %d rows, want ≤3 (cap not enforced)", len(rm))
	}
	if len(rm) == 0 || rm[0].Name != "helper" {
		t.Errorf("top of repo map = %+v, want the most-called symbol 'helper' first (PageRank)", rm)
	}
	if len(rm) >= 2 && rm[0].Rank <= rm[1].Rank {
		t.Errorf("repo map not rank-ordered: %.6f <= %.6f", rm[0].Rank, rm[1].Rank)
	}
}

func TestSearchSymbols(t *testing.T) {
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("auth.go", "package p\n\nfunc Login() {}\n\nfunc Logout() {}\n\nfunc validate() {}\n")
	gitInit(t, repo)
	g := newTestGraph(t)
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	res, err := g.SearchSymbols("Log", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, d := range res {
		found[d.Name] = true
	}
	if !found["Login"] || !found["Logout"] {
		t.Errorf("SearchSymbols(\"Log\") = %+v, want Login and Logout", res)
	}
	if found["validate"] {
		t.Errorf("SearchSymbols(\"Log\") unexpectedly matched validate")
	}
}

func TestWhoCallsPaginationCap(t *testing.T) {
	g := buildHubRepo(t, 10) // helper has 10 callers

	page1, err := g.WhoCalls("helper", 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	page2, err := g.WhoCalls("helper", 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 4 || len(page2) != 4 {
		t.Fatalf("pagination: page1=%d page2=%d, want 4 and 4", len(page1), len(page2))
	}
	seen := map[string]bool{}
	for _, c := range append(page1, page2...) {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		if seen[key] {
			t.Errorf("pagination overlap at %s", key)
		}
		seen[key] = true
	}
	if len(seen) != 8 {
		t.Errorf("two pages of 4 yielded %d distinct call sites, want 8", len(seen))
	}
}
