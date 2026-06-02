package tui

import "github.com/atotto/clipboard"

// copyToClipboard copies value and returns a status line. Uses the local
// clipboard (pbcopy / xclip / wl-copy); works in a local terminal.
func copyToClipboard(value, label string) string {
	if err := clipboard.WriteAll(value); err != nil {
		return "copy failed: " + err.Error()
	}
	return "copied " + label + ": " + value
}
