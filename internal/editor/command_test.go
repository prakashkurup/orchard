package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin creates an executable `name` in a temp dir and puts it first on PATH.
func fakeBin(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCommandGUIEditor(t *testing.T) {
	fakeBin(t, "code")
	e, _ := ByID("vscode")
	cmd, terminal := e.Command("/repo/path")
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if terminal {
		t.Error("vscode should be GUI (non-terminal)")
	}
	if cmd.Args[len(cmd.Args)-1] != "/repo/path" {
		t.Fatalf("args = %v, want path last", cmd.Args)
	}
}

func TestCommandAtVSCode(t *testing.T) {
	fakeBin(t, "code")
	e, _ := ByID("vscode")
	cmd, _ := e.CommandAt("/repo/file.go", 42)
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "-g") || !strings.Contains(joined, "/repo/file.go:42") {
		t.Fatalf("vscode CommandAt args = %v", cmd.Args)
	}
}

func TestCommandAtVim(t *testing.T) {
	fakeBin(t, "vim")
	e, _ := ByID("vim")
	cmd, terminal := e.CommandAt("/repo/file.go", 42)
	if !terminal {
		t.Error("vim should be a terminal editor")
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "+42") || !strings.Contains(joined, "/repo/file.go") {
		t.Fatalf("vim CommandAt args = %v", cmd.Args)
	}
}

func TestInstalledViaPath(t *testing.T) {
	fakeBin(t, "nvim")
	e, _ := ByID("nvim")
	if !e.Installed() {
		t.Fatal("nvim should be detected as installed when on PATH")
	}
}
