package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestStateFor verifies the read-only snapshot used by the TUI graph badge and
// detail panel: ok=false before a build, populated + HEAD-stamped after.
func TestStateFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)                                     // darwin: UserConfigDir uses $HOME/Library
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config")) // linux: UserConfigDir uses $XDG_CONFIG_HOME

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"),
		[]byte("package p\n\nfunc Foo() { Bar() }\n\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)

	if _, ok := StateFor(repo); ok {
		t.Fatal("StateFor before build = ok true, want false")
	}

	g, err := OpenForRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	g.Close()

	st, ok := StateFor(repo)
	if !ok {
		t.Fatal("StateFor after build = ok false, want true")
	}
	if st.Symbols < 2 {
		t.Errorf("Symbols = %d, want >= 2 (Foo, Bar)", st.Symbols)
	}
	if st.HeadCommit == "" {
		t.Error("HeadCommit is empty, want the repo's current HEAD")
	}
}

// TestRemoveForRepo verifies deleting a built graph: ok=true, and StateFor then
// reports no graph (a second delete is a no-op).
func TestRemoveForRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"),
		[]byte("package p\n\nfunc Foo() { Bar() }\n\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)

	g, err := OpenForRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Build(context.Background(), repo, DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	g.Close()

	if ok, err := RemoveForRepo(repo); err != nil || !ok {
		t.Fatalf("RemoveForRepo = (%v, %v), want (true, nil)", ok, err)
	}
	if _, ok := StateFor(repo); ok {
		t.Error("StateFor after delete = ok true, want false")
	}
	if ok, err := RemoveForRepo(repo); err != nil || ok {
		t.Errorf("second RemoveForRepo = (%v, %v), want (false, nil)", ok, err)
	}
}
