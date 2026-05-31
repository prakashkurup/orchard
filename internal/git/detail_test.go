package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestDetailPreservesStatusLeadingColumn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)

	// tracked files in a subdir, then modify them -> " M <path>"
	sub := filepath.Join(local, "app", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "Alpha.kt"), "v1\n")
	writeFile(t, filepath.Join(sub, "Beta.kt"), "v1\n")
	git(t, local, "add", "-A")
	git(t, local, "commit", "-m", "add sources")
	writeFile(t, filepath.Join(sub, "Alpha.kt"), "v2\n")
	writeFile(t, filepath.Join(sub, "Beta.kt"), "v2\n")

	info, err := Detail(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.StatusLines) == 0 {
		t.Fatal("expected status lines")
	}
	// the first line's leading space (XY column) must be intact (regression guard)
	first := info.StatusLines[0]
	if !strings.HasPrefix(first, " M ") {
		t.Fatalf("first status line lost its leading column: %q", first)
	}
	if path := strings.TrimSpace(first[3:]); !strings.HasPrefix(path, "app/src/") {
		t.Fatalf("path corrupted: %q", path)
	}

	if len(info.Commits) == 0 {
		t.Fatal("expected commits")
	}
	var sawCommit bool
	for _, row := range info.Graph {
		if row.IsCommit && row.Hash != "" {
			sawCommit = true
		}
	}
	if !sawCommit {
		t.Fatal("expected at least one commit row in the graph")
	}
	if len(info.Remotes) == 0 || !strings.Contains(strings.Join(info.Remotes, " "), "origin") {
		t.Fatalf("expected origin remote, got %v", info.Remotes)
	}
}

func TestFilterByName(t *testing.T) {
	repos := []repo.Repo{{Name: "payments-web"}, {Name: "auth-gateway"}, {Name: "payments-api"}}
	got, err := FilterByName(repos, "^payments")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 payments repos, got %d", len(got))
	}
	if all, _ := FilterByName(repos, ""); len(all) != 3 {
		t.Fatalf("empty pattern should return all, got %d", len(all))
	}
	if _, err := FilterByName(repos, "("); err == nil {
		t.Fatal("invalid regex should error")
	}
}

func TestDefaultBranchFallback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	git(t, dir, "init", "--initial-branch=trunk", dir)
	configUser(t, dir)
	writeFile(t, filepath.Join(dir, "f.txt"), "x\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "init")

	// no remote/origin HEAD; default should fall back to the current branch
	if got := defaultBranch(ctx, dir, "trunk"); got != "trunk" {
		t.Fatalf("defaultBranch = %q, want trunk", got)
	}
}

func TestScanFindsRepos(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	createRepoWithRemote(t, root) // creates root/local + root/origin.git
	repos, err := Scan(ctx, root, 4)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	if len(repos) == 0 {
		t.Fatalf("expected at least one repo, got %v", names)
	}
}
