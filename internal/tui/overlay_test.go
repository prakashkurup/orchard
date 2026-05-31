package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestHexRGB(t *testing.T) {
	cases := []struct {
		hex     string
		r, g, b int
	}{
		{"#FFFFFF", 255, 255, 255},
		{"#000000", 0, 0, 0},
		{"#82AAFF", 0x82, 0xAA, 0xFF},
		{"#abc", 0, 0, 0}, // invalid length -> zero
		{"", 0, 0, 0},
	}
	for _, c := range cases {
		if r, g, b := hexRGB(c.hex); r != c.r || g != c.g || b != c.b {
			t.Errorf("hexRGB(%q) = (%d,%d,%d), want (%d,%d,%d)", c.hex, r, g, b, c.r, c.g, c.b)
		}
	}
}

func TestDimHex(t *testing.T) {
	if got := dimHex("#FFFFFF", 55, 100); got != "#8C8C8C" {
		t.Errorf("dimHex(#FFFFFF,55,100) = %s, want #8C8C8C", got)
	}
	if got := dimHex("#82AAFF", 100, 100); got != "#82AAFF" {
		t.Errorf("dimHex at full brightness = %s, want #82AAFF", got)
	}
}

func TestDimANSI(t *testing.T) {
	// a 24-bit foreground colour should be rescaled to 55%
	got := dimANSI("\x1b[38;2;200;100;50mX\x1b[0m", 55, 100)
	if !strings.Contains(got, "38;2;110;55;27") {
		t.Errorf("dimANSI did not rescale 24-bit colour: %q", got)
	}
}

func TestOverlayCenter(t *testing.T) {
	const width, height = 40, 10
	boxLine := lipgloss.NewStyle().Background(lipgloss.Color(panel)).Render(strings.Repeat("X", 20))
	box := boxLine + "\n" + boxLine + "\n" + boxLine
	// backdrop: one row wider than width (truncate branch), one narrower (pad
	// branch), and fewer rows than height (row-pad branch).
	backdrop := strings.Repeat("W", 60) + "\n" + "ab"

	out := overlayCenter(box, backdrop, width, height)
	rows := strings.Split(out, "\n")
	if len(rows) != height {
		t.Fatalf("overlayCenter produced %d rows, want %d", len(rows), height)
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != width {
			t.Errorf("row %d width = %d, want %d", i, w, width)
		}
	}
}
