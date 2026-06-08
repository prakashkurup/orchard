package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRoutesHeaderAsCPPWithSibling(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "widget.h", "class Widget { public: void run(); };\n")
	writeFile(t, repo, "widget.cpp", "#include \"widget.h\"\nvoid Widget::run() {}\n")
	gitInit(t, repo)

	files, _, err := Discover(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := langFor(files, "widget.h"); got != "cpp" {
		t.Fatalf("widget.h lang = %q, want cpp", got)
	}
}

func TestDiscoverRoutesPlainHeaderAsC(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "lib.h", "int add(int a, int b);\n")
	writeFile(t, repo, "lib.c", "#include \"lib.h\"\nint add(int a, int b) { return a + b; }\n")
	gitInit(t, repo)

	files, _, err := Discover(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := langFor(files, "lib.h"); got != "c" {
		t.Fatalf("lib.h lang = %q, want c", got)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func langFor(files []DiscoveredFile, rel string) string {
	for _, f := range files {
		if f.Rel == rel {
			return f.Lang
		}
	}
	return ""
}
