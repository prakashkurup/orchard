package tui

import (
	"testing"
	"time"

	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

func logicModel() model {
	m := newModel("root", 8)
	m.repos = []repo.Repo{
		{Name: "alpha", Path: "/a", Branch: "main", DefaultBranch: "main", Display: repo.DisplayClean},
		{Name: "bravo", Path: "/b", Branch: "main", DefaultBranch: "main", Dirty: true, ChangedFiles: 2, Display: repo.DisplayDirty},
		{Name: "charlie", Path: "/c", Branch: "feat/x", DefaultBranch: "main", Display: repo.DisplayFeature},
		{Name: "delta", Path: "/d", Branch: "main", DefaultBranch: "main", Behind: 3, Display: repo.DisplayBehind},
	}
	return m
}

func visibleNames(m model) []string {
	var out []string
	for _, it := range m.view {
		if !it.header {
			out = append(out, m.repos[it.repoIdx].Name)
		}
	}
	return out
}

func TestRebuildViewSortName(t *testing.T) {
	m := logicModel()
	m.sortMode = sortName
	m.rebuildView()
	got := visibleNames(m)
	want := []string{"alpha", "bravo", "charlie", "delta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortName order = %v, want %v", got, want)
		}
	}
}

func TestRebuildViewSortAttention(t *testing.T) {
	m := logicModel()
	m.sortMode = sortAttention
	m.rebuildView()
	// clean repo (alpha) must come last under attention sort
	got := visibleNames(m)
	if got[len(got)-1] != "alpha" {
		t.Fatalf("attention sort should put clean last, got %v", got)
	}
}

func TestQuickFilterDirty(t *testing.T) {
	m := logicModel()
	m.quick = filterDirty
	m.rebuildView()
	got := visibleNames(m)
	if len(got) != 1 || got[0] != "bravo" {
		t.Fatalf("dirty filter = %v, want [bravo]", got)
	}
}

func TestTextFilter(t *testing.T) {
	m := logicModel()
	m.filterText = "char"
	m.rebuildView()
	if got := visibleNames(m); len(got) != 1 || got[0] != "charlie" {
		t.Fatalf("text filter = %v, want [charlie]", got)
	}
}

func TestGroupingInsertsHeadersAndCursorSkipsThem(t *testing.T) {
	m := logicModel()
	m.grouped = true
	m.rebuildView()
	headers := 0
	for _, it := range m.view {
		if it.header {
			headers++
		}
	}
	if headers == 0 {
		t.Fatal("grouping should insert header rows")
	}
	// cursor must land on a repo row, never a header
	m.cursor = 0
	m.normalizeCursor()
	if m.view[m.cursor].header {
		t.Fatal("cursor landed on a header row")
	}
	// moving down stays on repo rows
	for i := 0; i < len(m.view); i++ {
		m.moveCursor(1)
		if m.cursor < len(m.view) && m.view[m.cursor].header {
			t.Fatal("moveCursor landed on a header row")
		}
	}
}

func TestPullTargetsSelectionVsCursor(t *testing.T) {
	m := logicModel()
	m.rebuildView()
	m.cursor = m.snapToRepo(0, 1)

	// no selection -> just the repo under the cursor
	cur, _ := m.currentRepo()
	if got := m.pullTargets(); len(got) != 1 || got[0].Path != cur.Path {
		t.Fatalf("no selection -> current repo (%s), got %v", cur.Path, got)
	}

	// with a selection -> exactly the selected repos
	m.selected["/b"] = true
	m.selected["/d"] = true
	got := m.pullTargets()
	paths := map[string]bool{}
	for _, r := range got {
		paths[r.Path] = true
	}
	if len(got) != 2 || !paths["/b"] || !paths["/d"] {
		t.Fatalf("selection -> selected repos, got %v", got)
	}
}

func TestSelectionTargets(t *testing.T) {
	m := logicModel()
	m.rebuildView()
	m.selected["/c"] = true
	got := m.selectionTargets()
	if len(got) != 1 || got[0].Name != "charlie" {
		t.Fatalf("selectionTargets = %v", got)
	}
}

func TestJumpToNextNew(t *testing.T) {
	m := logicModel()
	m.rebuildView()
	m.newByPath = map[string]int{"/d": 4}
	m.cursor = m.snapToRepo(0, 1)
	m.jumpToNextNew()
	if r, _ := m.currentRepo(); r.Name != "delta" {
		t.Fatalf("jumpToNextNew landed on %q, want delta", r.Name)
	}
}

func TestApplyOneResultMerges(t *testing.T) {
	m := logicModel()
	updated := m.repos[1]
	updated.Dirty = false
	updated.Display = repo.DisplayClean
	m.applyOneResult(orchardgit.PullResult{Repo: updated, Status: "pulled"})
	if m.repos[1].Display != repo.DisplayClean {
		t.Fatal("applyOneResult did not merge the updated repo")
	}
}

func TestDropMissingSelections(t *testing.T) {
	m := logicModel()
	m.selected["/gone"] = true
	m.selected["/a"] = true
	m.dropMissingSelections()
	if m.selected["/gone"] {
		t.Fatal("stale selection not dropped")
	}
	if !m.selected["/a"] {
		t.Fatal("valid selection dropped")
	}
}

func TestSortIndicesSynced(t *testing.T) {
	m := logicModel()
	now := time.Now()
	m.repos[0].LastFetched = now                       // alpha newest
	m.repos[1].LastFetched = now.Add(-100 * time.Hour) // bravo oldest
	m.repos[2].LastFetched = now.Add(-10 * time.Hour)  // charlie
	m.repos[3].LastFetched = now.Add(-1 * time.Hour)   // delta
	m.sortMode = sortSynced
	m.rebuildView()
	if got := visibleNames(m); got[0] != "bravo" {
		t.Fatalf("synced sort (oldest first) = %v, want bravo first", got)
	}
}

func TestCountStates(t *testing.T) {
	m := logicModel()
	s := countStates(m.repos)
	if s.clean != 1 || s.dirty != 1 || s.behind != 1 {
		t.Fatalf("countStates = %+v", s)
	}
}

func TestStatePresentationHelpers(t *testing.T) {
	if statusText(repo.DisplayClean) != "✓" {
		t.Errorf("clean glyph = %q", statusText(repo.DisplayClean))
	}
	if colorForState(repo.DisplayDirty) != yellow {
		t.Error("dirty should be yellow")
	}
	if attentionRank(repo.DisplayError) >= attentionRank(repo.DisplayClean) {
		t.Error("error should rank before clean")
	}
	if groupTitle(repo.DisplayFeature) == "" {
		t.Error("group title missing for feature")
	}
}
