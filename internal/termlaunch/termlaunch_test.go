package termlaunch

import (
	"strings"
	"testing"
)

func TestNewTabRejectsNewlineDir(t *testing.T) {
	// a newline in the dir could break out of the AppleScript literal - refuse
	if _, ok := NewTab("/tmp/evil\ndo shell script \"id\"", "claude"); ok {
		t.Fatal("NewTab should refuse a directory containing a newline")
	}
	if _, ok := NewTab("/tmp/ok", "claude\ninject"); ok {
		t.Fatal("NewTab should refuse a program containing a newline")
	}
}

func TestAsQuoteStripsNewlines(t *testing.T) {
	if got := asQuote("a\nb\r"); strings.ContainsAny(got, "\n\r") {
		t.Fatalf("asQuote leaked a raw newline: %q", got)
	}
}

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func base(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("TMUX", "")
	t.Setenv("ORCHARD_TERMINAL_CMD", "")
}

func TestNewTabGhostty(t *testing.T) {
	base(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	cmd, ok := NewTab("/x/y", "claude")
	if !ok || cmd == nil {
		t.Fatal("expected a command")
	}
	if !argsContain(cmd.Args, "Ghostty.app") || !argsContain(cmd.Args, "-e") {
		t.Fatalf("ghostty args = %v", cmd.Args)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "claude") {
		t.Fatalf("command should run claude: %v", cmd.Args)
	}
}

func TestNewTabITerm(t *testing.T) {
	base(t)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	cmd, ok := NewTab("/x/y", "claude")
	if !ok || cmd.Args[0] != "osascript" {
		t.Fatalf("iTerm should use osascript: %v", cmd.Args)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "iTerm") {
		t.Fatalf("script should target iTerm: %v", cmd.Args)
	}
}

func TestNewTabTmux(t *testing.T) {
	base(t)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("TERM_PROGRAM", "ghostty")
	cmd, ok := NewTab("/repo", "claude")
	if !ok || cmd.Args[0] != "tmux" || !argsContain(cmd.Args, "new-window") {
		t.Fatalf("tmux env should use tmux new-window: %v", cmd.Args)
	}
	if !argsContain(cmd.Args, "/repo") {
		t.Fatalf("tmux should set cwd: %v", cmd.Args)
	}
}

func TestNewTabOverride(t *testing.T) {
	base(t)
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("ORCHARD_TERMINAL_CMD", "myterm --dir {dir} -- {cmd}")
	cmd, ok := NewTab("/a b/c", "claude")
	if !ok {
		t.Fatal("override should produce a command")
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "myterm --dir") || !strings.Contains(joined, "claude") {
		t.Fatalf("override not applied: %v", cmd.Args)
	}
	if !strings.Contains(joined, "'/a b/c'") {
		t.Fatalf("dir should be shell-quoted: %v", cmd.Args)
	}
}
