package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestStatusCleanRepo(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)

	r, err := Status(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}

	if r.Branch != "main" {
		t.Fatalf("branch = %q, want main", r.Branch)
	}
	if r.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", r.DefaultBranch)
	}
	if !r.HasUpstream {
		t.Fatal("expected upstream")
	}
	if !r.OnDefault {
		t.Fatal("expected on default branch")
	}
	if r.Dirty {
		t.Fatal("expected clean repo")
	}
	if r.Display != repo.DisplayClean {
		t.Fatalf("display = %s, want clean", r.Display)
	}
}

func TestStatusDirtyRepo(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)

	writeFile(t, filepath.Join(local, "README.md"), "changed\n")
	r, err := Status(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}

	if !r.Dirty {
		t.Fatal("expected dirty repo")
	}
	if r.Display != repo.DisplayDirty {
		t.Fatalf("display = %s, want dirty", r.Display)
	}
}

func TestPullSkipsDirtyRepo(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local := createRepoWithRemote(t, root)

	writeFile(t, filepath.Join(local, "README.md"), "changed\n")
	result := Pull(ctx, repo.Repo{Name: "local", Path: local})

	if result.Status != "skipped" {
		t.Fatalf("status = %q, want skipped", result.Status)
	}
	if !strings.Contains(result.Reason, "dirty") {
		t.Fatalf("reason = %q, want dirty", result.Reason)
	}
}

func TestPullFastForwardsBehindRepo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root := t.TempDir()
	local := createRepoWithRemote(t, root)
	remote := filepath.Join(root, "origin.git")
	other := filepath.Join(root, "other")

	git(t, root, "clone", remote, other)
	configUser(t, other)
	writeFile(t, filepath.Join(other, "remote.txt"), "remote change\n")
	git(t, other, "add", "remote.txt")
	git(t, other, "commit", "-m", "remote change")
	git(t, other, "push")

	git(t, local, "fetch", "--quiet")
	before, err := Status(ctx, repo.Repo{Name: "local", Path: local})
	if err != nil {
		t.Fatal(err)
	}
	if before.Behind != 1 {
		t.Fatalf("behind before pull = %d, want 1", before.Behind)
	}

	result := Pull(ctx, before)
	if result.Status != "pulled" {
		t.Fatalf("status = %q, err=%q reason=%q", result.Status, result.Error, result.Reason)
	}
	if result.Repo.Behind != 0 {
		t.Fatalf("behind after pull = %d, want 0", result.Repo.Behind)
	}
}

func createRepoWithRemote(t *testing.T, root string) string {
	t.Helper()

	remote := filepath.Join(root, "origin.git")
	local := filepath.Join(root, "local")

	git(t, root, "init", "--bare", "--initial-branch=main", remote)
	git(t, root, "init", "--initial-branch=main", local)
	configUser(t, local)
	writeFile(t, filepath.Join(local, "README.md"), "hello\n")
	git(t, local, "add", "README.md")
	git(t, local, "commit", "-m", "initial commit")
	git(t, local, "remote", "add", "origin", remote)
	git(t, local, "push", "-u", "origin", "main")

	return local
}

func configUser(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "config", "user.email", "orchard@example.com")
	git(t, dir, "config", "user.name", "Orchard Test")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}
