package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListOrgReposMissingOrg(t *testing.T) {
	if _, err := ListOrgRepos(context.Background(), "", "", false); err == nil {
		t.Fatal("expected error for missing org")
	}
}

func TestListOrgReposInvalidMatch(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fake-token") // so it gets past auth to regex compile
	if _, err := ListOrgRepos(context.Background(), "someorg", "(", false); err == nil {
		t.Fatal("expected error for invalid match regex")
	}
}

func TestAuthTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "  tok123  ")
	tok, err := authToken(context.Background())
	if err != nil || tok != "tok123" {
		t.Fatalf("authToken = %q, %v", tok, err)
	}
}

func TestCloneOneSkipsExisting(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := cloneOne(context.Background(), RemoteRepo{Name: "foo", SSHURL: "git@x:y/foo.git"}, root)
	if res.Status != "skipped" {
		t.Fatalf("status = %q, want skipped", res.Status)
	}
}

func TestCloneOneNoURL(t *testing.T) {
	res := cloneOne(context.Background(), RemoteRepo{Name: "foo"}, t.TempDir())
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed", res.Status)
	}
}

func TestCloneReposAggregates(t *testing.T) {
	out := CloneRepos(context.Background(), []RemoteRepo{{Name: "a"}, {Name: "b"}}, t.TempDir(), 2)
	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	for _, r := range out {
		if r.Status != "failed" { // no URL -> failed
			t.Fatalf("expected failed for URL-less repo, got %q", r.Status)
		}
	}
}
