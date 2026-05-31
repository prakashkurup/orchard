package lang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T, files map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, a...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	run("init")
	n := 0
	for ext, count := range files {
		for i := 0; i < count; i++ {
			f := filepath.Join(dir, fmt.Sprintf("f%d.%s", n, ext))
			if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			n++
		}
	}
	run("add", "-A")
	return dir
}

func TestDetectDominant(t *testing.T) {
	dir := mkRepo(t, map[string]int{"kt": 6, "md": 3})
	s := Detect(context.Background(), dir)
	if len(s) == 0 || s[0].Name != "Kotlin" {
		t.Fatalf("dominant = %+v, want Kotlin", s)
	}
}

// Real code must beat docs even when docs outnumber it (the bug we fixed).
func TestDetectDocsDemoted(t *testing.T) {
	dir := mkRepo(t, map[string]int{"kt": 1, "md": 10})
	if got := Dominant(context.Background(), dir).Name; got != "Kotlin" {
		t.Fatalf("dominant = %q, want Kotlin (docs should be demoted)", got)
	}
}

func TestDetectDocsOnly(t *testing.T) {
	dir := mkRepo(t, map[string]int{"md": 5})
	if got := Dominant(context.Background(), dir).Name; got != "Markdown" {
		t.Fatalf("dominant = %q, want Markdown", got)
	}
}

func TestDetectNoLanguages(t *testing.T) {
	dir := mkRepo(t, map[string]int{"xyz": 3})
	if s := Detect(context.Background(), dir); len(s) != 0 {
		t.Fatalf("expected no languages, got %+v", s)
	}
}
