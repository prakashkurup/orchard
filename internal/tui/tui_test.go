package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces a truecolor profile for the whole package's tests. Without it,
// lipgloss detects the non-TTY test environment and degrades to the Ascii
// profile, emitting no SGR codes - which would make every render test's
// ANSI-stripping and "no escape leaked" assertions silent no-ops and leave the
// colour-producing paths (dimANSI, freshnessColor, commitAgeColor, …) untested.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
