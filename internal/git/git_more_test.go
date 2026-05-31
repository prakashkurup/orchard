package git

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestCountLines(t *testing.T) {
	cases := map[string]int{"": 0, "x": 1, "a\nb\nc": 3, "a\nb\n": 2}
	for in, want := range cases {
		if got := countLines(in); got != want {
			t.Errorf("countLines(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseAheadBehind(t *testing.T) {
	b, a := parseAheadBehind("3 5")
	if b != 3 || a != 5 {
		t.Fatalf("parseAheadBehind(3 5) = (%d,%d), want (3,5)", b, a)
	}
	if b, a := parseAheadBehind(""); b != 0 || a != 0 {
		t.Fatalf("parseAheadBehind(empty) = (%d,%d), want (0,0)", b, a)
	}
}

// TestWebURL exercises the full path (git remote get-url + transform).
func TestWebURL(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)
	git(t, local, "remote", "set-url", "origin", "git@github.com:acme/widget-service.git")
	if got := WebURL(ctx, local); got != "https://github.com/acme/widget-service" {
		t.Fatalf("WebURL = %q", got)
	}
}

// TestToWebURL covers the pure remote-URL transform without a real repository.
func TestToWebURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:org/repo.git":     "https://github.com/org/repo",
		"git@github.com:org/repo":         "https://github.com/org/repo",
		"ssh://git@github.com/org/repo":   "https://github.com/org/repo",
		"https://github.com/org/repo":     "https://github.com/org/repo",
		"https://github.com/org/repo.git": "https://github.com/org/repo",
		"":                                "",
		// embedded credentials must be stripped (never reach the browser/argv)
		"https://x-access-token:ghp_SECRET@github.com/org/repo.git": "https://github.com/org/repo",
		"ssh://git@example.com:22/org/repo.git":                     "https://example.com:22/org/repo",
	}
	for in, want := range cases {
		if got := toWebURL(in); got != want {
			t.Errorf("toWebURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCheckoutRejectsLeadingDash guards against argument injection: a ref name
// beginning with "-" must never be handed to `git checkout` as a flag.
func TestCheckoutRejectsLeadingDash(t *testing.T) {
	for _, bad := range []string{"-f", "-D", "--orphan"} {
		if err := Checkout(context.Background(), t.TempDir(), bad); err == nil {
			t.Errorf("Checkout(%q) = nil, want a refusal error", bad)
		}
	}
}

func TestStatusChangedFilesAndHead(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)
	writeFile(t, filepath.Join(local, "a.txt"), "a\n")
	writeFile(t, filepath.Join(local, "b.txt"), "b\n")

	r, err := Status(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChangedFiles != 2 {
		t.Fatalf("ChangedFiles = %d, want 2", r.ChangedFiles)
	}
	if r.Head == "" {
		t.Fatal("Head should be set")
	}
}

func TestBranches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)
	git(t, local, "branch", "feature/x")

	bs, err := Branches(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}
	var current string
	names := map[string]bool{}
	for _, b := range bs {
		names[b.Name] = true
		if b.Current {
			current = b.Name
		}
	}
	if current != "main" {
		t.Fatalf("current branch = %q, want main", current)
	}
	if !names["feature/x"] {
		t.Fatalf("branches missing feature/x: %v", names)
	}
	// origin/HEAD must not leak in as a bogus "origin" branch
	if names["origin"] {
		t.Fatal("origin/HEAD leaked as a branch named 'origin'")
	}
}

func TestNewCommitCount(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)
	before := git(t, local, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(local, "n.txt"), "n\n")
	git(t, local, "add", "n.txt")
	git(t, local, "commit", "-m", "another")

	if n := NewCommitCount(ctx, local, before); n != 1 {
		t.Fatalf("NewCommitCount = %d, want 1", n)
	}
	if n := NewCommitCount(ctx, local, ""); n != 0 {
		t.Fatalf("NewCommitCount(empty since) = %d, want 0", n)
	}
}

func TestWorklogAndAuthor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)

	if got := AuthorEmail(ctx, local); got != "orchard@example.com" {
		t.Fatalf("AuthorEmail = %q", got)
	}
	cs := Worklog(ctx, local, "1 year ago")
	if len(cs) == 0 {
		t.Fatal("expected at least one commit in worklog")
	}
}
