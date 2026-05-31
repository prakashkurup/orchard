package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsImmediateGitRepositories(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "service-a")
	otherDir := filepath.Join(root, "notes")

	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	repos, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Name != "service-a" {
		t.Fatalf("unexpected repo name: %s", repos[0].Name)
	}
}

func TestPullSkipReasonUsesComposableFacts(t *testing.T) {
	r := Repo{
		Name:          "service-a",
		Branch:        "feature/test",
		DefaultBranch: "main",
		Dirty:         true,
		HasUpstream:   true,
	}
	r = r.WithDisplay()

	if r.Display != DisplayDirty {
		t.Fatalf("expected dirty to be primary display, got %s", r.Display)
	}
	if got := r.PullSkipReason(); got != "working tree is dirty" {
		t.Fatalf("unexpected skip reason: %s", got)
	}
}
