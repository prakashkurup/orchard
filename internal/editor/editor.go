// Package editor detects installed editors/IDEs and opens repositories in them.
// The chosen default is remembered in a small state file so the picker only
// appears until the user settles on one.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Editor describes a launchable editor.
type Editor struct {
	ID       string   // stable id used for the saved default
	Name     string   // display name
	Cmd      string   // CLI launcher on PATH
	Args     []string // args before the path (e.g. nvim none, idea none)
	Apps     []string // macOS .app bundle names, for the `open -a` fallback
	Terminal bool     // true for TUI editors that must run in the foreground
}

// Catalog is every editor orchard knows how to launch.
func Catalog() []Editor {
	return []Editor{
		{ID: "vscode", Name: "VS Code", Cmd: "code", Apps: []string{"Visual Studio Code.app"}},
		{ID: "cursor", Name: "Cursor", Cmd: "cursor", Apps: []string{"Cursor.app"}},
		{ID: "intellij", Name: "IntelliJ IDEA", Cmd: "idea", Apps: []string{"IntelliJ IDEA.app", "IntelliJ IDEA CE.app"}},
		{ID: "goland", Name: "GoLand", Cmd: "goland", Apps: []string{"GoLand.app"}},
		{ID: "zed", Name: "Zed", Cmd: "zed", Apps: []string{"Zed.app"}},
		{ID: "sublime", Name: "Sublime Text", Cmd: "subl", Apps: []string{"Sublime Text.app"}},
		{ID: "nvim", Name: "Neovim", Cmd: "nvim", Terminal: true},
		{ID: "vim", Name: "Vim", Cmd: "vim", Terminal: true},
	}
}

// Available returns the editors actually installed on this machine.
func Available() []Editor {
	var out []Editor
	for _, e := range Catalog() {
		if e.Installed() {
			out = append(out, e)
		}
	}
	return out
}

// ByID looks up an editor in the catalog.
func ByID(id string) (Editor, bool) {
	for _, e := range Catalog() {
		if e.ID == id {
			return e, true
		}
	}
	return Editor{}, false
}

// Installed reports whether the editor can be launched: CLI on PATH, or (macOS)
// an installed .app bundle.
func (e Editor) Installed() bool {
	if e.Cmd != "" {
		if _, err := exec.LookPath(e.Cmd); err == nil {
			return true
		}
	}
	if runtime.GOOS == "darwin" {
		for _, app := range e.Apps {
			for _, base := range appDirs() {
				if _, err := os.Stat(filepath.Join(base, app)); err == nil {
					return true
				}
			}
		}
	}
	return false
}

// Command builds the command to open path. The bool reports whether it must run
// in the foreground (a terminal editor). Returns nil if it can't be launched.
func (e Editor) Command(path string) (*exec.Cmd, bool) {
	if e.Cmd != "" {
		if bin, err := exec.LookPath(e.Cmd); err == nil {
			args := append(append([]string{}, e.Args...), path)
			return exec.Command(bin, args...), e.Terminal
		}
	}
	if runtime.GOOS == "darwin" {
		for _, app := range e.Apps {
			for _, base := range appDirs() {
				if _, err := os.Stat(filepath.Join(base, app)); err == nil {
					return exec.Command("open", "-a", filepath.Join(base, app), path), false
				}
			}
		}
	}
	return nil, false
}

// CommandAt opens a specific file at a line number, using each editor's syntax.
func (e Editor) CommandAt(file string, line int) (*exec.Cmd, bool) {
	if e.Cmd != "" {
		if bin, err := exec.LookPath(e.Cmd); err == nil {
			switch e.ID {
			case "vscode", "cursor":
				return exec.Command(bin, "-g", fmt.Sprintf("%s:%d", file, line)), false
			case "zed", "sublime":
				return exec.Command(bin, fmt.Sprintf("%s:%d", file, line)), false
			case "intellij", "goland":
				return exec.Command(bin, "--line", strconv.Itoa(line), file), false
			case "vim", "nvim":
				return exec.Command(bin, fmt.Sprintf("+%d", line), file), true
			default:
				return exec.Command(bin, file), e.Terminal
			}
		}
	}
	if runtime.GOOS == "darwin" {
		for _, app := range e.Apps {
			for _, base := range appDirs() {
				if _, err := os.Stat(filepath.Join(base, app)); err == nil {
					return exec.Command("open", "-a", filepath.Join(base, app), file), false
				}
			}
		}
	}
	return nil, false
}

func appDirs() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// remembered default

func statePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "orchard", "editor")
}

// DefaultID returns the saved default editor id, or "".
func DefaultID() string {
	p := statePath()
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SaveDefault persists the chosen default editor id.
func SaveDefault(id string) error {
	p := statePath()
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(id+"\n"), 0o644)
}
