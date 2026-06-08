package graph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuildGoRepo is an integration test: it builds a tiny real git repo of Go
// files end-to-end (discover → go/ast → SQLite) and queries the result.
func TestBuildGoRepo(t *testing.T) {
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("auth.go", "package p\n\nfunc Login() { validate() }\n\nfunc validate() {}\n")
	write("api.go", "package p\n\nfunc Handle() { Login() }\n")
	write("gen.pb.go", "package p\n\nfunc Generated() {}\n") // must be skipped (generated)
	gitInit(t, repo)

	g := newTestGraph(t)
	stats, err := g.Build(context.Background(), repo, DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Errorf("Files = %d, want 2 (gen.pb.go skipped)", stats.Files)
	}
	if stats.Symbols < 3 {
		t.Errorf("Symbols = %d, want >= 3 (Login, validate, Handle)", stats.Symbols)
	}
	if stats.ByTier[TierPrecise] != 2 {
		t.Errorf("ByTier[precise] = %d, want 2", stats.ByTier[TierPrecise])
	}

	callers, err := g.WhoCalls("Login", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].Caller != "Handle" {
		t.Errorf("WhoCalls(Login) = %+v, want Handle", callers)
	}
	if defs, _ := g.FindDef("Generated", 50); len(defs) != 0 {
		t.Errorf("FindDef(Generated) = %+v, want none (generated file skipped)", defs)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
