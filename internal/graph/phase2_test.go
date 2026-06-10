package graph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// dumpGraph returns a sorted, stable textual snapshot of every symbol and edge,
// so two graphs can be compared for byte-identical equality.
func dumpGraph(t *testing.T, g *Graph) []string {
	t.Helper()
	var out []string
	srows, err := g.store.db.Query(
		`SELECT f.path, s.name, s.kind, s.start_line, s.end_line FROM symbols s JOIN files f ON f.id=s.file_id`)
	if err != nil {
		t.Fatal(err)
	}
	for srows.Next() {
		var path, name, kind string
		var sl, el int
		srows.Scan(&path, &name, &kind, &sl, &el)
		out = append(out, fmt.Sprintf("S|%s|%s|%s|%d|%d", path, name, kind, sl, el))
	}
	srows.Close()
	erows, err := g.store.db.Query(
		`SELECT f.path, COALESCE(cs.name,''), e.dst_name, e.kind, e.line, e.confidence
		   FROM edges e JOIN files f ON f.id=e.ref_file_id LEFT JOIN symbols cs ON cs.id=e.src_id`)
	if err != nil {
		t.Fatal(err)
	}
	for erows.Next() {
		var path, src, dst, kind, conf string
		var line int
		erows.Scan(&path, &src, &dst, &kind, &line, &conf)
		out = append(out, fmt.Sprintf("E|%s|%s|%s|%s|%d|%s", path, src, dst, kind, line, conf))
	}
	erows.Close()
	sort.Strings(out)
	return out
}

func gitStageAll(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "add", "-A")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

// TestIncrementalEqualsFull is the Phase-2 core gate: an incremental Update after
// editing one file must produce a byte-identical graph to a full rebuild, and
// must reuse the unchanged files (re-parsing only the edited one), in <1 s.
func TestIncrementalEqualsFull(t *testing.T) {
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const n = 50
	for i := 0; i < n; i++ {
		write(fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package p\n\nfunc F%02d() { helper() }\n", i))
	}
	write("helper.go", "package p\n\nfunc helper() {}\n")
	gitInit(t, repo)

	// Initial full build (state A).
	g := newTestGraph(t)
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	// Edit one file → state B (adds a symbol + an edge).
	write("f01.go", "package p\n\nfunc F01() { helper(); other() }\n\nfunc other() {}\n")
	gitStageAll(t, repo)

	// Incremental update, timed.
	start := time.Now()
	us, err := g.Update(context.Background(), repo, DefaultRegistry())
	dur := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if us.Changed != 1 || us.Reused != n {
		t.Errorf("Update stats: changed=%d reused=%d, want changed=1 reused=%d", us.Changed, us.Reused, n)
	}
	if dur > time.Second {
		t.Errorf("single-file reindex took %s, want <1s", dur)
	}
	t.Logf("incremental single-file reindex: %s (changed=%d reused=%d)", dur.Round(time.Millisecond), us.Changed, us.Reused)

	// Full rebuild of state B in a fresh DB.
	full := newTestGraph(t)
	if _, err := full.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	// The incrementally-updated graph must equal the full rebuild, byte for byte.
	got, want := dumpGraph(t, g), dumpGraph(t, full)
	if len(got) != len(want) {
		t.Fatalf("row count: incremental=%d full=%d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("row %d differs:\n incremental: %s\n full:        %s", i, got[i], want[i])
		}
	}
}

func TestStaleAndFreshness(t *testing.T) {
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package p\n\nfunc A() {}\n")
	gitInit(t, repo)

	g := newTestGraph(t)
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	// Freshly built → not stale.
	if stale, changed, err := g.Stale(context.Background(), repo); err != nil || stale || changed != 0 {
		t.Errorf("Stale after build = (%v,%d,%v), want (false,0,nil)", stale, changed, err)
	}

	// Freshness recorded.
	f := g.Freshness()
	if f.BuiltAt.IsZero() {
		t.Error("Freshness.BuiltAt is zero")
	}
	if f.HeadCommit == "" {
		t.Error("Freshness.HeadCommit is empty (expected a git HEAD)")
	}

	// Edit a file → stale.
	write("a.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")
	gitStageAll(t, repo)
	if stale, changed, err := g.Stale(context.Background(), repo); err != nil || !stale || changed != 1 {
		t.Errorf("Stale after edit = (%v,%d,%v), want (true,1,nil)", stale, changed, err)
	}
}
