// Package termlaunch opens a command in a new terminal tab/window, picking the
// right mechanism for the user's terminal. There is no cross-terminal standard,
// so we detect via $TERM_PROGRAM (and $TMUX) and fall back to a user-provided
// template in $ORCHARD_TERMINAL_CMD.
package termlaunch

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// NewTab returns a command that runs `program` in a new tab/window with the
// working directory set to dir. supported=false means no mechanism was found
// and the caller should fall back (e.g. run in-place).
//
// Resolution order:
//  1. $ORCHARD_TERMINAL_CMD template ({dir} and {cmd} are substituted)
//  2. tmux, if running inside it
//  3. the detected terminal ($TERM_PROGRAM)
func NewTab(dir, program string) (*exec.Cmd, bool) {
	// A newline/CR in dir or program would break out of the AppleScript string
	// literal (and the shell/tmux/template paths) and let following bytes run as
	// script. Such paths are pathological; refuse so the caller falls back to an
	// in-place launch rather than executing injected commands.
	if strings.ContainsAny(dir, "\n\r") || strings.ContainsAny(program, "\n\r") {
		return nil, false
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	// Run via a login shell so PATH (and thus `claude`) resolves like a normal
	// terminal session.
	login := func(inDir bool) string {
		if inDir {
			return "cd " + shQuote(dir) + " && exec " + program
		}
		return program
	}

	if tmpl := os.Getenv("ORCHARD_TERMINAL_CMD"); tmpl != "" {
		expanded := strings.ReplaceAll(tmpl, "{dir}", shQuote(dir))
		expanded = strings.ReplaceAll(expanded, "{cmd}", program)
		return exec.Command(shell, "-lc", expanded), true
	}

	if os.Getenv("TMUX") != "" {
		// new tmux window in the repo dir
		return exec.Command("tmux", "new-window", "-c", dir, shell, "-lc", login(false)), true
	}

	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return exec.Command("osascript", "-e", iTermScript(shell, login(true))), true
	case "Apple_Terminal":
		return exec.Command("osascript", "-e", terminalScript(shell, login(true))), true
	case "ghostty", "Ghostty":
		// macOS Ghostty has no new-window IPC; the documented way is
		// `open -na Ghostty.app --args -e <cmd>`. cd is baked into the command so
		// we don't depend on the --working-directory key.
		return exec.Command("open", "-na", "Ghostty.app", "--args",
			"-e", shell, "-lc", login(true)), true
	case "WezTerm":
		return exec.Command("wezterm", "cli", "spawn", "--cwd", dir, "--", shell, "-lc", login(false)), true
	}

	// macOS generic fallback: a new Terminal.app window.
	if runtime.GOOS == "darwin" {
		return exec.Command("osascript", "-e", terminalScript(shell, login(true))), true
	}
	return nil, false
}

func iTermScript(shell, cmd string) string {
	run := shell + " -lc " + shQuote(cmd)
	return `tell application "iTerm"
	activate
	if (count of windows) = 0 then
		create window with default profile
		tell current session of current window to write text ` + asQuote(run) + `
	else
		tell current window
			create tab with default profile
			tell current session to write text ` + asQuote(run) + `
		end tell
	end if
end tell`
}

func terminalScript(shell, cmd string) string {
	run := shell + " -lc " + shQuote(cmd)
	return `tell application "Terminal"
	activate
	do script ` + asQuote(run) + `
end tell`
}

// shQuote single-quotes a string for POSIX shells.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// asQuote produces an AppleScript string literal. AppleScript literals cannot
// hold raw newlines, so CR/LF are dropped (defense-in-depth; NewTab already
// refuses newline-bearing input) to prevent breaking out of the literal.
func asQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return `"` + s + `"`
}
