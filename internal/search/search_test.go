package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompileSmartCase(t *testing.T) {
	re, _ := Compile("foo") // all-lowercase → case-insensitive
	if re == nil || !re.MatchString("FOO") {
		t.Fatal("lowercase query should be case-insensitive")
	}
	re, _ = Compile("Foo") // has uppercase → case-sensitive
	if re == nil || re.MatchString("foo") {
		t.Fatal("mixed-case query should be case-sensitive")
	}
}

func TestCompileRegexAndLiteralFallback(t *testing.T) {
	re, _ := Compile("a.c") // valid regex
	if re == nil || !re.MatchString("abc") {
		t.Fatal("expected regex match")
	}
	re, _ = Compile("a(") // invalid regex → literal fallback
	if re == nil || !re.MatchString("x a( y") {
		t.Fatal("invalid regex should fall back to literal")
	}
}

func TestSearchFindsMatch(t *testing.T) {
	dir := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, a...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("package x\n// the needle is here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.log"), []byte("needle in a log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")

	res := Search(context.Background(), []Target{{Name: "x", Path: dir}}, "needle", 0)
	if len(res) != 1 || len(res[0].Matches) != 1 {
		t.Fatalf("expected 1 match in code.go, got %+v", res)
	}
	m := res[0].Matches[0]
	if m.File != "code.go" || m.Line != 2 {
		t.Fatalf("match = %s:%d, want code.go:2 (.log should be skipped)", m.File, m.Line)
	}
}
