package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffUntrackedAndScoped(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	configUser(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "hello\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	ctx := context.Background()

	// untracked new file: HEAD diff is empty, so we fall back to its full content
	writeFile(t, filepath.Join(dir, "new.txt"), "brand new line\n")
	out, err := Diff(ctx, dir, "new.txt")
	if err != nil {
		t.Fatalf("Diff untracked err: %v", err)
	}
	if !strings.Contains(out, "brand new line") {
		t.Fatalf("untracked diff should show new content, got %q", out)
	}

	// tracked + unmodified: must stay empty (not dump the whole file via --no-index)
	if out, err := Diff(ctx, dir, "tracked.txt"); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("unmodified tracked file should diff empty, got %q (err %v)", out, err)
	}

	// tracked + modified: shows the change
	writeFile(t, filepath.Join(dir, "tracked.txt"), "hello\nmore\n")
	if out, _ := Diff(ctx, dir, "tracked.txt"); !strings.Contains(out, "more") {
		t.Fatalf("modified diff should show the change, got %q", out)
	}
}

func TestDiffDeletedAndMultiPath(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	configUser(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	ctx := context.Background()

	// a deleted tracked file: the diff shows the removal (not the untracked fallback)
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	out, err := Diff(ctx, dir, "a.txt")
	if err != nil || !strings.Contains(out, "a.txt") || strings.TrimSpace(out) == "" {
		t.Fatalf("deleted-file diff should show the removal, got %q (err %v)", out, err)
	}

	// multiple pathspecs: both files appear in one diff
	writeFile(t, filepath.Join(dir, "b.txt"), "b2\n")
	out, _ = Diff(ctx, dir, "a.txt", "b.txt")
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
		t.Fatalf("multi-pathspec diff should include both files, got %q", out)
	}
}
